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
	iface := loadTestInterface(t, "../app/ob.obi.json")

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

	// Should import from @openbindings/sdk.
	if !strings.Contains(code, `from "@openbindings/sdk"`) {
		t.Error("missing @openbindings/sdk import")
	}

	// Should have typed interfaces.
	if !strings.Contains(code, "export interface MenuItem") {
		t.Error("missing MenuItem interface")
	}

	// Should have client class.
	if !strings.Contains(code, "export class OpenBlendingsClient") {
		t.Error("missing client class")
	}

	// Should have unary method.
	if !strings.Contains(code, "async getMenu(") {
		t.Error("missing getMenu unary method")
	}

	// Should have stream method.
	if !strings.Contains(code, "async *getMenuStream(") {
		t.Error("missing getMenuStream method")
	}

	// Should have connect method.
	if !strings.Contains(code, "async connect(") {
		t.Error("missing connect method")
	}

	// Should have ClientOperationError.
	if !strings.Contains(code, "class ClientOperationError") {
		t.Error("missing ClientOperationError")
	}

	// Should have operations type map.
	if !strings.Contains(code, "type OpenBlendingsOperations") {
		t.Error("missing operations type map")
	}

	// Should have embedded OBI.
	if !strings.Contains(code, "const INTERFACE: OBInterface") {
		t.Error("missing embedded interface")
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

	// Should have structs.
	if !strings.Contains(code, "type MenuItem struct") {
		t.Error("missing MenuItem struct")
	}

	// Should have client.
	if !strings.Contains(code, "type OpenBlendingsClient struct") {
		t.Error("missing client struct")
	}

	// Should have constructor.
	if !strings.Contains(code, "func NewOpenBlendingsClient") {
		t.Error("missing constructor")
	}

	// Should have methods.
	if !strings.Contains(code, "func (c *OpenBlendingsClient) GetMenu") {
		t.Error("missing GetMenu method")
	}

	// Should have execUnary helper.
	if !strings.Contains(code, "func execUnary[T any]") {
		t.Error("missing execUnary helper")
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
		"type": "object",
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

func TestContractOBIStripsBindings_HTTPSources(t *testing.T) {
	// HTTP-fetchable OBI: bindings + sources are stripped from the
	// embedded contract because the runtime client re-fetches the live
	// OBI from the URL passed to connect().
	iface := loadTestInterface(t, "../demo/api/openbindings.json")
	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var contract map[string]any
	if err := json.Unmarshal(result.RawOBI, &contract); err != nil {
		t.Fatalf("unmarshal contract: %v", err)
	}

	if _, ok := contract["bindings"]; ok {
		t.Error("contract contains bindings (HTTP source — should strip)")
	}
	if _, ok := contract["sources"]; ok {
		t.Error("contract contains sources (HTTP source — should strip)")
	}
	if _, ok := contract["transforms"]; ok {
		t.Error("contract contains transforms (HTTP source — should strip)")
	}
	if _, ok := contract["operations"]; !ok {
		t.Error("contract missing operations")
	}
}

func TestContractOBIEmbedsBindings_NonHTTPSources(t *testing.T) {
	// Non-HTTP-fetchable OBI (workers-rpc): bindings + sources MUST be
	// embedded because the runtime client has no way to fetch the OBI
	// from a symbolic URL like workers-rpc://service-name. Without
	// embedding, dispatch fails with "no binding for operation: X".
	iface := &openbindings.Interface{
		OpenBindings: "0.1.0",
		Name:         "TestRpcService",
		Operations: map[string]openbindings.Operation{
			"ping": {
				Description: "health check",
			},
		},
		Sources: map[string]openbindings.Source{
			"rpc": {
				Format:   "workers-rpc@^1.0.0",
				Location: "workers-rpc://test-service",
			},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"ping.rpc": {
				Operation: "ping",
				Source:    "rpc",
				Ref:       "ping",
			},
		},
	}

	result, err := Generate(iface)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var contract map[string]any
	if err := json.Unmarshal(result.RawOBI, &contract); err != nil {
		t.Fatalf("unmarshal contract: %v", err)
	}

	if _, ok := contract["bindings"]; !ok {
		t.Error("contract missing bindings (workers-rpc source — must embed)")
	}
	if _, ok := contract["sources"]; !ok {
		t.Error("contract missing sources (workers-rpc source — must embed)")
	}

	// Spot-check that the embedded binding has the right shape.
	bindings, _ := contract["bindings"].(map[string]any)
	pingBind, _ := bindings["ping.rpc"].(map[string]any)
	if pingBind["operation"] != "ping" {
		t.Errorf("embedded binding has wrong operation: %v", pingBind["operation"])
	}
	if pingBind["ref"] != "ping" {
		t.Errorf("embedded binding has wrong ref: %v", pingBind["ref"])
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
