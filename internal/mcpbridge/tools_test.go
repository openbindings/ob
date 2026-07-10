package mcpbridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
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
			"rest": {Format: "openapi@3.1", Location: "./api.yaml"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"listPets.rest": {Operation: "listPets", Source: "rest", Ref: "#/paths/~1pets/get"},
			"getPet.rest":   {Operation: "getPet", Source: "rest", Ref: "#/paths/~1pets~1{id}/get"},
		},
	}
	srv := gomcp.NewServer(&gomcp.Implementation{Name: "test"}, nil)
	invoker := openbindings.NewOperationInvoker()
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
			"mcpServer": {Format: "mcp", Location: "http://localhost:8080"},
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
	invoker := openbindings.NewOperationInvoker()
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
				Input: openbindings.JSONSchema{
					"type": "object",
					"properties": map[string]any{
						"code": map[string]any{"type": "string"},
					},
				},
			},
		},
		Sources: map[string]openbindings.Source{
			"mcpServer": {Format: "mcp", Location: "http://localhost:8080"},
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
	invoker := openbindings.NewOperationInvoker()
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
			"mcpServer": {Format: "mcp", Location: "http://localhost:8080"},
		},
		Bindings: map[string]openbindings.BindingEntry{
			"callTool.mcp":    {Operation: "callTool", Source: "mcpServer", Ref: "tools/callTool"},
			"readDoc.mcp":     {Operation: "readDoc", Source: "mcpServer", Ref: "resources/file:///doc.md"},
			"askQuestion.mcp": {Operation: "askQuestion", Source: "mcpServer", Ref: "prompts/ask"},
		},
	}
	srv := gomcp.NewServer(&gomcp.Implementation{Name: "test"}, nil)
	invoker := openbindings.NewOperationInvoker()
	count := RegisterInterface(srv, iface, invoker, nil, RegisterOptions{})
	if count != 3 {
		t.Fatalf("expected 3 primitives, got %d", count)
	}
}

func TestFindMCPBinding_NoMCPSource(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"rest": {Format: "openapi@3.1"},
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

func TestFindMCPBinding_ResourceRef(t *testing.T) {
	iface := &openbindings.Interface{
		Sources: map[string]openbindings.Source{
			"mcp": {Format: "mcp"},
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
	iface := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{
			"openbindings.kv.get":   {},
			"openbindings.blob.get": {},
			"weird key/with:chars":  {},
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
	input := openbindings.JSONSchema{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}}
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
		"ValidateInput": {
			"type": "object",
			"properties": map[string]any{
				"interface": map[string]any{"$ref": "#/schemas/Iface"},
				"strict":    map[string]any{"type": "boolean"},
			},
			"required": []any{"interface"},
		},
		"Iface": {
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		},
	}
	got := bundleInputSchema(openbindings.JSONSchema{"$ref": "#/schemas/ValidateInput"}, schemas).(map[string]any)

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
		"Node": {
			"type":       "object",
			"properties": map[string]any{"child": map[string]any{"$ref": "#/schemas/Node"}},
		},
	}
	// Must terminate despite the self-reference.
	got := bundleInputSchema(openbindings.JSONSchema{"$ref": "#/schemas/Node"}, schemas).(map[string]any)
	defs, _ := got["$defs"].(map[string]any)
	if _, ok := defs["Node"]; !ok {
		t.Errorf("expected Node in $defs, got %v", defs)
	}
}

// neverEndingInvoker emits outputs forever: the shape of a subscription
// binding bridged into a request-scoped MCP tool call.
type neverEndingInvoker struct{}

func (n *neverEndingInvoker) Formats() []openbindings.FormatInfo {
	return []openbindings.FormatInfo{{Token: "test-stream", Description: "unbounded stream"}}
}

func (n *neverEndingInvoker) InvokeBinding(ctx context.Context, args *openbindings.BindingInvocationArgs) openbindings.Invocation[any, any] {
	inv := openbindings.NewInvocationImpl[any, any](ctx)
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
	invoker := openbindings.NewOperationInvoker(&neverEndingInvoker{})
	iface := &openbindings.Interface{
		OpenBindings: "0.2.0",
		Name:         "streams",
		Operations:   map[string]openbindings.Operation{"orderUpdates": {Description: "subscription"}},
		Sources:      map[string]openbindings.Source{"s": {Format: "test-stream", Location: "https://example.com/stream"}},
		Bindings: map[string]openbindings.BindingEntry{
			"orderUpdates.s": {Operation: "orderUpdates", Source: "s", Ref: "updates"},
		},
	}

	start := time.Now()
	call := openbindings.Invoke(context.Background(), invoker, iface,
		openbindings.NewOperationSignature[any, any]("orderUpdates"))
	out, ierr := drainOperation(context.Background(), call, nil, "orderUpdates", 120*time.Millisecond)
	elapsed := time.Since(start)

	if ierr == nil {
		t.Fatalf("unbounded stream must terminate with a refusal, got output %v", out)
	}
	if ierr.Code != openbindings.ErrCodeTimeout {
		t.Errorf("want ERR_TIMEOUT, got %s", ierr.Code)
	}
	for _, want := range []string{"request-scoped", "subscription-style", "event(s) collected"} {
		if !strings.Contains(ierr.Message, want) {
			t.Errorf("refusal must mention %q, got: %s", want, ierr.Message)
		}
	}
	if elapsed > 2*time.Second {
		t.Errorf("drain must terminate at the deadline, took %v", elapsed)
	}
}
