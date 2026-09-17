package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/compare"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

const (
	maxDelegateInterfaceBytes = 1 << 20
	maxDelegateRegistryBytes  = 4 << 20
	maxDelegateRecords        = 256
	maxDelegateRoles          = 32
	maxRoleAlternatives       = 8
	rolePolicyVersion         = "ob.delegate-role-policy@1"
)

// DelegateRole is a manager-local responsibility. Its expected interfaces are
// unbound values, not locators, invokers, or authorization grants.
type DelegateRole struct {
	ID                 string            `json:"id"`
	Description        string            `json:"description"`
	AcceptedInterfaces []json.RawMessage `json:"acceptedInterfaces"`
}

type roleAlternative struct {
	identity string
	value    json.RawMessage
	iface    *openbindings.Interface
}

// roleCatalogue is immutable once constructed. Neither callers' values nor
// returned discovery values alias its storage. Identity also covers OB policy.
type roleCatalogue struct {
	identity     string
	roles        []DelegateRole
	alternatives map[string][]roleAlternative
}

// roleAdmission carries the exact correspondence used to admit this provider.
// Runtime queries/work must use these keys, never rediscover via bare names.
type roleAdmission struct {
	Alternative string
	Operations  map[string]string
}

func newRoleCatalogue(roles []DelegateRole) (*roleCatalogue, error) {
	if len(roles) > maxDelegateRoles {
		return nil, errors.New("role catalogue exceeds OB capacity")
	}
	data, err := jsonvalue.Marshal(roles)
	if err != nil || len(data) > maxDelegateRegistryBytes {
		return nil, errors.New("role catalogue cannot be retained")
	}
	var snapshot []DelegateRole
	if err := jsonvalue.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	if snapshot == nil {
		snapshot = []DelegateRole{}
	}
	c := &roleCatalogue{roles: snapshot, alternatives: map[string][]roleAlternative{}}
	for _, role := range snapshot {
		if role.ID == "" || role.Description == "" || len(role.AcceptedInterfaces) == 0 || len(role.AcceptedInterfaces) > maxRoleAlternatives {
			return nil, errors.New("role needs an ID, description and bounded nonempty accepted interfaces")
		}
		if _, duplicate := c.alternatives[role.ID]; duplicate {
			return nil, errors.New("duplicate role ID")
		}
		alternatives := []roleAlternative{}
		for _, raw := range role.AcceptedInterfaces {
			if len(raw) > maxDelegateInterfaceBytes {
				return nil, errors.New("expected interface exceeds OB capacity")
			}
			iface, err := openbindings.ValidateDocument(raw)
			if err != nil {
				return nil, errors.New("invalid expected interface")
			}
			if len(iface.Operations) == 0 || len(iface.Bindings) != 0 || len(iface.Dependencies) != 0 {
				return nil, errors.New("expected interfaces must be nonempty, unbound and dependency-free")
			}
			// SDK encoding retains exact numbers and authored fields. Hashing is
			// a deterministic local choice, not an interface content identity.
			canonical, err := jsonvalue.Marshal(iface)
			if err != nil {
				return nil, errors.New("expected interface cannot be retained")
			}
			alternatives = append(alternatives, roleAlternative{HashContent(canonical), raw, iface})
		}
		sort.Slice(alternatives, func(i, j int) bool { return alternatives[i].identity < alternatives[j].identity })
		c.alternatives[role.ID] = alternatives
	}
	// Catalogue discovery order is unspecified. Normalize IDs and alternatives
	// for process agreement without making source-array order a preference.
	sort.Slice(c.roles, func(i, j int) bool { return c.roles[i].ID < c.roles[j].ID })
	for i := range c.roles {
		for j, alt := range c.alternatives[c.roles[i].ID] {
			c.roles[i].AcceptedInterfaces[j] = alt.value
		}
	}
	canonical, err := jsonvalue.Marshal(c.roles)
	if err != nil {
		return nil, err
	}
	c.identity = HashContent(append([]byte(rolePolicyVersion+"\n"), canonical...))
	return c, nil
}

func (c *roleCatalogue) list() ([]DelegateRole, error) {
	data, err := jsonvalue.Marshal(c.roles)
	if err != nil {
		return nil, err
	}
	var out []DelegateRole
	err = jsonvalue.Unmarshal(data, &out)
	return out, err
}

func (c *roleCatalogue) admit(raw json.RawMessage, roles []string) (map[string]roleAdmission, error) {
	if len(raw) > maxDelegateInterfaceBytes {
		return nil, errors.New("interface exceeds OB capacity")
	}
	if len(roles) == 0 || len(roles) > maxDelegateRoles {
		return nil, errors.New("a bounded nonempty role set is required")
	}
	seen := map[string]bool{}
	for _, id := range roles {
		if seen[id] {
			return nil, errors.New("duplicate requested role")
		}
		if _, found := c.alternatives[id]; !found {
			return nil, errors.New("unknown requested role")
		}
		seen[id] = true
	}
	provider, err := openbindings.ValidateDocument(raw)
	if err != nil {
		return nil, errors.New("invalid supplied interface")
	}
	admissions := map[string]roleAdmission{}
	for _, role := range roles {
		for _, alternative := range c.alternatives[role] {
			operations, err := exactRoleCorrespondence(alternative.iface, provider)
			if err != nil {
				continue
			}
			admissions[role] = roleAdmission{Alternative: alternative.identity, Operations: operations}
			break
		}
		if _, found := admissions[role]; !found {
			return nil, fmt.Errorf("no complete compatible alternative for role %q", role)
		}
	}
	return admissions, nil
}

func exactRoleCorrespondence(expected, provider *openbindings.Interface) (map[string]string, error) {
	keys := make([]string, 0, len(expected.Operations))
	for key := range expected.Operations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	correspondence := map[string]string{}
	for _, key := range keys {
		required := expected.Operations[key]
		actualKey, actual, found := openbindings.ResolveOperation(provider, key)
		if !found {
			return nil, errors.New("missing expected operation")
		}
		// A skipped comparison cannot prove a specified requirement. Expected
		// unspecified slots intentionally make no claim; they are never {}.
		if (required.Input != nil && actual.Input == nil) || (required.Output != nil && actual.Output == nil) {
			return nil, errors.New("required schema evidence is unspecified")
		}
		issues, err := compare.CheckOperationCompatibility(expected, key, provider)
		if err != nil || len(issues) != 0 {
			return nil, errors.New("required directional compatibility was not established")
		}
		correspondence[key] = actualKey
	}
	return correspondence, nil
}

func defaultRoleCatalogue() (*roleCatalogue, error) {
	roles := make([]DelegateRole, 0, len(DelegateCapabilities))
	for _, capability := range DelegateCapabilities {
		expected, err := RequirementInterface(capability)
		if err != nil {
			return nil, err
		}
		raw, err := jsonvalue.Marshal(expected)
		if err != nil {
			return nil, err
		}
		roles = append(roles, DelegateRole{ID: string(capability),
			Description:        "OB " + string(capability) + " handling. Admission records compatibility, not trust or activation. Runtime requires exact binding-spec support, existing recipient authorization and an executable binding. External preference orders eligible providers; equal preferences preserve registration order. Native precedence is entrypoint-specific; no workload failover.",
			AcceptedInterfaces: []json.RawMessage{raw}})
	}
	return newRoleCatalogue(roles)
}
