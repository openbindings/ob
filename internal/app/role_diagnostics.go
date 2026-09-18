package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

// RoleResolutionInput is OB's native diagnostic, not a shared Delegate Manager
// operation. A path identifies the actual selection policy being explained.
// RegistrationID instead constrains selection to one explicitly enrolled record.
type RoleResolutionInput struct {
	Role           DelegateCapability `json:"role"`
	BindingSpec    string             `json:"bindingSpec"`
	Path           string             `json:"path,omitempty"`
	RegistrationID string             `json:"registrationId,omitempty"`
}

// Preserve the distinction between an omitted selector and an invalid supplied
// value. In particular, null/empty registrationId must not enable automatic
// routing when the caller intended to constrain selection.
func (in *RoleResolutionInput) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(data, &fields); err != nil || fields == nil {
		return errors.New("role resolution requires an object")
	}
	var out RoleResolutionInput
	for key, raw := range fields {
		var value string
		if err := jsonvalue.Unmarshal(raw, &value); err != nil || value == "" {
			return fmt.Errorf("%s must be a nonempty string", key)
		}
		switch key {
		case "role":
			out.Role = DelegateCapability(value)
		case "bindingSpec":
			out.BindingSpec = value
		case "path":
			out.Path = value
		case "registrationId":
			out.RegistrationID = value
		default:
			return errors.New("unknown role resolution field")
		}
	}
	*in = out
	return nil
}

// RoleResolutionOutput deliberately omits provider values, source locators,
// recipient configuration and credentials. Unavailable is a successful negative
// assessment, never a swallowed registry/query error or a fallback promise.
type RoleResolutionOutput struct {
	Role           DelegateCapability `json:"role"`
	BindingSpec    string             `json:"bindingSpec"`
	Path           string             `json:"path"`
	Available      bool               `json:"available"`
	Builtin        bool               `json:"builtin"`
	RegistrationID string             `json:"registrationId,omitempty"`
}

func (r RoleResolutionOutput) Render() string {
	winner := "unavailable"
	if r.Available {
		if r.Builtin {
			winner = "built-in"
		} else {
			winner = r.RegistrationID
		}
	}
	return fmt.Sprintf("Role: %s\nBinding specification: %s\nPath: %s\nSelected: %s", r.Role, r.BindingSpec, r.Path, winner)
}

// resolveRoleSelection is also the explicit-provider application seam. Callers
// invoking work keep the returned route; they must not diagnose and then elect
// again. It never promotes source metadata to an execution authority.
func resolveRoleSelection(ctx context.Context, input RoleResolutionInput) (*roleSelection, string, error) {
	path := roleRoutingPath(input.Path)
	if input.RegistrationID != "" {
		if input.Path != "" && input.Path != "explicit" {
			return nil, "", errors.New("an explicit registration cannot also request ranked or native-first selection")
		}
		selected, err := selectInstalledRolesFrom(ctx, input.Role, []string{input.BindingSpec}, roleRanked, input.RegistrationID)
		return selected[input.BindingSpec], "explicit", err
	}
	if path == "" {
		path = roleNativeFirst
		if input.Role == CapInvoke {
			path = roleRanked
		}
	}
	if input.Role != CapInvoke && path != roleNativeFirst {
		return nil, "", errors.New("authoring diagnostics require the native-first path")
	}
	selected, err := selectInstalledRole(ctx, input.Role, input.BindingSpec, path)
	return selected, string(path), err
}

// ResolveRoleDelegate assesses support using the same retained role engine as
// actual routing, but never invokes workload. It is a point-in-time diagnostic,
// not a lease, authorization grant or promise about a later request's winner.
func ResolveRoleDelegate(ctx context.Context, input RoleResolutionInput) (*RoleResolutionOutput, error) {
	selected, path, err := resolveRoleSelection(ctx, input)
	if err != nil {
		return nil, err
	}
	out := &RoleResolutionOutput{Role: input.Role, BindingSpec: input.BindingSpec, Path: path, Available: selected != nil}
	if selected != nil {
		out.Builtin = selected.Builtin
		if !selected.Builtin {
			out.RegistrationID = selected.Runtime.candidate.Record.ID
		}
	}
	return out, nil
}
