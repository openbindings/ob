package mcpbridge

import (
	"testing"

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
	count := RegisterInterface(srv, iface, invoker, nil)
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
	count := RegisterInterface(srv, iface, invoker, nil)
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
	count := RegisterInterface(srv, iface, invoker, nil)
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
	count := RegisterInterface(srv, iface, invoker, nil)
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

func TestBuildInputSchema_Empty(t *testing.T) {
	schema := buildInputSchema(nil)
	m, ok := schema.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any")
	}
	if m["type"] != "object" {
		t.Fatalf("expected type=object, got %v", m["type"])
	}
}

func TestBuildInputSchema_WithType(t *testing.T) {
	input := openbindings.JSONSchema{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}}
	schema := buildInputSchema(input)
	m := schema.(map[string]any)
	if m["type"] != "object" {
		t.Fatal("type should be preserved")
	}
	if m["properties"] == nil {
		t.Fatal("properties should be preserved")
	}
}

func TestBuildInputSchema_MissingType(t *testing.T) {
	input := openbindings.JSONSchema{"properties": map[string]any{"a": map[string]any{"type": "string"}}}
	schema := buildInputSchema(input)
	m := schema.(map[string]any)
	if m["type"] != "object" {
		t.Fatal("type should be injected as 'object'")
	}
}
