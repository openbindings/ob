package app

import (
	"strings"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

// TestOperationRealizationMatrix proves that the contract, CLI, and served
// surfaces form one complete and non-overlapping operation inventory:
//
//   - every contract operation has exactly one CLI usage binding;
//   - every remotely meaningful operation has exactly one HTTP or WebSocket
//     realization;
//   - foreground operations are explicitly local-only; and
//   - the generated serve OBI carries exactly the transport selected here.
//
// This is the machine-checked realization matrix. Adding a contract operation
// without making a deliberate surface decision cannot remain a documentation-
// only omission.
func TestOperationRealizationMatrix(t *testing.T) {
	const prefix = "openbindings.ob."

	contract, err := resolveInterface("../../ob.obi.json")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := GenerateBoundCLI("../../ob.obi.json", "../cmd/usage.kdl")
	if err != nil {
		t.Fatal(err)
	}
	served, err := GenerateBoundServe(
		"../../ob.obi.json",
		"../server/openapi.yaml",
		"../server/serve.obi.json",
		"http://127.0.0.1:20290",
	)
	if err != nil {
		t.Fatal(err)
	}

	type realization struct {
		cli       int
		http      int
		stream    int
		localOnly int
	}
	matrix := map[string]*realization{}
	for key := range contract.Operations {
		if !strings.HasPrefix(key, prefix) {
			t.Errorf("contract operation %q is outside the project namespace", key)
			continue
		}
		matrix[strings.TrimPrefix(key, prefix)] = &realization{}
	}

	for short, command := range CommandByShort {
		row := matrix[short]
		if row == nil {
			t.Errorf("CLI command %q (%s) has no contract operation", command, short)
			continue
		}
		row.cli++
		key := prefix + short
		binding, ok := cli.Bindings[key+".usage"]
		if !ok {
			t.Errorf("%s: generated CLI has no usage binding", key)
		} else if binding.Operation != key || binding.Source != "usage" {
			t.Errorf("%s: malformed CLI binding: %+v", key, binding)
		}
	}

	routeKeys := map[string]string{}
	runtimeRouteKeys := map[string]string{}
	for _, route := range ServeHTTPRoutes() {
		row := matrix[route.Operation]
		if row == nil {
			t.Errorf("HTTP route %s %s names unknown operation %s", route.Method, route.Path, route.Operation)
			continue
		}
		row.http++
		routeKey := strings.ToUpper(route.Method) + " " + route.Path
		if previous := routeKeys[routeKey]; previous != "" {
			t.Errorf("%s is assigned to both %s and %s", routeKey, previous, route.Operation)
		}
		routeKeys[routeKey] = route.Operation
		runtimePath := route.Path
		if route.RuntimePath != "" {
			runtimePath = route.RuntimePath
		}
		runtimeKey := strings.ToUpper(route.Method) + " " + runtimePath
		if previous := runtimeRouteKeys[runtimeKey]; previous != "" {
			t.Errorf("%s is assigned to both %s and %s at runtime", runtimeKey, previous, route.Operation)
		}
		runtimeRouteKeys[runtimeKey] = route.Operation
		assertServedBinding(t, served, route.Operation, "openapi")
	}

	for _, route := range ServeStreamRoutes() {
		row := matrix[route.Operation]
		if row == nil {
			t.Errorf("stream route %s names unknown operation %s", route.Path, route.Operation)
			continue
		}
		row.stream++
		routeKey := "GET " + route.Path
		if previous := routeKeys[routeKey]; previous != "" {
			t.Errorf("%s is assigned to both %s and %s", routeKey, previous, route.Operation)
		}
		routeKeys[routeKey] = route.Operation
		if previous := runtimeRouteKeys[routeKey]; previous != "" {
			t.Errorf("%s is assigned to both %s and %s at runtime", routeKey, previous, route.Operation)
		}
		runtimeRouteKeys[routeKey] = route.Operation
		assertServedBinding(t, served, route.Operation, "asyncapi")
	}

	for _, short := range LocalOnlyOperations() {
		row := matrix[short]
		if row == nil {
			t.Errorf("local-only classification names unknown operation %s", short)
			continue
		}
		row.localOnly++
	}

	for short, row := range matrix {
		key := prefix + short
		if row.cli != 1 {
			t.Errorf("%s: CLI realization count = %d, want 1", key, row.cli)
		}
		remote := row.http + row.stream
		if row.localOnly == 1 {
			if remote != 0 {
				t.Errorf("%s: local-only operation also has %d remote realization(s)", key, remote)
			}
			if _, exposed := served.Operations[key]; exposed {
				t.Errorf("%s: local-only operation appears in the served OBI", key)
			}
			continue
		}
		if row.localOnly != 0 {
			t.Errorf("%s: local-only classification count = %d, want 0 or 1", key, row.localOnly)
		}
		if remote != 1 {
			t.Errorf("%s: remote realization count = %d, want exactly 1", key, remote)
		}
		if _, exposed := served.Operations[key]; !exposed {
			t.Errorf("%s: remotely meaningful operation is absent from the served OBI", key)
		}
	}

	if got, want := len(matrix), 54; got != want {
		t.Errorf("contract operation count = %d, want %d", got, want)
	}
	if got, want := len(CommandByShort), len(matrix); got != want {
		t.Errorf("CLI realization count = %d, want %d", got, want)
	}
	if got, want := len(ServeHTTPRoutes()), 49; got != want {
		t.Errorf("HTTP realization count = %d, want %d", got, want)
	}
	if got, want := len(ServeStreamRoutes()), 2; got != want {
		t.Errorf("stream realization count = %d, want %d", got, want)
	}
	if got, want := len(LocalOnlyOperations()), 3; got != want {
		t.Errorf("local-only realization count = %d, want %d", got, want)
	}
}

func assertServedBinding(t *testing.T, served *openbindings.Interface, short, source string) {
	t.Helper()
	binding, ok := served.Bindings["openbindings.ob."+short+"."+source]
	if !ok {
		t.Errorf("openbindings.ob.%s: generated serve OBI has no %s binding", short, source)
		return
	}
	if binding.Operation != "openbindings.ob."+short || binding.Source != source {
		t.Errorf(
			"openbindings.ob.%s: generated serve binding = operation %q, source %q",
			short,
			binding.Operation,
			binding.Source,
		)
	}
}
