package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// This file is OB's realization of the shared role-scoped Delegate Manager
// interface (openbindings.delegate-manager 0.1): listRoles / registerDelegate /
// listDelegates / setDelegatePreference / unregisterDelegate, plus the OB-native
// binding-spec override. CLI and HTTP call exactly these functions; neither
// surface carries its own policy. The registry, admission, locking and
// exact-value facilities live in role_registry.go / role_admission.go.
//
// Registrations are values: the caller supplies an actual OBI document, never a
// locator to dereference. Built-in handling is not a synthetic registration.

// Sentinel classifications. Handlers map them to transport statuses; the CLI
// maps them to exit codes. They never replace the specific message.
var (
	// errRegistryUnavailable: the environment holds legacy delegate rows, mixed
	// legacy/new state, or a registry written under another role catalogue.
	// Explicit operator action (conversion, review) is required; retrying the
	// same request cannot succeed.
	errRegistryUnavailable = errors.New("delegate registry requires explicit operator action")
	// errEnvironmentUnreadable: the configuration or registry cannot be read
	// or validated, or the lock could not be acquired in bounded time.
	errEnvironmentUnreadable = errors.New("environment state is unreadable")
	// errCommitUncertain: a failure after the atomic replacement. The caller
	// must inspect state; anonymous registration must not be retried blindly.
	errCommitUncertain = errors.New("environment commit may have completed; inspect state before retrying")
	// errNoEnvironment: a mutation was requested with no initialized environment.
	errNoEnvironment = errors.New("no environment found; run 'ob init' first")
)

func IsDelegateRegistryUnavailable(err error) bool { return errors.Is(err, errRegistryUnavailable) }
func IsEnvironmentUnreadable(err error) bool       { return errors.Is(err, errEnvironmentUnreadable) }
func IsCommitUncertain(err error) bool             { return errors.Is(err, errCommitUncertain) }
func IsNoEnvironment(err error) bool               { return errors.Is(err, errNoEnvironment) }

// DelegateRoleList is listRoles' output.
type DelegateRoleList struct {
	Roles []DelegateRole `json:"roles"`
}

// Render is the human view; the JSON payload is the complete catalogue.
func (l DelegateRoleList) Render() string {
	s := Styles
	if len(l.Roles) == 0 {
		return s.Dim.Render("No delegate roles are advertised.")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Delegate roles"))
	for _, role := range l.Roles {
		fmt.Fprintf(&sb, "\n\n  %s", s.Key.Render(role.ID))
		fmt.Fprintf(&sb, "\n    %s", role.Description)
		for i, raw := range role.AcceptedInterfaces {
			keys := "unreadable expected interface"
			if iface, err := openbindings.ValidateDocument(raw); err == nil {
				ops := make([]string, 0, len(iface.Operations))
				for key := range iface.Operations {
					ops = append(ops, key)
				}
				sort.Strings(ops)
				keys = strings.Join(ops, ", ")
			}
			fmt.Fprintf(&sb, "\n    %s%s", s.Dim.Render(fmt.Sprintf("accepted interface %d: ", i+1)), keys)
		}
	}
	return sb.String()
}

// DelegateList is listDelegates' output: complete retained records.
type DelegateList struct {
	Delegates []DelegateRegistration `json:"delegates"`
}

// Render summarizes without becoming the machine payload.
func (l DelegateList) Render() string {
	s := Styles
	if len(l.Delegates) == 0 {
		return s.Dim.Render("No delegates are registered.")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Delegates"))
	for _, record := range l.Delegates {
		sb.WriteString("\n\n")
		for _, line := range strings.Split(record.Render(), "\n") {
			sb.WriteString("  " + line + "\n")
		}
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// Render is the human summary of one registration. The full value, including
// the retained OBI, is the JSON output; the summary never stands in for it.
func (r DelegateRegistration) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Registration "))
	sb.WriteString(s.Key.Render(r.ID))
	fmt.Fprintf(&sb, "\n  %s%s", s.Dim.Render("roles: "), strings.Join(r.Roles, ", "))
	prefs := make([]string, 0, len(r.RolePreferences))
	for role := range r.RolePreferences {
		prefs = append(prefs, role)
	}
	sort.Strings(prefs)
	if len(prefs) == 0 {
		fmt.Fprintf(&sb, "\n  %s%s", s.Dim.Render("explicit preferences: "), "none")
	} else {
		parts := make([]string, len(prefs))
		for i, role := range prefs {
			parts[i] = role + "=" + string(r.RolePreferences[role])
		}
		fmt.Fprintf(&sb, "\n  %s%s", s.Dim.Render("explicit preferences: "), strings.Join(parts, ", "))
	}
	name, operations := "(unnamed interface)", 0
	if iface, err := openbindings.ValidateDocument(r.Interface); err == nil {
		if iface.Name != "" {
			name = iface.Name
		}
		operations = len(iface.Operations)
	}
	fmt.Fprintf(&sb, "\n  %s%s (%d operations)", s.Dim.Render("interface: "), name, operations)
	return sb.String()
}

// activeRoleRegistry binds the built-in catalogue to the active environment.
// exists reports whether that environment has been initialized.
func activeRoleRegistry() (registry *roleRegistry, exists bool, err error) {
	catalogue, err := defaultRoleCatalogue()
	if err != nil {
		return nil, false, err
	}
	envPath, _, err := FindEnvironment()
	if err != nil {
		return nil, false, err
	}
	_, statErr := os.Stat(filepath.Join(envPath, EnvConfigFile))
	return &roleRegistry{path: envPath, catalogue: catalogue}, statErr == nil, nil
}

func requireActiveRoleRegistry() (*roleRegistry, error) {
	registry, exists, err := activeRoleRegistry()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errNoEnvironment
	}
	return registry, nil
}

// ListDelegateRoles returns the complete advertised catalogue. It needs no
// environment and performs no registry read.
func ListDelegateRoles() (*DelegateRoleList, error) {
	catalogue, err := defaultRoleCatalogue()
	if err != nil {
		return nil, err
	}
	roles, err := catalogue.list()
	if err != nil {
		return nil, err
	}
	return &DelegateRoleList{Roles: roles}, nil
}

// ListDelegates lists stored registrations, optionally filtered by exact role.
// A missing environment is an honest empty registry; legacy, mixed, corrupt or
// unsupported state is an error, never an empty inventory.
func ListDelegates(role string) (*DelegateList, error) {
	registry, exists, err := activeRoleRegistry()
	if err != nil {
		return nil, err
	}
	if !exists {
		return &DelegateList{Delegates: []DelegateRegistration{}}, nil
	}
	rows, err := registry.list(role)
	if err != nil {
		return nil, err
	}
	return &DelegateList{Delegates: rows}, nil
}

// RegisterDelegate enrolls or (with an ID) replaces a registration. The input's
// interface is the retained value; omission, {} and null preferences keep their
// distinct meanings all the way to the locked serialization point.
func RegisterDelegate(input RoleRegistrationInput) (*DelegateRegistration, error) {
	registry, err := requireActiveRoleRegistry()
	if err != nil {
		return nil, err
	}
	return registry.register(input)
}

// SetDelegatePreference sets (number) or clears (nil) one explicit role
// preference of an existing registration enrolled in that exact role.
func SetDelegatePreference(id, role string, preference *json.Number) error {
	if id == "" || role == "" {
		return errors.New("registration ID and role are required")
	}
	registry, err := requireActiveRoleRegistry()
	if err != nil {
		return err
	}
	return registry.prefer(id, role, preference)
}

// SetDelegateBindingPreference is the OB-native override keyed by registration,
// role and exact binding-specification identifier. It never touches the shared
// rolePreferences map and cannot enroll a role.
func SetDelegateBindingPreference(id, role, bindingSpec string, preference *json.Number) error {
	if id == "" || role == "" || bindingSpec == "" {
		return errors.New("registration ID, role and exact binding specification are required")
	}
	registry, err := requireActiveRoleRegistry()
	if err != nil {
		return err
	}
	return registry.preferBindingSpec(id, role, bindingSpec, preference)
}

// UnregisterDelegate ensures the registration is absent. An absent ID, including
// one in an uninitialized environment, succeeds; other records are untouched.
func UnregisterDelegate(id string) error {
	if id == "" {
		return errors.New("registration ID is required")
	}
	registry, exists, err := activeRoleRegistry()
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return registry.unregister(id)
}

// ValidDelegatePreference reports whether text is an exact JSON number OB can
// retain and compare. Native surfaces parse preference text with this check;
// no float64 conversion participates.
func ValidDelegatePreference(value json.Number) bool { return validPreference(value) }

// DelegateRegistryStatus reports the stored registration count for environment
// summaries. Legacy/mixed/corrupt state is reported as an error with guidance.
func delegateRegistryCount(envPath string) (int, error) {
	config, err := LoadEnvConfig(envPath)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", errEnvironmentUnreadable, err)
	}
	state, err := readRoleState(config)
	if err != nil {
		return 0, err
	}
	if state == nil {
		return 0, nil
	}
	return len(state.Records), nil
}

// UnmarshalJSON keeps omission, {} and null distinct and refuses shapes that
// would otherwise be coerced: a supplied empty ID is not absence, null roles
// are not an empty set, and unknown fields are not ignored.
func (in *RoleRegistrationInput) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(data, &fields); err != nil || fields == nil {
		return errors.New("registration input must be an object")
	}
	var out RoleRegistrationInput
	for key, raw := range fields {
		switch key {
		case "id":
			var id string
			if err := jsonvalue.Unmarshal(raw, &id); err != nil || id == "" {
				return errors.New("id must be a nonempty registration identifier; omit it for a fresh enrollment")
			}
			out.ID = id
		case "interface":
			trimmed := bytes.TrimSpace(raw)
			if len(trimmed) == 0 || trimmed[0] != '{' {
				return errors.New("interface must be an OpenBindings interface object, not a locator")
			}
			out.Interface = raw
		case "roles":
			var roles []string
			if err := jsonvalue.Unmarshal(raw, &roles); err != nil || roles == nil {
				return errors.New("roles must be an array of role identifiers")
			}
			out.Roles = roles
		case "rolePreferences":
			var probe map[string]json.RawMessage
			if err := jsonvalue.Unmarshal(raw, &probe); err != nil || probe == nil {
				return errors.New("rolePreferences must be a numeric map, not null")
			}
			out.RolePreferences = raw
		default:
			return fmt.Errorf("unknown registration field %q", key)
		}
	}
	if out.Interface == nil || out.Roles == nil {
		return errors.New("interface and roles are required")
	}
	*in = out
	return nil
}

// DelegatePreferenceInput is setDelegatePreference's wire input. A missing
// preference key is malformed; null clears; a number sets. Nothing is rounded.
type DelegatePreferenceInput struct {
	ID         string
	Role       string
	Preference *json.Number
}

func (in *DelegatePreferenceInput) UnmarshalJSON(data []byte) error {
	fields, err := preferenceFields(data, []string{"id", "role", "preference"})
	if err != nil {
		return err
	}
	out := DelegatePreferenceInput{ID: fields.strings["id"], Role: fields.strings["role"], Preference: fields.preference}
	*in = out
	return nil
}

// DelegateBindingPreferenceInput is the OB-native override's wire input.
type DelegateBindingPreferenceInput struct {
	ID          string
	Role        string
	BindingSpec string
	Preference  *json.Number
}

func (in *DelegateBindingPreferenceInput) UnmarshalJSON(data []byte) error {
	fields, err := preferenceFields(data, []string{"id", "role", "bindingSpec", "preference"})
	if err != nil {
		return err
	}
	*in = DelegateBindingPreferenceInput{ID: fields.strings["id"], Role: fields.strings["role"], BindingSpec: fields.strings["bindingSpec"], Preference: fields.preference}
	return nil
}

type preferenceInputFields struct {
	strings    map[string]string
	preference *json.Number
}

func preferenceFields(data []byte, names []string) (preferenceInputFields, error) {
	var raw map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(data, &raw); err != nil || raw == nil {
		return preferenceInputFields{}, errors.New("preference input must be an object")
	}
	out := preferenceInputFields{strings: map[string]string{}}
	for key, value := range raw {
		if !slices.Contains(names, key) {
			return preferenceInputFields{}, fmt.Errorf("unknown preference field %q", key)
		}
		if key == "preference" {
			var probe any
			if err := jsonvalue.Unmarshal(value, &probe); err != nil {
				return preferenceInputFields{}, errors.New("preference must be a JSON number or null")
			}
			switch number := probe.(type) {
			case nil:
				out.preference = nil
			case json.Number:
				if !validPreference(number) {
					return preferenceInputFields{}, errors.New("preference must be a finite JSON number OB can retain exactly")
				}
				out.preference = &number
			default:
				return preferenceInputFields{}, errors.New("preference must be a JSON number or null")
			}
			continue
		}
		var text string
		if err := jsonvalue.Unmarshal(value, &text); err != nil || text == "" {
			return preferenceInputFields{}, fmt.Errorf("%s must be a nonempty string", key)
		}
		out.strings[key] = text
	}
	for _, name := range names {
		if _, present := raw[name]; !present {
			return preferenceInputFields{}, fmt.Errorf("%s is required (use null to clear a preference)", name)
		}
	}
	return out, nil
}
