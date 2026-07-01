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
			Name:        key,
			Description: op.Description,
			Deprecated:  op.Deprecated,
			Tags:        op.Tags,
		}
		// An operation may override its generated symbol name via
		// x-ob.codegenName. The key stays untouched; only the emitted
		// identifier (and its I/O type names) follow the override.
		if cn := codegenName(op); cn != "" {
			sig.Name = cn
		}

		pathPrefix := toPascalCase(sig.Name)

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

	// Guard: two operations must not generate the same symbol. The emitted
	// member name (Go PascalCase / TS camelCase) and the I/O type-name prefix
	// all derive from sig.Name, so a collision would emit non-compiling code.
	// Catches both coincident x-ob.codegenName overrides and distinct keys that
	// PascalCase to the same identifier (e.g. "get-menu" vs "getMenu"). Every
	// colliding group is reported together so they can be fixed in one pass.
	bySymbol := make(map[string][]string, len(ops))
	var symbolOrder []string // first-appearance order (ops are key-sorted) for determinism
	for _, op := range ops {
		sym := toPascalCase(op.Name)
		if _, seen := bySymbol[sym]; !seen {
			symbolOrder = append(symbolOrder, sym)
		}
		bySymbol[sym] = append(bySymbol[sym], op.Key)
	}
	var collisions []string
	for _, sym := range symbolOrder {
		if keys := bySymbol[sym]; len(keys) > 1 {
			collisions = append(collisions, fmt.Sprintf("  %s <- %s", sym, strings.Join(keys, ", ")))
		}
	}
	if len(collisions) > 0 {
		return nil, fmt.Errorf("codegen: %d generated symbol(s) collide across operations; set a distinct x-ob.codegenName on one operation in each group (see `ob operation codegen-name`):\n%s",
			len(collisions), strings.Join(collisions, "\n"))
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

// codegenName returns an operation's x-ob.codegenName override, or "" if unset.
// This is the only place codegen reads ob vendor metadata: it lets an author
// pick a friendlier symbol name (e.g. "invokeBinding") for a verbose,
// namespace-prefixed operation key without touching the key itself.
func codegenName(op openbindings.Operation) string {
	raw, ok := op.Extensions["x-ob"]
	if !ok {
		return ""
	}
	var xob struct {
		CodegenName string `json:"codegenName"`
	}
	if err := json.Unmarshal(raw, &xob); err != nil {
		return ""
	}
	return xob.CodegenName
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
