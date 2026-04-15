package codegen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// Generate converts an OBI Interface into a CodegenResult suitable for emitters.
func Generate(iface *openbindings.Interface) (*CodegenResult, error) {
	if iface == nil {
		return nil, fmt.Errorf("nil interface")
	}
	if len(iface.Operations) == 0 {
		return nil, fmt.Errorf("interface has no operations")
	}

	// Marshal the interface to a raw map for $ref resolution.
	rawBytes, err := json.Marshal(iface)
	if err != nil {
		return nil, fmt.Errorf("marshal interface: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(rawBytes, &root); err != nil {
		return nil, fmt.Errorf("unmarshal interface: %w", err)
	}

	conv := newSchemaConverter(root)

	// Pre-convert all named schemas so $ref targets are in the registry.
	schemaKeys := make([]string, 0, len(iface.Schemas))
	for k := range iface.Schemas {
		schemaKeys = append(schemaKeys, k)
	}
	sort.Strings(schemaKeys)

	for _, name := range schemaKeys {
		ref := "#/schemas/" + name
		if _, ok := conv.registry[ref]; ok {
			continue
		}
		if iface.Schemas[name] == nil {
			continue
		}
		conv.resolveRef(ref)
	}

	// Convert operations.
	opKeys := make([]string, 0, len(iface.Operations))
	for k := range iface.Operations {
		opKeys = append(opKeys, k)
	}
	sort.Strings(opKeys)

	var ops []OperationSig
	for _, key := range opKeys {
		op := iface.Operations[key]
		sig := OperationSig{
			Key:         key,
			Description: op.Description,
			Deprecated:  op.Deprecated,
			Tags:        op.Tags,
		}

		pathPrefix := toPascalCase(key)

		// Convert input schema.
		if op.Input != nil && len(op.Input) > 0 {
			inputRef := conv.convert(op.Input, pathPrefix+"Input")
			sig.Input = &inputRef
		}

		// Convert output schema.
		if op.Output != nil && len(op.Output) > 0 {
			outputRef := conv.convert(op.Output, pathPrefix+"Output")
			sig.Output = &outputRef
		}

		ops = append(ops, sig)
	}

	// Collect all types: registry entries first (named schemas), then generated inline types.
	typeMap := make(map[string]TypeDef)
	for _, td := range conv.types {
		typeMap[td.Name] = td
	}
	for _, td := range conv.registry {
		if td != nil {
			typeMap[td.Name] = *td
		}
	}

	typeNames := make([]string, 0, len(typeMap))
	for k := range typeMap {
		typeNames = append(typeNames, k)
	}
	sort.Strings(typeNames)

	types := make([]TypeDef, 0, len(typeNames))
	for _, name := range typeNames {
		types = append(types, typeMap[name])
	}

	// Build contract OBI: operations + schemas only, minified.
	contractOBI := buildContractOBI(iface)

	// Derive interface name.
	interfaceName := "Client"
	if iface.Name != "" {
		interfaceName = toPascalCase(iface.Name)
	}

	return &CodegenResult{
		InterfaceName: interfaceName,
		Description:   iface.Description,
		Types:         types,
		Operations:    ops,
		RawOBI:        contractOBI,
	}, nil
}

// buildContractOBI produces a minified JSON containing the fields needed
// for the embedded interface contract baked into a codegenned client.
//
// For HTTP-fetchable OBIs (openapi/asyncapi/graphql with http(s) source
// locations), the embedded contract contains only operations + schemas
// — the runtime client re-fetches the live OBI from the URL passed to
// `connect()` and uses that for binding dispatch. Stripping bindings
// keeps the codegen output compact (the live OBI is the source of
// truth at runtime).
//
// For non-HTTP-fetchable OBIs (workers-rpc, usage exec, etc), the
// embedded contract MUST include bindings + sources. The runtime
// client can't fetch the OBI from a symbolic URL like
// `workers-rpc://service-name`, so the codegen output is the only
// place these come from. The InterfaceClient.resolve() fallback path
// detects non-HTTP URLs and uses the embedded interface directly,
// so dispatch needs the bindings to be present.
//
// The decision is per-OBI: if ANY source has a non-HTTP location, all
// bindings + sources are embedded. This is conservative — mixed-transport
// OBIs (rare) embed everything rather than risking partial gaps.
func buildContractOBI(iface *openbindings.Interface) []byte {
	contract := map[string]any{
		"openbindings": iface.OpenBindings,
	}
	if iface.Name != "" {
		contract["name"] = iface.Name
	}

	// Operations: strip to schema-relevant fields only.
	ops := make(map[string]any, len(iface.Operations))
	for k, op := range iface.Operations {
		entry := map[string]any{}
		if op.Description != "" {
			entry["description"] = op.Description
		}
		if op.Deprecated {
			entry["deprecated"] = true
		}
		if op.Idempotent != nil {
			entry["idempotent"] = *op.Idempotent
		}
		if op.Input != nil {
			entry["input"] = op.Input
		}
		if op.Output != nil {
			entry["output"] = op.Output
		}
		if len(op.Tags) > 0 {
			entry["tags"] = op.Tags
		}
		ops[k] = entry
	}
	contract["operations"] = ops

	if len(iface.Schemas) > 0 {
		schemas := make(map[string]any, len(iface.Schemas))
		for k, v := range iface.Schemas {
			schemas[k] = v
		}
		contract["schemas"] = schemas
	}

	// Embed bindings + sources when any source uses a non-HTTP transport
	// (the runtime can't fetch a symbolic URL like workers-rpc://service).
	if hasNonHTTPSource(iface) {
		if len(iface.Bindings) > 0 {
			contract["bindings"] = iface.Bindings
		}
		if len(iface.Sources) > 0 {
			contract["sources"] = iface.Sources
		}
		if len(iface.Transforms) > 0 {
			contract["transforms"] = iface.Transforms
		}
	}

	b, err := json.Marshal(contract)
	if err != nil {
		// Should not happen with well-formed data.
		return []byte("{}")
	}
	return b
}

// hasNonHTTPSource reports whether the interface has any source whose
// format is known to NOT support runtime OBI re-fetching. Used to decide
// whether the embedded contract must include bindings + sources for
// runtime dispatch.
//
// The discriminator is the format TOKEN, not the source's location field.
// At codegen time the location can be anything the user is reading from
// (often a relative file path like `./openapi.json`); at runtime the
// codegen client expects a live URL to be passed to `connect()`, which
// triggers a fresh fetch. For most formats, that fetch happens over HTTP
// against the well-known endpoint and provides the live bindings, so the
// embedded contract can omit them.
//
// For formats where the runtime URL is symbolic (workers-rpc://service-name)
// or the transport is process-local (exec:my-cli), there is no runtime
// fetch and bindings MUST be embedded by codegen or dispatch fails with
// "no binding for operation: X".
//
// We check by EXCLUSION: known non-fetchable format prefixes return true.
// Anything else falls through to the historical "strip bindings" behavior.
// Adding a new non-fetchable format requires extending the switch below.
func hasNonHTTPSource(iface *openbindings.Interface) bool {
	for _, src := range iface.Sources {
		if isSymbolicURLFormat(src.Format) {
			return true
		}
	}
	return false
}

// isSymbolicURLFormat reports whether a binding format uses a symbolic
// URL at runtime (one the SDK can't HTTP-fetch). For these formats,
// codegen MUST embed bindings + sources because the runtime client has
// no URL to refetch from.
func isSymbolicURLFormat(format string) bool {
	if format == "" {
		return false
	}
	// Strip the version suffix to get the base format token. Format
	// strings look like "openapi@3.1", "workers-rpc@^1.0.0", "usage@2.0.0".
	base := format
	if at := strings.IndexByte(format, '@'); at >= 0 {
		base = format[:at]
	}
	switch base {
	case "workers-rpc", "usage":
		return true
	default:
		return false
	}
}

// SanitizePackageName produces a valid Go package name from an interface name.
func SanitizePackageName(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	result := b.String()
	if result == "" {
		return "client"
	}
	// Must start with a letter.
	if result[0] >= '0' && result[0] <= '9' {
		result = "pkg" + result
	}
	return result
}
