package mcpbridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/invoke"
)

func makeInterface(name string, ops map[string]openbindings.Operation) *openbindings.Interface {
	return &openbindings.Interface{
		OpenBindings: "0.1.0",
		Name:         name,
		Operations:   ops,
	}
}

func TestRegisterInterface_ToolsFromNonMCPBindings(t *testing.T) {
	iface := &openbindings.Interface{
		OpenBindings: "0.1.0",
		Name:         "petstore",
		Operations: map[string]openbindings.Operation{
			"listPets": {Description: "List all pets"},
			"getPet":   {Description: "Get a pet"},
		},
		Sources: map[string]openbindings.Source{
			"rest": {BindingSpec: "openbindings.openapi@1", Location: "./api.yaml"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.rest": {Operation: "listPets", Source: "rest", Ref: "#/paths/~1pets/get"},
			"getPet.rest":   {Operation: "getPet", Source: "rest", Ref: "#/paths/~1pets~1{id}/get"},
		},
	}
	srv := gomcp.NewServer(&gomcp.Implementation{Name: "test"}, nil)
	invoker := newRegistrationTestInvoker("openbindings.openapi@1")
	count := RegisterInterface(srv, iface, invoker, nil, RegisterOptions{})
	if count != 2 {
		t.Fatalf("expected 2 primitives, got %d", count)
	}
}

func TestRegisterInterface_ResourceFromMCPBinding(t *testing.T) {
	iface := &openbindings.Interface{
		OpenBindings: "0.1.0",
		Name:         "docs",
		Operations: map[string]openbindings.Operation{
			"readSpec": {Description: "Read the spec doc"},
		},
		Sources: map[string]openbindings.Source{
			"mcpServer": {BindingSpec: MCPBindingSpec, Location: "http://localhost:8080"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"readSpec.mcp": {
				Operation: "readSpec",
				Source:    "mcpServer",
				Ref:       "resources/openbindings://spec/quick-reference.md",
			},
		},
	}
	srv := gomcp.NewServer(&gomcp.Implementation{Name: "test"}, nil)
	invoker := newRegistrationTestInvoker(MCPBindingSpec)
	count := RegisterInterface(srv, iface, invoker, nil, RegisterOptions{})
	if count != 1 {
		t.Fatalf("expected 1 primitive, got %d", count)
	}
}

func TestRegisterInterface_PromptFromMCPBinding(t *testing.T) {
	iface := &openbindings.Interface{
		OpenBindings: "0.1.0",
		Name:         "assistant",
		Operations: map[string]openbindings.Operation{
			"codeReview": {
				Description: "Review code",
				Input: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"code": map[string]any{"type": "string"},
					},
				},
			},
		},
		Sources: map[string]openbindings.Source{
			"mcpServer": {BindingSpec: MCPBindingSpec, Location: "http://localhost:8080"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"codeReview.mcp": {
				Operation: "codeReview",
				Source:    "mcpServer",
				Ref:       "prompts/code_review",
			},
		},
	}
	srv := gomcp.NewServer(&gomcp.Implementation{Name: "test"}, nil)
	invoker := newRegistrationTestInvoker(MCPBindingSpec)
	count := RegisterInterface(srv, iface, invoker, nil, RegisterOptions{})
	if count != 1 {
		t.Fatalf("expected 1 primitive, got %d", count)
	}
}

func TestRegisterInterface_MixedPrimitives(t *testing.T) {
	iface := &openbindings.Interface{
		OpenBindings: "0.1.0",
		Name:         "mixed",
		Operations: map[string]openbindings.Operation{
			"callTool":    {Description: "A tool"},
			"readDoc":     {Description: "A resource"},
			"askQuestion": {Description: "A prompt"},
		},
		Sources: map[string]openbindings.Source{
			"mcpServer": {BindingSpec: MCPBindingSpec, Location: "http://localhost:8080"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"callTool.mcp":    {Operation: "callTool", Source: "mcpServer", Ref: "tools/callTool"},
			"readDoc.mcp":     {Operation: "readDoc", Source: "mcpServer", Ref: "resources/file:///doc.md"},
			"askQuestion.mcp": {Operation: "askQuestion", Source: "mcpServer", Ref: "prompts/ask"},
		},
	}
	srv := gomcp.NewServer(&gomcp.Implementation{Name: "test"}, nil)
	invoker := newRegistrationTestInvoker(MCPBindingSpec)
	count := RegisterInterface(srv, iface, invoker, nil, RegisterOptions{})
	if count != 3 {
		t.Fatalf("expected 3 primitives, got %d", count)
	}
}

type registrationTestInvoker struct {
	specs []openbindings.BindingSpecInfo
}

func newRegistrationTestInvoker(specs ...string) *invoke.OperationInvoker {
	infos := make([]openbindings.BindingSpecInfo, 0, len(specs))
	for _, spec := range specs {
		infos = append(infos, openbindings.BindingSpecInfo{BindingSpec: spec})
	}
	return invoke.NewOperationInvoker(&registrationTestInvoker{specs: infos})
}

func (i *registrationTestInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	return i.specs
}

func (i *registrationTestInvoker) InvokeBinding(ctx context.Context, _ *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	inv := invoke.NewInvocationImpl[any, any](ctx)
	inv.FireError(invoke.NewInvocationError(invoke.ErrCodeRuntime))
	return inv
}

func TestFindMCPBinding_NoMCPSource(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"rest": {BindingSpec: "openbindings.openapi@1"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"op.rest": {Operation: "op", Source: "rest", Ref: "#/paths/~1op/get"},
		},
	}
	_, kind := findMCPBinding(iface, "op")
	if kind != "tools" {
		t.Fatalf("expected 'tools' default, got %q", kind)
	}
}

func TestFindMCPBinding_RequiresExactBindingSpecIdentifier(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"legacy": {BindingSpec: "mcp"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"doc.legacy": {Operation: "doc", Source: "legacy", Ref: "resources/file:///readme.md"},
		},
	}
	ref, kind := findMCPBinding(iface, "doc")
	if ref != "" || kind != "tools" {
		t.Fatalf("non-normative shorthand was treated as MCP: ref=%q kind=%q", ref, kind)
	}
}

func TestFindMCPBinding_DoesNotInventChoiceForMultiBindingOperation(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"mcp":  {BindingSpec: MCPBindingSpec},
			"http": {BindingSpec: "openbindings.openapi@1"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"doc.mcp":  {Operation: "doc", Source: "mcp", Ref: "resources/file:///readme.md"},
			"doc.http": {Operation: "doc", Source: "http", Ref: "#/paths/~1readme/get"},
		},
	}
	ref, kind := findMCPBinding(iface, "doc")
	if ref != "" || kind != "tools" {
		t.Fatalf("multi-binding operation acquired an implicit MCP preference: ref=%q kind=%q", ref, kind)
	}
}

func TestFindMCPBinding_ResourceRef(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"mcp": {BindingSpec: MCPBindingSpec},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"doc.mcp": {Operation: "doc", Source: "mcp", Ref: "resources/file:///readme.md"},
		},
	}
	ref, kind := findMCPBinding(iface, "doc")
	if kind != "resources" {
		t.Fatalf("expected 'resources', got %q", kind)
	}
	if ref != "resources/file:///readme.md" {
		t.Fatalf("unexpected ref %q", ref)
	}
}

func TestFindMCPBinding_ResourceTemplateRef(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"mcp": {BindingSpec: MCPBindingSpec},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"log.mcp": {Operation: "log", Source: "mcp", Ref: "resourceTemplates/file:///logs/{date}"},
		},
	}
	ref, kind := findMCPBinding(iface, "log")
	if kind != "resourceTemplates" {
		t.Fatalf("expected 'resourceTemplates' (R5: its own entity, not misclassified as tools), got %q", kind)
	}
	if ref != "resourceTemplates/file:///logs/{date}" {
		t.Fatalf("unexpected ref %q", ref)
	}
}

func TestToolNames_SanitizesFullKey(t *testing.T) {
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"openbindings.ob.describe": {},
			"echo":                     {}, // bare key, e.g. an OpenAPI-derived op
		},
	}
	names := toolNames(iface)
	// The full key is preserved, only sanitized to the protocol charset — the
	// bridge assumes no key convention and strips no namespace.
	if got := names["openbindings.ob.describe"]; got != "openbindings_ob_describe" {
		t.Errorf("namespaced key: got %q, want %q", got, "openbindings_ob_describe")
	}
	if got := names["echo"]; got != "echo" {
		t.Errorf("bare key: got %q, want %q", got, "echo")
	}
}

func TestToolNames_CharsetSafeUniqueAndBounded(t *testing.T) {
	longPrefix := strings.Repeat("a", 64)
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"openbindings.kv.get":   {},
			"openbindings.blob.get": {},
			"weird key/with:chars":  {},
			longPrefix + ".x":       {},
			longPrefix + ".y":       {},
		},
	}
	names := toolNames(iface)
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate tool name %q", n)
		}
		seen[n] = true
		if len(n) == 0 || len(n) > 64 {
			t.Errorf("name %q out of length bounds", n)
		}
		for _, r := range n {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
			if !ok {
				t.Errorf("name %q contains disallowed char %q", n, r)
			}
		}
	}
	// Distinct keys that share a last segment stay distinct (no namespace stripping).
	if names["openbindings.kv.get"] == names["openbindings.blob.get"] {
		t.Error("expected distinct names for kv.get vs blob.get")
	}
}

func TestBundleInputSchema_Empty(t *testing.T) {
	m, ok := bundleInputSchema(nil, nil).(map[string]any)
	if !ok || m["type"] != "object" {
		t.Fatalf("expected {type:object}, got %v", m)
	}
}

func TestBundleInputSchema_NoRefsPassThrough(t *testing.T) {
	input := map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}}
	m := bundleInputSchema(input, nil).(map[string]any)
	if m["type"] != "object" || m["properties"] == nil {
		t.Fatalf("type+properties should be preserved, got %v", m)
	}
	if _, ok := m["$defs"]; ok {
		t.Error("did not expect $defs when there are no refs")
	}
}

func TestBundleInputSchema_BundlesAndRewritesRefs(t *testing.T) {
	schemas := map[string]openbindings.JSONSchema{
		"ValidateInput": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"interface": map[string]any{"$ref": "#/schemas/Iface"},
				"strict":    map[string]any{"type": "boolean"},
			},
			"required": []any{"interface"},
		},
		"Iface": map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		},
	}
	got := bundleInputSchema(map[string]any{"$ref": "#/schemas/ValidateInput"}, schemas).(map[string]any)

	// The top-level ref is resolved to a concrete object schema.
	if got["type"] != "object" || got["properties"] == nil {
		t.Errorf("expected resolved root object schema, got %v", got)
	}
	props := got["properties"].(map[string]any)
	if ref := props["interface"].(map[string]any)["$ref"]; ref != "#/$defs/Iface" {
		t.Errorf("nested ref should be rewritten to #/$defs/Iface, got %v", ref)
	}
	defs, _ := got["$defs"].(map[string]any)
	if _, ok := defs["Iface"]; !ok {
		t.Errorf("expected Iface bundled under $defs, got %v", defs)
	}
	// No dangling #/schemas/ refs survive anywhere in the bundled schema.
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "#/schemas/") {
		t.Errorf("expected no #/schemas/ refs after bundling, got %s", b)
	}
}

func TestBundleInputSchema_HandlesCycle(t *testing.T) {
	schemas := map[string]openbindings.JSONSchema{
		"Node": map[string]any{
			"type":       "object",
			"properties": map[string]any{"child": map[string]any{"$ref": "#/schemas/Node"}},
		},
	}
	// Must terminate despite the self-reference.
	got := bundleInputSchema(map[string]any{"$ref": "#/schemas/Node"}, schemas).(map[string]any)
	defs, _ := got["$defs"].(map[string]any)
	if _, ok := defs["Node"]; !ok {
		t.Errorf("expected Node in $defs, got %v", defs)
	}
}

func TestBundleInputSchema_PreservesBooleanSharedSchema(t *testing.T) {
	got := bundleInputSchema(map[string]any{
		"type":       "object",
		"properties": map[string]any{"never": map[string]any{"$ref": "#/schemas/Never"}},
	}, map[string]openbindings.JSONSchema{"Never": false}).(map[string]any)
	defs := got["$defs"].(map[string]any)
	if value, ok := defs["Never"]; !ok || value != false {
		t.Fatalf("boolean shared schema was not bundled: %#v", got)
	}
}

func TestBundleInputSchema_DoesNotOverwriteAuthoredDefinitions(t *testing.T) {
	got := bundleInputSchema(map[string]any{
		"type": "object",
		"$defs": map[string]any{
			"Thing": map[string]any{"type": "string"},
		},
		"properties": map[string]any{
			"local":  map[string]any{"$ref": "#/$defs/Thing"},
			"shared": map[string]any{"$ref": "#/schemas/Thing"},
		},
	}, map[string]openbindings.JSONSchema{
		"Thing": map[string]any{"type": "integer"},
	}).(map[string]any)

	defs := got["$defs"].(map[string]any)
	if defs["Thing"].(map[string]any)["type"] != "string" {
		t.Fatalf("authored definition was overwritten: %#v", defs)
	}
	sharedRef := got["properties"].(map[string]any)["shared"].(map[string]any)["$ref"]
	if sharedRef == "#/$defs/Thing" {
		t.Fatalf("shared schema collided with authored definition: %#v", got)
	}
	allocatedName := strings.TrimPrefix(sharedRef.(string), "#/$defs/")
	if defs[allocatedName].(map[string]any)["type"] != "integer" {
		t.Fatalf("shared definition was not allocated separately: %#v", got)
	}
}

func TestSchemaRefName_DecodesJSONPointerToken(t *testing.T) {
	name, ok := schemaRefName("#/schemas/A~1B~0C")
	if !ok || name != "A/B~C" {
		t.Fatalf("decoded name = %q, %v", name, ok)
	}
	if jsonPointerToken(name) != "A~1B~0C" {
		t.Fatalf("pointer token did not round trip: %q", jsonPointerToken(name))
	}
}

// neverEndingInvoker emits outputs forever: the shape of a subscription
// binding bridged into a request-scoped MCP tool call.
type neverEndingInvoker struct{}

func (n *neverEndingInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: "test-stream", Description: "unbounded stream"}}
}

func (n *neverEndingInvoker) InvokeBinding(ctx context.Context, args *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	inv := invoke.NewInvocationImpl[any, any](ctx)
	go func() {
		_ = inv.CloseInput()
		i := 0
		for {
			select {
			case <-inv.Done():
				return
			case <-time.After(5 * time.Millisecond):
				i++
				if err := inv.EmitOutput(map[string]any{"event": i}); err != nil {
					return
				}
			}
		}
	}()
	return inv
}

// A subscription-style operation cannot complete a request-scoped MCP tool
// call; the drain must terminate at the deadline with an honest refusal
// (never hang until the agent's client gives up — the field-check finding).
func TestDrainOperation_UnboundedStreamHitsDeadline(t *testing.T) {
	invoker := invoke.NewOperationInvoker(&neverEndingInvoker{})
	iface := &openbindings.Interface{
		OpenBindings: "0.2.0",
		Name:         "streams",
		Operations:   map[string]openbindings.Operation{"orderUpdates": {Description: "subscription"}},
		Sources:      map[string]openbindings.Source{"s": {BindingSpec: "test-stream", Location: "https://example.com/stream"}},
		Bindings: map[string]openbindings.BindingEntry{
			"orderUpdates.s": {Operation: "orderUpdates", Source: "s", Ref: "updates"},
		},
	}

	start := time.Now()
	call := invoke.Invoke(context.Background(), invoker, iface,
		invoke.NewOperationSignature[any, any]("orderUpdates"))
	drained := drainOperation(context.Background(), call, operationInput{}, "orderUpdates", 120*time.Millisecond)
	elapsed := time.Since(start)

	if drained.Error == nil {
		t.Fatalf("unbounded stream must terminate with a refusal, got outputs %v", drained.Outputs)
	}
	if drained.Error.Code != invoke.ErrCodeCancelled {
		t.Errorf("want ERR_CANCELLED, got %s", drained.Error.Code)
	}
	if len(drained.Outputs) == 0 {
		t.Error("already-emitted outputs must survive the terminal timeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("drain must terminate at the deadline, took %v", elapsed)
	}
}

func TestDrainMCPTool_DistinguishesNoFinalResult(t *testing.T) {
	ctx := context.Background()
	call := invoke.NewInvocationImpl[any, any](ctx)
	_ = call.CloseInput()
	call.CloseOutput()

	final, found, ierr := drainMCPTool(ctx, call, operationInput{}, "empty", time.Second, nil)
	if ierr != nil {
		t.Fatalf("empty successful stream returned an error: %v", ierr)
	}
	if found || final != nil {
		t.Fatalf("empty stream invented a final CallToolResult: final=%#v found=%v", final, found)
	}
}
