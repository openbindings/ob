package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/invoke"

	"github.com/openbindings/ob/internal/app"
)

// TestCrossSurfaceConformance executes the same contract operation and input
// through ob's generated CLI OBI and its discovered ob-start OBI. It compares
// cardinality and canonical JSON values, while the SDK operation invoker
// independently validates every output against the operation schema. The
// scenarios cover pure reads, embedded-source acquisition, an editing chain,
// and persisted context state. A difference means one transport adapter has
// changed the operation's observable meaning.
func TestCrossSurfaceConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and execs the real ob binary")
	}

	binDir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(binDir, "ob"), "./cmd/ob")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ob: %v\n%s", err, out)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Build before replacing HOME: otherwise the Go command populates the test
	// fixture directory with a module cache containing read-only files, which
	// TempDir cannot clean up.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OB_CREDENTIALS_FILE", filepath.Join(home, "credentials.json"))

	cliPath, err := filepath.Abs("../app/ob.bound.obi.json")
	if err != nil {
		t.Fatal(err)
	}
	cliData, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatal(err)
	}
	var cli openbindings.Interface
	if err := json.Unmarshal(cliData, &cli); err != nil {
		t.Fatal(err)
	}

	ts := testEnv(t)
	defer ts.Close()
	served, err := app.ResolveInterface(ts.URL)
	if err != nil {
		t.Fatalf("resolve served OBI: %v", err)
	}

	docA := map[string]any{
		"openbindings": "0.2.0",
		"name":         "surface-a",
		"version":      "1.0.0",
		"description":  "Cross-surface fixture.",
		"operations": map[string]any{
			"ping": map[string]any{
				"description": "Ping.",
				"aliases":     []any{"example.ping"},
				"input":       map[string]any{"type": "object"},
				"output":      map[string]any{"type": "string"},
			},
			"pong": map[string]any{},
		},
		"sources": map[string]any{
			"api": map[string]any{
				"bindingSpec": "openbindings.openapi@1",
				"location":    "https://example.com/openapi.yaml",
			},
		},
		"bindings": map[string]any{
			"ping.api": map[string]any{
				"operation": "ping",
				"source":    "api",
				"selector":  "#/paths/~1ping/get",
			},
		},
		"x-ob": map[string]any{"transient": true},
	}
	docB := map[string]any{
		"openbindings": "0.2.0",
		"name":         "surface-b",
		"operations": map[string]any{
			"ping": map[string]any{
				"aliases": []any{"example.ping"},
				"input":   map[string]any{"type": "object"},
				"output":  map[string]any{"type": "string"},
			},
		},
	}
	openAPISource := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "Surface API", "version": "1.0.0"},
		"paths": map[string]any{
			"/ping": map[string]any{"get": map[string]any{
				"operationId": "getPing",
				"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
			}},
		},
	}
	graphQLSource := map[string]any{
		"data": map[string]any{
			"__schema": map[string]any{
				"queryType":        map[string]any{"name": "Query"},
				"mutationType":     nil,
				"subscriptionType": nil,
				"types": []any{
					map[string]any{
						"kind": "OBJECT",
						"name": "Query",
						"fields": []any{
							map[string]any{
								"name": "status",
								"args": []any{},
								"type": map[string]any{
									"kind": "SCALAR", "name": "String", "ofType": nil,
								},
								"isDeprecated": false,
							},
						},
					},
					map[string]any{"kind": "SCALAR", "name": "String"},
				},
			},
		},
	}

	cases := []struct {
		name  string
		short string
		input any
	}{
		{name: "describe", short: "describe"},
		{name: "binding specs", short: "listBindingSpecs"},
		{name: "inspect embedded source", short: "inspectSource", input: map[string]any{"source": map[string]any{
			"bindingSpec": "openbindings.openapi@1", "content": openAPISource,
		}}},
		{name: "synthesize embedded source", short: "synthesizeInterface", input: map[string]any{
			"name": "Surface synthesis",
			"sources": []any{map[string]any{
				"bindingSpec": "openbindings.openapi@1", "name": "api", "content": openAPISource,
			}},
		}},
		{name: "inspect embedded GraphQL source", short: "inspectSource", input: map[string]any{"source": map[string]any{
			"bindingSpec": "openbindings.graphql@1",
			"location":    "https://graphql.example.test/query",
			"content":     graphQLSource,
		}}},
		{name: "synthesize embedded GraphQL source", short: "synthesizeInterface", input: map[string]any{
			"name": "GraphQL surface synthesis",
			"sources": []any{map[string]any{
				"bindingSpec": "openbindings.graphql@1",
				"name":        "graphql",
				"location":    "https://graphql.example.test/query",
				"content":     graphQLSource,
			}},
		}},
		{name: "add embedded source", short: "addSource", input: map[string]any{
			"interface": map[string]any{"openbindings": "0.2.0", "name": "Surface source", "operations": map[string]any{}},
			"source": map[string]any{
				"bindingSpec": "openbindings.openapi@1", "name": "api", "content": openAPISource,
			},
		}},
		{name: "validate", short: "validateInterface", input: map[string]any{"interface": docA, "strict": true}},
		{name: "list sources", short: "listSources", input: map[string]any{"interface": docA}},
		{name: "list bindings", short: "listBindings", input: map[string]any{"interface": docA}},
		{name: "list operations", short: "listOperations", input: map[string]any{"interface": docA}},
		{name: "list aliases", short: "listOperationAliases", input: map[string]any{"interface": docA, "operation": "ping"}},
		{name: "purify", short: "purifyInterface", input: docA},
		{name: "compare", short: "compareInterfaces", input: map[string]any{"baseline": docA, "comparison": docB}},
		{name: "compatibility", short: "reportCompatibility", input: map[string]any{"target": docA, "candidate": docB}},
		{name: "codegen", short: "codegen", input: map[string]any{"interface": docA, "language": "go", "package": "surface"}},
		{name: "delegate requirements", short: "getDelegateRequirements", input: map[string]any{"capability": "invoke"}},
		{name: "new interface", short: "newInterface", input: map[string]any{"name": "Surface", "version": "1.0.0", "description": "Created over either surface."}},
		{name: "set metadata", short: "setMetadata", input: map[string]any{"interface": docB, "version": "2.0.0", "description": "Updated over either surface."}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertCrossSurfaceParity(t, cliPath, cli, served, tc.short, tc.input)
		})
	}

	t.Run("editing chain", func(t *testing.T) {
		doc := map[string]any{
			"openbindings": "0.2.0",
			"name":         "surface-edit",
			"operations":   map[string]any{},
			"sources": map[string]any{
				"api": map[string]any{
					"bindingSpec": "openbindings.openapi@1",
					"content":     openAPISource,
				},
			},
		}
		steps := []struct {
			short string
			input func() any
		}{
			{short: "setMetadata", input: func() any {
				return map[string]any{"interface": doc, "version": "2.0.0", "description": "Edited identically."}
			}},
			{short: "addOperation", input: func() any {
				return map[string]any{
					"interface": doc, "key": "ping", "description": "Ping.",
					"aliases": []any{"example.ping"}, "tags": []any{"surface"},
					"input": map[string]any{"type": "object"}, "output": map[string]any{"type": "string"},
					"idempotent": true,
				}
			}},
			{short: "setOperation", input: func() any {
				return map[string]any{
					"interface": doc, "operation": "ping", "description": "Updated ping.",
					"deprecated": true, "addTags": []any{"updated"},
				}
			}},
			{short: "addOperationAlias", input: func() any {
				return map[string]any{"interface": doc, "operation": "ping", "aliases": []any{"example.getPing"}}
			}},
			{short: "removeOperationAlias", input: func() any {
				return map[string]any{"interface": doc, "operation": "ping", "aliases": []any{"example.getPing"}}
			}},
			{short: "setOperationCodegenName", input: func() any {
				return map[string]any{"interface": doc, "operation": "ping", "codegenName": "Ping"}
			}},
			{short: "setOperationOutputSchema", input: func() any {
				return map[string]any{"interface": doc, "operation": "ping", "outputSchema": map[string]any{"type": "array"}}
			}},
			{short: "renameOperation", input: func() any {
				return map[string]any{"interface": doc, "oldKey": "ping", "newKey": "pong"}
			}},
			{short: "bindOperation", input: func() any {
				return map[string]any{
					"interface": doc, "operation": "pong", "source": "api",
					"selector": "#/paths/~1ping/get", "preference": 5,
				}
			}},
			{short: "unbindOperation", input: func() any {
				return map[string]any{"interface": doc, "operation": "pong", "source": "api"}
			}},
			{short: "removeOperation", input: func() any {
				return map[string]any{"interface": doc, "keys": []any{"pong"}}
			}},
			{short: "removeSource", input: func() any {
				return map[string]any{"interface": doc, "key": "api"}
			}},
		}
		for _, step := range steps {
			t.Run(step.short, func(t *testing.T) {
				outputs := assertCrossSurfaceParity(t, cliPath, cli, served, step.short, step.input())
				if len(outputs) != 1 {
					t.Fatalf("%s produced %d outputs, want one resulting interface", step.short, len(outputs))
				}
				next, ok := canonicalJSON(t, outputs[0]).(map[string]any)
				if !ok {
					t.Fatalf("%s output is %T, want interface object", step.short, outputs[0])
				}
				doc = next
			})
		}
	})

	t.Run("context state", func(t *testing.T) {
		key := "https://surface-parity.example/v1"
		set := func(invoker func(*testing.T, string, any) []any, value map[string]any) {
			t.Helper()
			invoker(t, "setContext", map[string]any{"key": key, "value": value})
		}
		cliInvoke := func(t *testing.T, short string, input any) []any {
			return invokeCLIContractOperation(t, cliPath, cli, "openbindings.ob."+short, input)
		}
		serveInvoke := func(t *testing.T, short string, input any) []any {
			return invokeServedContractOperation(t, served, "openbindings.ob."+short, input)
		}
		assertReadsAgree := func() {
			t.Helper()
			assertCrossSurfaceParity(t, cliPath, cli, served, "getContext", map[string]any{"key": key})
			assertCrossSurfaceParity(t, cliPath, cli, served, "listContexts", nil)
		}

		set(cliInvoke, map[string]any{"headers": map[string]any{"X-Surface": "cli"}})
		assertReadsAgree()
		set(serveInvoke, map[string]any{
			"headers":  map[string]any{"X-Surface": "serve"},
			"metadata": map[string]any{"owner": "parity"},
		})
		assertReadsAgree()
		cliInvoke(t, "removeContext", map[string]any{"key": key})
		assertReadsAgree()
	})
}

func assertCrossSurfaceParity(
	t *testing.T,
	cliPath string,
	cli openbindings.Interface,
	served *openbindings.Interface,
	short string,
	input any,
) []any {
	t.Helper()
	op := "openbindings.ob." + short
	cliOutputs := invokeCLIContractOperation(t, cliPath, cli, op, input)
	serveOutputs := invokeServedContractOperation(t, served, op, input)
	cliComparable := comparableOperationOutput(short, canonicalJSON(t, cliOutputs))
	serveComparable := comparableOperationOutput(short, canonicalJSON(t, serveOutputs))
	if !reflect.DeepEqual(cliComparable, serveComparable) {
		t.Fatalf(
			"%s differs across surfaces\nCLI:   %#v\nserve: %#v",
			op,
			cliOutputs,
			serveOutputs,
		)
	}
	return serveOutputs
}

// comparableOperationOutput removes only contract-declared nondeterminism.
// Carrier details are deliberately not normalized here: those belong in a
// binding output transform and a difference must fail this test.
func comparableOperationOutput(short string, value any) any {
	switch short {
	case "reportCompatibility":
		outputs, ok := value.([]any)
		if !ok {
			return value
		}
		for _, output := range outputs {
			if report, ok := output.(map[string]any); ok {
				delete(report, "generated_at")
			}
		}
	case "addSource", "synthesizeInterface":
		removeXOBTimestamps(value)
	}
	return value
}

func removeXOBTimestamps(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if metadata, ok := typed["x-ob"].(map[string]any); ok {
			delete(metadata, "lastSynced")
		}
		for _, child := range typed {
			removeXOBTimestamps(child)
		}
	case []any:
		for _, child := range typed {
			removeXOBTimestamps(child)
		}
	}
}

func invokeCLIContractOperation(
	t *testing.T,
	cliPath string,
	cli openbindings.Interface,
	operation string,
	input any,
) []any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	events, _, err := app.InvokeOBIOperation(ctx, cliPath, operation, "", input)
	if err != nil {
		t.Fatalf("%s CLI invoke: %v", operation, err)
	}
	var outputs []any
	for event := range events {
		if event.Error != nil {
			t.Fatalf("%s CLI error: %s", operation, event.Error.Code)
		}
		validateContractOutput(t, cli, operation, event.Output)
		outputs = append(outputs, event.Output)
	}
	return outputs
}

func invokeServedContractOperation(
	t *testing.T,
	served *openbindings.Interface,
	operation string,
	input any,
) []any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	signature := invoke.NewOperationSignature[any, any](operation)
	invocation := invoke.Invoke(
		ctx,
		app.DefaultInvoker(),
		served,
		signature,
		invoke.WithContext(map[string]any{"bearerToken": "test-token"}),
	)
	if input != nil {
		if err := invocation.Write(ctx, input); err != nil {
			t.Fatalf("%s serve write: %v", operation, err)
		}
	}
	if err := invocation.Close(); err != nil {
		t.Fatalf("%s serve close: %v", operation, err)
	}

	var outputs []any
	stream := invocation.Outputs()
	for {
		output, err := stream.Read(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("%s serve output: %v", operation, err)
		}
		validateContractOutput(t, *served, operation, output)
		outputs = append(outputs, output)
	}
	return outputs
}

func validateContractOutput(
	t *testing.T,
	iface openbindings.Interface,
	operation string,
	output any,
) {
	t.Helper()
	schema := iface.Operations[operation].Output
	if schema == nil {
		return
	}
	if err := openbindings.ValidateAgainstSchema(output, schema, iface.Schemas); err != nil {
		t.Fatalf("%s output does not satisfy its contract: %v\noutput: %#v", operation, err, output)
	}
}

func canonicalJSON(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
