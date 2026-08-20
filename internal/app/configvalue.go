package app

import (
	"strings"

	"github.com/openbindings/openbindings-go/invoke"
)

// This file holds ob's consumer-side helpers for the binding-invoker
// contract's config.value requirement family: reading a requirement's
// (point, path, schema) carriage, shaping a resolved value into the
// configuration fragment the path addresses, and classifying alternatives
// for the keying rule (context-scope model, ratified 2026-08-19). The
// JSON-Pointer semantics mirror the SDK's configurationFragment /
// configurationValueAt exactly: the empty pointer addresses the whole
// point, '/variables/region' addresses a nested member.

// configValueCarriage reads the config.value members from a requirement's
// type-specific carriage. Unknown extra members (e.g. a legacy `choices`
// from an un-migrated engine) are ignored. ok is false when the requirement
// is not usable config.value carriage (missing point or path, or an invalid
// pointer).
func configValueCarriage(req invoke.ContextRequirement) (point, path string, schema map[string]any, ok bool) {
	if req.Type != "config.value" {
		return "", "", nil, false
	}
	point, _ = req.Extra["point"].(string)
	path, pathPresent := req.Extra["path"].(string)
	if point == "" || !pathPresent || !validConfigPointer(path) {
		return "", "", nil, false
	}
	// A schema that is present but not an object is invalid carriage; treat
	// it as absent (unconstrained) for rendering and unusable for prompting.
	schema, _ = req.Extra["schema"].(map[string]any)
	return point, path, schema, true
}

// schemaEnum returns the closed admissible set an engine-asserted schema
// declares, or nil when the schema declares none (or is absent).
func schemaEnum(schema map[string]any) []any {
	if schema == nil {
		return nil
	}
	members, _ := schema["enum"].([]any)
	if len(members) == 0 {
		return nil
	}
	return members
}

// configValueOnlyAlternative reports whether every requirement of the
// alternative is config.value — an artifact-bound configuration alternative,
// which the keying rule files and fetches under the exact asserted target
// rather than the normalized endpoint (twin of the SDK's unexported check).
func configValueOnlyAlternative(alt invoke.ContextAlternative) bool {
	if len(alt.Requirements) == 0 {
		return false
	}
	for _, req := range alt.Requirements {
		if req.Type != "config.value" {
			return false
		}
	}
	return true
}

// validConfigPointer reports whether path is a well-formed RFC 6901 JSON
// Pointer ("" or "/"-led tokens with only ~0/~1 escapes).
func validConfigPointer(path string) bool {
	if path == "" {
		return true
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	for _, token := range strings.Split(path[1:], "/") {
		for i := 0; i < len(token); i++ {
			if token[i] != '~' {
				continue
			}
			if i+1 >= len(token) || (token[i+1] != '0' && token[i+1] != '1') {
				return false
			}
		}
	}
	return true
}

func configPointerTokens(path string) []string {
	if path == "" {
		return nil
	}
	tokens := strings.Split(path[1:], "/")
	for i, token := range tokens {
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
	}
	return tokens
}

// configValueAt selects the value the pointer addresses within a point's
// value. The empty pointer addresses the whole value.
func configValueAt(root any, path string) (any, bool) {
	current := root
	for _, token := range configPointerTokens(path) {
		record, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = record[token]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// configPointValue shapes a resolved value into the value stored at
// configuration[point]: the value itself for the empty pointer, a nested
// map fragment for a non-empty one (the SDK's configurationFragment
// semantics: path "/url" and value v yield {"url": v}).
func configPointValue(path string, value any) any {
	tokens := configPointerTokens(path)
	var fragment any = value
	for i := len(tokens) - 1; i >= 0; i-- {
		fragment = map[string]any{tokens[i]: fragment}
	}
	return fragment
}

// mergeConfigFragment deep-merges right into left: nested maps merge
// member-wise, anything else replaces (twin of the SDK's
// mergeConfigurationFragment). It lets a path fragment land in a stored
// point value without clobbering sibling members.
func mergeConfigFragment(left, right map[string]any) {
	for key, value := range right {
		prior, priorOK := left[key].(map[string]any)
		incoming, incomingOK := value.(map[string]any)
		if priorOK && incomingOK {
			mergeConfigFragment(prior, incoming)
			continue
		}
		left[key] = value
	}
}
