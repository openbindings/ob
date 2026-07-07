package codegen

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

func TestGenerateDemoOBI(t *testing.T) {
	iface := loadTestInterface(t, "../demo/api/openbindings.json")

	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if result.InterfaceName != "OpenBlendings" {
		t.Errorf("InterfaceName = %q, want %q", result.InterfaceName, "OpenBlendings")
	}

	// Expect 6 operations (including placeAndTrack graph operation).
	if len(result.Operations) != 6 {
		t.Errorf("len(Operations) = %d, want 6", len(result.Operations))
	}

	// Expect known types.
	typeNames := make(map[string]bool)
	for _, td := range result.Types {
		typeNames[td.Name] = true
	}
	for _, want := range []string{"MenuItem", "MenuResponse", "SizePrice", "PlaceOrderInput", "PlaceOrderOutput"} {
		if !typeNames[want] {
			t.Errorf("missing type %q", want)
		}
	}

	// Operations should be sorted.
	for i := 1; i < len(result.Operations); i++ {
		if result.Operations[i].Key < result.Operations[i-1].Key {
			t.Errorf("operations not sorted: %q < %q", result.Operations[i].Key, result.Operations[i-1].Key)
		}
	}
}

func TestGenerateCLIOBI(t *testing.T) {
	iface := loadTestInterface(t, "../app/ob.bound.obi.json")

	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(result.Operations) < 10 {
		t.Errorf("expected many operations, got %d", len(result.Operations))
	}

	// Should have types (CLI OBI has inline schemas).
	if len(result.Types) == 0 {
		t.Error("expected types from inline schemas")
	}
}

func TestEmitTypeScriptDemo(t *testing.T) {
	iface := loadTestInterface(t, "../demo/api/openbindings.json")
	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	code := EmitTypeScript(result)

	// Imports the signature constructor from the SDK.
	if !strings.Contains(code, `import { operationSignature } from "@openbindings/sdk"`) {
		t.Error("missing operationSignature import from @openbindings/sdk")
	}

	// Shared schema interfaces are still emitted (named schemas reused, not duplicated).
	if !strings.Contains(code, "export interface MenuItem") {
		t.Error("missing MenuItem interface")
	}

	// The OperationSignatures namespace: an immutable const of branded signatures.
	if !strings.Contains(code, "export const OperationSignatures = {") {
		t.Error("missing OperationSignatures const")
	}
	if !strings.Contains(code, "} as const;") {
		t.Error("OperationSignatures should be `as const` (immutable)")
	}
	if !strings.Contains(code, "getMenu: operationSignature<") {
		t.Error("missing typed getMenu signature")
	}
	if !strings.Contains(code, `>("getMenu")`) {
		t.Error(`getMenu signature should be built with operationSignature(..., "getMenu")`)
	}

	// Greenfield strip: none of the removed surface (bound invoker interface,
	// factory, per-call opts, embedded contract, thrown error class) appears.
	for _, banned := range []string{
		"InvokerCallOpts",
		"OpenBlendingsInvoker",
		"createOpenBlendingsInvoker",
		"export const CONTRACT",
		"const INTERFACE",
		"Stream(",
		"class OperationError",
	} {
		if strings.Contains(code, banned) {
			t.Errorf("generated code must not contain %q in the signature model", banned)
		}
	}
}

func TestEmitGoDemo(t *testing.T) {
	iface := loadTestInterface(t, "../demo/api/openbindings.json")
	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	code := EmitGo(result, "")

	// Should have correct package name.
	if !strings.Contains(code, "package openblendings") {
		t.Error("wrong package name")
	}

	// Shared schema structs are still emitted (named schemas reused, not duplicated).
	if !strings.Contains(code, "type MenuItem struct") {
		t.Error("missing MenuItem struct")
	}

	// The OperationSignatures namespace: a named struct type plus the
	// package-level value built through the SDK constructor.
	if !strings.Contains(code, "type operationSignatures struct") {
		t.Error("missing operationSignatures namespace type")
	}
	if !strings.Contains(code, "var OperationSignatures = operationSignatures{") {
		t.Error("missing OperationSignatures namespace value")
	}
	if !strings.Contains(code, "GetMenu openbindings.OperationSignature[") {
		t.Error("missing typed GetMenu signature field")
	}
	if !strings.Contains(code, "openbindings.NewOperationSignature[") || !strings.Contains(code, `]("getMenu")`) {
		t.Error(`GetMenu signature should be built with NewOperationSignature(..., "getMenu")`)
	}

	// Greenfield strip: none of the removed surface (bound invoker, per-op
	// methods, per-call opts struct, embedded contract, old args type) appears.
	for _, banned := range []string{
		"InvokerCallOpts",
		"OpenBlendingsInvoker",
		"func New",
		"func (inv",
		"OperationInvocationArgs",
		"mustParseInterface",
		"interfaceJSON",
		"Contract()",
		"Stream(ctx",
	} {
		if strings.Contains(code, banned) {
			t.Errorf("generated code must not contain %q in the signature model", banned)
		}
	}
}

func TestEmitGoPackageOverride(t *testing.T) {
	iface := loadTestInterface(t, "../demo/api/openbindings.json")
	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	code := EmitGo(result, "myapi")

	if !strings.Contains(code, "package myapi") {
		t.Error("package override not applied")
	}
}

// TestCodegenNameOverride verifies that x-ob.codegenName renames the emitted
// symbol (and its I/O type names) while leaving the operation key untouched, and
// that an operation without the override keeps the verbose full-key derivation.
func TestCodegenNameOverride(t *testing.T) {
	iface := &openbindings.Interface{
		Name: "binding-invoker",
		Operations: map[string]openbindings.Operation{
			// Overridden: verbose key, friendly symbol name, inline input so the
			// I/O type name follows the override too.
			"openbindings.binding-invoker.invokeBinding": {
				Input: openbindings.JSONSchema{
					"type":       "object",
					"properties": map[string]any{"format": map[string]any{"type": "string"}},
				},
				LosslessFields: openbindings.LosslessFields{
					Extensions: map[string]json.RawMessage{
						"x-ob": json.RawMessage(`{"codegenName":"invokeBinding"}`),
					},
				},
			},
			// No override: keeps the verbose full-key derivation.
			"openbindings.binding-invoker.listFormats": {
				Output: openbindings.JSONSchema{"type": "object"},
			},
		},
	}

	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// IR: Name carries the override; Key is always the raw key.
	for _, op := range result.Operations {
		switch op.Key {
		case "openbindings.binding-invoker.invokeBinding":
			if op.Name != "invokeBinding" {
				t.Errorf("override op Name = %q, want %q", op.Name, "invokeBinding")
			}
		case "openbindings.binding-invoker.listFormats":
			if op.Name != op.Key {
				t.Errorf("default op Name = %q, want the key %q", op.Name, op.Key)
			}
		}
	}

	const rawKey = `"openbindings.binding-invoker.invokeBinding"`

	goCode := EmitGo(result, "binvoker")
	// Friendly member + friendly I/O type from the override.
	if !strings.Contains(goCode, "InvokeBinding openbindings.OperationSignature[") {
		t.Error("Go: missing overridden InvokeBinding member")
	}
	if !strings.Contains(goCode, "type InvokeBindingInput struct") {
		t.Error("Go: I/O type name should follow the override (InvokeBindingInput)")
	}
	// The key string in the constructor stays raw.
	if !strings.Contains(goCode, "NewOperationSignature") || !strings.Contains(goCode, rawKey) {
		t.Errorf("Go: constructor must carry the raw key %s", rawKey)
	}
	// The verbose derivation must NOT appear for the overridden op...
	if strings.Contains(goCode, "OpenbindingsBindingInvokerInvokeBinding") {
		t.Error("Go: override should replace the verbose OpenbindingsBindingInvokerInvokeBinding")
	}
	// ...but the un-overridden op keeps it.
	if !strings.Contains(goCode, "OpenbindingsBindingInvokerListFormats openbindings.OperationSignature[") {
		t.Error("Go: un-overridden op should keep the verbose full-key symbol")
	}

	tsCode := EmitTypeScript(result)
	if !strings.Contains(tsCode, "invokeBinding: operationSignature<") {
		t.Error("TS: missing overridden invokeBinding member")
	}
	if !strings.Contains(tsCode, "export interface InvokeBindingInput") {
		t.Error("TS: I/O type name should follow the override (InvokeBindingInput)")
	}
	if !strings.Contains(tsCode, rawKey) {
		t.Errorf("TS: constructor must carry the raw key %s", rawKey)
	}
	if strings.Contains(tsCode, "openbindingsBindingInvokerInvokeBinding") {
		t.Error("TS: override should replace the verbose openbindingsBindingInvokerInvokeBinding")
	}
	if !strings.Contains(tsCode, "openbindingsBindingInvokerListFormats: operationSignature<") {
		t.Error("TS: un-overridden op should keep the verbose full-key symbol")
	}
}

// TestGenerateRejectsDuplicateSymbols verifies the guard against two operations
// generating the same symbol — whether via coincident codegenName overrides or
// distinct keys that PascalCase to the same identifier.
func TestGenerateRejectsDuplicateSymbols(t *testing.T) {
	// Two overrides collide on the same symbol.
	dupOverride := &openbindings.Interface{
		Name: "svc",
		Operations: map[string]openbindings.Operation{
			"acme.a": {LosslessFields: openbindings.LosslessFields{Extensions: map[string]json.RawMessage{
				"x-ob": json.RawMessage(`{"codegenName":"doThing"}`)}}},
			"acme.b": {LosslessFields: openbindings.LosslessFields{Extensions: map[string]json.RawMessage{
				"x-ob": json.RawMessage(`{"codegenName":"doThing"}`)}}},
		},
	}
	if _, err := Generate(dupOverride); err == nil {
		t.Error("expected an error when two codegenName overrides collide")
	}

	// Distinct keys that PascalCase to the same identifier (no overrides).
	dupDerived := &openbindings.Interface{
		Name: "svc",
		Operations: map[string]openbindings.Operation{
			"get-menu": {},
			"getMenu":  {},
		},
	}
	if _, err := Generate(dupDerived); err == nil {
		t.Error("expected an error when two keys derive the same symbol")
	}

	// Multiple independent collisions are all reported in a single error.
	multi := &openbindings.Interface{
		Name: "svc",
		Operations: map[string]openbindings.Operation{
			"get-menu": {}, "getMenu": {}, // -> GetMenu
			"list-x": {}, "listX": {}, // -> ListX
		},
	}
	_, err := Generate(multi)
	if err == nil {
		t.Fatal("expected an error for multiple collisions")
	}
	for _, want := range []string{"GetMenu", "ListX"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated error should name the colliding symbol %q; got: %v", want, err)
		}
	}
}

func TestSchemaConverterRefCycle(t *testing.T) {
	// Build a schema with a self-referencing $ref (tree node pattern).
	root := map[string]any{
		"schemas": map[string]any{
			"TreeNode": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{"type": "string"},
					"children": map[string]any{
						"type":  "array",
						"items": map[string]any{"$ref": "#/schemas/TreeNode"},
					},
				},
			},
		},
	}

	conv := newSchemaConverter(root)
	ref := conv.resolveRef("#/schemas/TreeNode")

	if ref.Kind != KindNamed {
		t.Fatalf("expected KindNamed, got %v", ref.Kind)
	}
	if ref.Name != "TreeNode" {
		t.Errorf("Name = %q, want %q", ref.Name, "TreeNode")
	}

	// The type should have a children field.
	var treeDef *TypeDef
	for _, td := range conv.registry {
		if td.Name == "TreeNode" {
			treeDef = td
			break
		}
	}
	if treeDef == nil {
		t.Fatal("TreeNode type not in registry")
	}

	found := false
	for _, f := range treeDef.Fields {
		if f.JSONName == "children" {
			found = true
			if f.Type.Kind != KindArray {
				t.Errorf("children kind = %v, want KindArray", f.Type.Kind)
			}
			if f.Type.Items.Kind != KindNamed || f.Type.Items.Name != "TreeNode" {
				t.Errorf("children items = %+v, want KindNamed TreeNode", f.Type.Items)
			}
		}
	}
	if !found {
		t.Error("TreeNode missing children field")
	}
}

func TestSchemaConverterAllOf(t *testing.T) {
	root := map[string]any{
		"schemas": map[string]any{
			"Base": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
				"required": []any{"id"},
			},
		},
	}

	schema := map[string]any{
		"allOf": []any{
			map[string]any{"$ref": "#/schemas/Base"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []any{"name"},
			},
		},
	}

	conv := newSchemaConverter(root)
	ref := conv.convert(schema, "Extended")

	if ref.Kind != KindNamed {
		t.Fatalf("expected KindNamed, got %v", ref.Kind)
	}

	// Find the generated type.
	var found *TypeDef
	for i := range conv.types {
		if conv.types[i].Name == ref.Name {
			found = &conv.types[i]
			break
		}
	}
	// Also check registry.
	if found == nil {
		for _, td := range conv.registry {
			if td.Name == ref.Name {
				found = td
				break
			}
		}
	}
	if found == nil {
		t.Fatalf("type %q not found", ref.Name)
	}

	fieldNames := make(map[string]bool)
	for _, f := range found.Fields {
		fieldNames[f.JSONName] = true
	}
	if !fieldNames["id"] {
		t.Error("missing 'id' field from allOf merge")
	}
	if !fieldNames["name"] {
		t.Error("missing 'name' field from allOf merge")
	}
}

func TestSchemaConverterOneOf(t *testing.T) {
	root := map[string]any{}
	schema := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}

	conv := newSchemaConverter(root)
	ref := conv.convert(schema, "MyUnion")

	if ref.Kind != KindUnion {
		t.Fatalf("expected KindUnion, got %v", ref.Kind)
	}
	if len(ref.Variants) != 2 {
		t.Errorf("expected 2 variants, got %d", len(ref.Variants))
	}
}

func TestSchemaConverterNullableType(t *testing.T) {
	root := map[string]any{}
	schema := map[string]any{
		"type": []any{"string", "null"},
	}

	conv := newSchemaConverter(root)
	ref := conv.convert(schema, "NullableString")

	if ref.Kind != KindPrimitive || ref.Primitive != "string" || !ref.Nullable {
		t.Errorf("expected nullable string, got %+v", ref)
	}
}

func TestSchemaConverterEnum(t *testing.T) {
	root := map[string]any{}
	schema := map[string]any{
		"type": "string",
		"enum": []any{"small", "medium", "large"},
	}

	conv := newSchemaConverter(root)
	ref := conv.convert(schema, "Size")

	if ref.Enum == nil || len(ref.Enum) != 3 {
		t.Errorf("expected 3 enum values, got %v", ref.Enum)
	}
}

func TestSchemaConverterMapType(t *testing.T) {
	root := map[string]any{}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": map[string]any{"type": "string"},
	}

	conv := newSchemaConverter(root)
	ref := conv.convert(schema, "StringMap")

	if ref.Kind != KindMap {
		t.Fatalf("expected KindMap, got %v", ref.Kind)
	}
	if ref.Values == nil || ref.Values.Primitive != "string" {
		t.Errorf("expected string values, got %+v", ref.Values)
	}
}

func TestSanitizePackageName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"CoffeeShop", "coffeeshop"},
		{"my-api-v2", "myapiv2"},
		{"123start", "pkg123start"},
		{"", "client"},
		{"Simple", "simple"},
	}
	for _, tt := range tests {
		got := SanitizePackageName(tt.input)
		if got != tt.want {
			t.Errorf("SanitizePackageName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"placeOrder", "PlaceOrder"},
		{"order_id", "OrderId"},
		{"get-menu", "GetMenu"},
		{"already.done", "AlreadyDone"},
		{"simple", "Simple"},
		{"", ""},
	}
	for _, tt := range tests {
		got := toPascalCase(tt.input)
		if got != tt.want {
			t.Errorf("toPascalCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestTsTypeRef_ArrayOfEnumWrapsInParens is the regression test for the
// bug where `scopes?: ("a" | "b" | "c")[]` was emitted as
// `scopes?: "a" | "b" | "c"[]` (no parens) -- TypeScript precedence
// then binds the `[]` to only `"c"`, producing `string | string |
// string[]` instead of an array of any of the three values.
func TestTsTypeRef_ArrayOfEnumWrapsInParens(t *testing.T) {
	enumRef := TypeRef{
		Kind:      KindPrimitive,
		Primitive: "string",
		Enum:      []any{"user:read", "user:write", "interface:write", "org:read", "org:write"},
	}
	arrayRef := TypeRef{
		Kind:  KindArray,
		Items: &enumRef,
	}

	got := tsTypeRef(arrayRef)
	want := `("user:read" | "user:write" | "interface:write" | "org:read" | "org:write")[]`
	if got != want {
		t.Errorf("tsTypeRef(array of enum) = %q, want %q", got, want)
	}
}

// TestTsTypeRef_ArrayOfNullableWrapsInParens covers the existing
// nullable-item case (kept passing under the new union-detection rule).
func TestTsTypeRef_ArrayOfNullableWrapsInParens(t *testing.T) {
	nullableString := TypeRef{
		Kind:      KindPrimitive,
		Primitive: "string",
		Nullable:  true,
	}
	arrayRef := TypeRef{
		Kind:  KindArray,
		Items: &nullableString,
	}

	got := tsTypeRef(arrayRef)
	want := `(string | null)[]`
	if got != want {
		t.Errorf("tsTypeRef(array of nullable) = %q, want %q", got, want)
	}
}

// TestTsTypeRef_ArrayOfPlainPrimitiveNoParens verifies the no-parens
// path for arrays of plain (non-union) types stays correct.
func TestTsTypeRef_ArrayOfPlainPrimitiveNoParens(t *testing.T) {
	arrayRef := TypeRef{
		Kind: KindArray,
		Items: &TypeRef{
			Kind:      KindPrimitive,
			Primitive: "string",
		},
	}

	got := tsTypeRef(arrayRef)
	want := `string[]`
	if got != want {
		t.Errorf("tsTypeRef(array of string) = %q, want %q", got, want)
	}
}

func TestEmptySchemaIsUnknown(t *testing.T) {
	root := map[string]any{}
	conv := newSchemaConverter(root)
	ref := conv.convert(map[string]any{}, "Empty")

	// {} (empty schema) means "accepts any value" → KindUnknown
	if ref.Kind != KindUnknown {
		t.Errorf("expected KindUnknown for empty schema, got %v", ref.Kind)
	}
}

func TestNilSchemaIsUnknown(t *testing.T) {
	root := map[string]any{}
	conv := newSchemaConverter(root)
	ref := conv.convert(nil, "Nil")

	if ref.Kind != KindUnknown {
		t.Errorf("expected KindUnknown for nil schema, got %v", ref.Kind)
	}
}

func loadTestInterface(t *testing.T, path string) *openbindings.Interface {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var iface openbindings.Interface
	if err := json.Unmarshal(data, &iface); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return &iface
}
