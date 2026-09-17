package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

const roleRegistryFormat = "ob.delegate-registry@1"

// DelegateRegistration is the shared full-value record. Native selection
// overrides, migration provenance and issuance metadata are deliberately absent.
type DelegateRegistration struct {
	ID              string                 `json:"id"`
	Interface       json.RawMessage        `json:"interface"`
	Roles           []string               `json:"roles"`
	RolePreferences map[string]json.Number `json:"rolePreferences"`
}

func (r *DelegateRegistration) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(data, &fields); err != nil || fields == nil {
		return errors.New("invalid registration record")
	}
	for key := range fields {
		if !slices.Contains([]string{"id", "interface", "roles", "rolePreferences"}, key) {
			return errors.New("unsupported registration field")
		}
	}
	type plain DelegateRegistration
	var decoded plain
	if err := jsonvalue.Unmarshal(data, &decoded); err != nil {
		return errors.New("invalid registration record")
	}
	preferences, err := decodeRolePreferences(fields["rolePreferences"])
	if err != nil {
		return err
	}
	decoded.RolePreferences = preferences
	*r = DelegateRegistration(decoded)
	return nil
}

// RoleRegistrationInput retains omission, {} and null as different inputs.
// Its interface is a value, never a locator to be dereferenced by admission.
type RoleRegistrationInput struct {
	ID              string          `json:"id,omitempty"`
	Interface       json.RawMessage `json:"interface"`
	Roles           []string        `json:"roles"`
	RolePreferences json.RawMessage `json:"rolePreferences,omitempty"`
}

type roleRegistryState struct {
	Format             string                                       `json:"format"`
	Catalogue          string                                       `json:"catalogue"`
	Revision           uint64                                       `json:"revision"`
	Issuance           string                                       `json:"issuance"`
	Next               string                                       `json:"next"`
	Records            []DelegateRegistration                       `json:"records"`
	BindingPreferences map[string]map[string]map[string]json.Number `json:"bindingPreferences"`
	MigrationReceipt   json.RawMessage                              `json:"migrationReceipt,omitempty"`
}

type roleRegistry struct {
	path      string
	catalogue *roleCatalogue
}

type roleCandidate struct {
	Record             DelegateRegistration
	Admission          roleAdmission
	Role               string
	RegistryRevision   uint64
	BindingPreferences map[string]json.Number
	ScopeIdentity      string
	CatalogueIdentity  string
	ConfigIdentity     string
}

func (c roleCandidate) preference(bindingSpec string) json.Number {
	if value, ok := c.BindingPreferences[bindingSpec]; ok {
		return value
	}
	if value, ok := c.Record.RolePreferences[c.Role]; ok {
		return value
	}
	return json.Number("0")
}

// candidates is a coherent admission snapshot, not a claim of executable
// capability. Runtime must still assess bindings/support/trust before work.
// It refuses a mixed catalogue and never probes an unrequested role.
func (r *roleRegistry) candidates(role string) ([]roleCandidate, error) {
	config, err := LoadEnvConfig(r.path)
	if err != nil {
		return nil, err
	}
	state, err := readRoleState(config)
	if err != nil {
		return nil, err
	}
	out := []roleCandidate{}
	if state == nil {
		return out, nil
	}
	if state.Catalogue != r.catalogue.identity {
		return nil, errors.New("role catalogue changed; explicit registry review is required")
	}
	if _, known := r.catalogue.alternatives[role]; !known {
		return out, nil
	}
	for _, record := range state.Records {
		if !slices.Contains(record.Roles, role) {
			continue
		}
		admission, err := r.catalogue.admit(record.Interface, []string{role})
		if err != nil {
			return nil, errors.New("retained registration no longer satisfies its role")
		}
		out = append(out, roleCandidate{Record: record, Admission: admission[role], Role: role, RegistryRevision: state.Revision, BindingPreferences: state.BindingPreferences[record.ID][role],
			ScopeIdentity: HashContent([]byte(r.path)), CatalogueIdentity: r.catalogue.identity, ConfigIdentity: config.loaded.hash})
	}
	return out, nil
}

func validPreference(value json.Number) bool {
	return jsonvalue.IsNumber(value) && jsonvalue.CheckNumericWork(value) == nil
}

func decodeRolePreferences(raw json.RawMessage) (map[string]json.Number, error) {
	var values map[string]any
	if err := jsonvalue.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, errors.New("rolePreferences must be a numeric map, not null")
	}
	result := map[string]json.Number{}
	for key, value := range values {
		number, ok := value.(json.Number)
		if !ok || !validPreference(number) {
			return nil, errors.New("role preference must be a supported JSON number")
		}
		result[key] = number
	}
	return result, nil
}

func readRoleState(config *EnvConfig) (*roleRegistryState, error) {
	if len(config.DelegateRegistry) == 0 {
		if len(config.Delegates) != 0 {
			return nil, errors.New("legacy delegates require explicit migration")
		}
		return nil, nil
	}
	if len(config.Delegates) != 0 {
		return nil, errors.New("mixed legacy and role registry state")
	}
	if len(config.DelegateRegistry) > maxDelegateRegistryBytes {
		return nil, errors.New("registry exceeds OB capacity")
	}
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(config.DelegateRegistry, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid registry state")
	}
	for key := range fields {
		if !slices.Contains([]string{"format", "catalogue", "revision", "issuance", "next", "records", "bindingPreferences", "migrationReceipt"}, key) {
			return nil, errors.New("unsupported registry field")
		}
	}
	var state roleRegistryState
	if err := jsonvalue.Unmarshal(config.DelegateRegistry, &state); err != nil {
		return nil, errors.New("invalid registry state")
	}
	if state.Format != roleRegistryFormat || state.Catalogue == "" || state.Revision == 0 || state.Records == nil || state.BindingPreferences == nil {
		return nil, errors.New("unsupported or incomplete registry state")
	}
	// encoding/json's Number destination accepts quoted numeric strings. Do
	// not let that convenience coerce persisted native preferences either.
	var native map[string]map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(fields["bindingPreferences"], &native); err != nil {
		return nil, errors.New("invalid native preferences")
	}
	for id, roles := range native {
		if roles == nil {
			return nil, errors.New("invalid native role preference map")
		}
		for role, raw := range roles {
			values, err := decodeRolePreferences(raw)
			if err != nil {
				return nil, err
			}
			state.BindingPreferences[id][role] = values
		}
	}
	if _, err := hex.DecodeString(state.Issuance); err != nil || len(state.Issuance) != 32 {
		return nil, errors.New("invalid issuance namespace")
	}
	if n, err := strconv.ParseUint(state.Next, 10, 64); err != nil || n == 0 || strconv.FormatUint(n, 10) != state.Next {
		return nil, errors.New("invalid issuance sequence")
	}
	if len(state.Records) > maxDelegateRecords {
		return nil, errors.New("registry exceeds record capacity")
	}
	seen := map[string]DelegateRegistration{}
	for _, record := range state.Records {
		if record.ID == "" || len(record.Interface) > maxDelegateInterfaceBytes || len(record.Roles) == 0 || len(record.Roles) > maxDelegateRoles || record.RolePreferences == nil {
			return nil, errors.New("invalid registration state")
		}
		if _, duplicate := seen[record.ID]; duplicate {
			return nil, errors.New("duplicate registration ID")
		}
		seen[record.ID] = record
		if strings.HasPrefix(record.ID, "dlg_"+state.Issuance+"_") {
			seq, err := strconv.ParseUint(strings.TrimPrefix(record.ID, "dlg_"+state.Issuance+"_"), 10, 64)
			next, _ := strconv.ParseUint(state.Next, 10, 64)
			if err != nil || seq == 0 || seq >= next {
				return nil, errors.New("invalid retained issuance state")
			}
		}
		roles := map[string]bool{}
		for _, role := range record.Roles {
			if role == "" || roles[role] {
				return nil, errors.New("invalid retained roles")
			}
			roles[role] = true
		}
		for role, value := range record.RolePreferences {
			if !roles[role] || !validPreference(value) {
				return nil, errors.New("invalid retained preference")
			}
		}
		if _, err := openbindings.ValidateDocument(record.Interface); err != nil {
			return nil, errors.New("invalid retained interface")
		}
	}
	for id, roles := range state.BindingPreferences {
		record, ok := seen[id]
		if !ok {
			return nil, errors.New("orphan native preferences")
		}
		for role, specs := range roles {
			if !slices.Contains(record.Roles, role) {
				return nil, errors.New("orphan native role preference")
			}
			for spec, value := range specs {
				if spec == "" || !validPreference(value) {
					return nil, errors.New("invalid native preference")
				}
			}
		}
	}
	return &state, nil
}

func newRoleState(catalogue string) (*roleRegistryState, error) {
	var namespace [16]byte
	if _, err := rand.Read(namespace[:]); err != nil {
		return nil, err
	}
	return &roleRegistryState{Format: roleRegistryFormat, Catalogue: catalogue, Issuance: hex.EncodeToString(namespace[:]), Next: "1", Records: []DelegateRegistration{}, BindingPreferences: map[string]map[string]map[string]json.Number{}}, nil
}

// Supported backup recovery calls this before publishing restored state. Old
// record IDs stay stable; future IDs cannot reuse the abandoned timeline.
func rotateRoleIssuance(state *roleRegistryState) error {
	fresh, err := newRoleState(state.Catalogue)
	if err != nil {
		return err
	}
	if fresh.Issuance == state.Issuance {
		return errors.New("issuance namespace collision")
	}
	for _, record := range state.Records {
		if strings.HasPrefix(record.ID, "dlg_"+fresh.Issuance+"_") {
			return errors.New("issuance namespace collision")
		}
	}
	state.Issuance, state.Next = fresh.Issuance, "1"
	return nil
}

func retainRoleState(config *EnvConfig, state *roleRegistryState) error {
	if state.Revision == math.MaxUint64 {
		return errors.New("registry revision exhausted")
	}
	state.Revision++
	raw, err := jsonvalue.Marshal(state)
	if err != nil || len(raw) > maxDelegateRegistryBytes {
		return errors.New("registry exceeds OB capacity")
	}
	// Validate the complete prospective state before changing the disk document.
	check := &EnvConfig{DelegateRegistry: raw}
	if _, err := readRoleState(check); err != nil {
		return err
	}
	config.DelegateRegistry = raw
	return nil
}

func (r *roleRegistry) list(role string) ([]DelegateRegistration, error) {
	config, err := LoadEnvConfig(r.path)
	if err != nil {
		return nil, err
	}
	state, err := readRoleState(config)
	if err != nil {
		return nil, err
	}
	out := []DelegateRegistration{}
	if state != nil {
		for _, record := range state.Records {
			if role == "" || slices.Contains(record.Roles, role) {
				out = append(out, record)
			}
		}
	}
	// readRoleState decodes a fresh document; nothing returned aliases manager
	// storage. Full records are retained even when filtering by one role.
	return out, nil
}

func (r *roleRegistry) register(input RoleRegistrationInput) (*DelegateRegistration, error) {
	// Snapshot callers' mutable slices before entering the persistence boundary.
	raw, err := jsonvalue.Marshal(input)
	if err != nil || len(raw) > maxDelegateRegistryBytes {
		return nil, errors.New("invalid registration input")
	}
	var request RoleRegistrationInput
	if err := jsonvalue.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	return mutateEnvConfig(r.path, func(config *EnvConfig) (*DelegateRegistration, error) {
		state, err := readRoleState(config)
		if err != nil {
			return nil, err
		}
		if state == nil {
			state, err = newRoleState(r.catalogue.identity)
			if err != nil {
				return nil, err
			}
		}
		if state.Catalogue != r.catalogue.identity {
			return nil, errors.New("role catalogue changed; explicit registry review is required")
		}
		if _, err := r.catalogue.admit(request.Interface, request.Roles); err != nil {
			return nil, err
		}
		index := -1
		for i := range state.Records {
			if state.Records[i].ID == request.ID {
				index = i
				break
			}
		}
		if request.ID != "" && index < 0 {
			return nil, errors.New("unknown registration ID")
		}
		preferences := map[string]json.Number{}
		if len(request.RolePreferences) != 0 {
			preferences, err = decodeRolePreferences(request.RolePreferences)
			if err != nil {
				return nil, err
			}
		} else if index >= 0 {
			for role, value := range state.Records[index].RolePreferences {
				if slices.Contains(request.Roles, role) {
					preferences[role] = value
				}
			}
		}
		for role, value := range preferences {
			if !slices.Contains(request.Roles, role) || !validPreference(value) {
				return nil, errors.New("invalid role preference")
			}
		}
		record := DelegateRegistration{ID: request.ID, Interface: request.Interface, Roles: request.Roles, RolePreferences: preferences}
		if index >= 0 {
			state.Records[index] = record
			for role := range state.BindingPreferences[record.ID] {
				if !slices.Contains(record.Roles, role) {
					delete(state.BindingPreferences[record.ID], role)
				}
			}
		} else {
			if len(state.Records) >= maxDelegateRecords {
				return nil, errors.New("registry exceeds record capacity")
			}
			seq, _ := strconv.ParseUint(state.Next, 10, 64)
			if seq == math.MaxUint64 {
				return nil, errors.New("registration issuance exhausted")
			}
			record.ID = "dlg_" + state.Issuance + "_" + state.Next
			state.Next = strconv.FormatUint(seq+1, 10)
			state.Records = append(state.Records, record)
		}
		if err := retainRoleState(config, state); err != nil {
			return nil, err
		}
		return &record, nil
	})
}

func (r *roleRegistry) prefer(id, role string, preference *json.Number) error {
	if preference != nil && !validPreference(*preference) {
		return errors.New("invalid numeric preference")
	}
	_, err := mutateEnvConfig(r.path, func(config *EnvConfig) (struct{}, error) {
		state, err := readRoleState(config)
		if err != nil {
			return struct{}{}, err
		}
		if state != nil {
			for i := range state.Records {
				record := &state.Records[i]
				if record.ID != id {
					continue
				}
				if !slices.Contains(record.Roles, role) {
					return struct{}{}, errors.New("registration is not enrolled in role")
				}
				// Administrative edits remain possible for withdrawn memberships.
				if preference == nil {
					delete(record.RolePreferences, role)
				} else {
					record.RolePreferences[role] = *preference
				}
				return struct{}{}, retainRoleState(config, state)
			}
		}
		return struct{}{}, errors.New("unknown registration ID")
	})
	return err
}

func (r *roleRegistry) unregister(id string) error {
	_, err := mutateEnvConfig(r.path, func(config *EnvConfig) (struct{}, error) {
		state, err := readRoleState(config)
		if err != nil {
			return struct{}{}, err
		}
		if state != nil {
			for i := range state.Records {
				if state.Records[i].ID != id {
					continue
				}
				state.Records = append(state.Records[:i], state.Records[i+1:]...)
				delete(state.BindingPreferences, id)
				return struct{}{}, retainRoleState(config, state)
			}
		}
		return struct{}{}, errEnvConfigNoop
	})
	return err
}

// preferBindingSpec is OB-native routing configuration, deliberately separate
// from the portable rolePreferences map. It cannot enroll a role implicitly.
func (r *roleRegistry) preferBindingSpec(id, role, token string, preference *json.Number) error {
	if token == "" || (preference != nil && !validPreference(*preference)) {
		return errors.New("exact binding specification and valid preference required")
	}
	_, err := mutateEnvConfig(r.path, func(config *EnvConfig) (struct{}, error) {
		state, err := readRoleState(config)
		if err != nil {
			return struct{}{}, err
		}
		if state != nil {
			for _, record := range state.Records {
				if record.ID != id {
					continue
				}
				if !slices.Contains(record.Roles, role) {
					return struct{}{}, errors.New("registration is not enrolled in role")
				}
				if preference == nil {
					delete(state.BindingPreferences[id][role], token)
				} else {
					if state.BindingPreferences[id] == nil {
						state.BindingPreferences[id] = map[string]map[string]json.Number{}
					}
					if state.BindingPreferences[id][role] == nil {
						state.BindingPreferences[id][role] = map[string]json.Number{}
					}
					state.BindingPreferences[id][role][token] = *preference
				}
				return struct{}{}, retainRoleState(config, state)
			}
		}
		return struct{}{}, errors.New("unknown registration ID")
	})
	return err
}
