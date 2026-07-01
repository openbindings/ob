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
	}, nil
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
