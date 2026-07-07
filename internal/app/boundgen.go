package app

import (
	"fmt"
	"os"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/usage"
)

// CommandByShort maps each contract operation's short name to its CLI
// command path — the generator's binding-derivation table. This knowledge
// lived in usage.kdl as opKey props until the openbindings.usage port
// evicted it: the artifact stays pristine jdx, and the command↔operation
// marriage is generator configuration, reviewable in one place. The
// usage.kdl conformance test (internal/cmd) cross-checks this table against
// the kdl tree and the contract.
var CommandByShort = map[string]string{
	"addOperation":             "operation add",
	"addOperationAlias":        "operation alias add",
	"addSource":                "source add",
	"bindOperation":            "operation bind",
	"codegen":                  "codegen",
	"compareInterfaces":        "diff",
	"conform":                  "conform",
	"demo":                     "demo",
	"describe":                 "describe",
	"detachOperation":          "operation detach",
	"getContext":               "context get",
	"getDelegateRequirements":  "delegate requirements",
	"initializeEnvironment":    "init",
	"inspectSource":            "inspect",
	"invokeBinding":            "binding invoke",
	"invokeOperation":          "operation invoke",
	"listContexts":             "context list",
	"listDelegates":            "delegate list",
	"listFormats":              "formats",
	"listOperationAliases":     "operation alias list",
	"listOperations":           "operation list",
	"listSources":              "source list",
	"mergeInterfaces":          "merge",
	"newInterface":             "new",
	"prepareBinding":           "binding prepare",
	"prepareOperation":         "operation prepare",
	"pullSource":               "source pull",
	"purifyInterface":          "purify",
	"registerDelegate":         "delegate register",
	"removeContext":            "context remove",
	"removeOperation":          "operation remove",
	"removeOperationAlias":     "operation alias remove",
	"removeSource":             "source remove",
	"renameOperation":          "operation rename",
	"reportCompatibility":      "compat",
	"reportEnvironmentStatus":  "environment",
	"reportInterfaceStatus":    "status",
	"resolveDelegate":          "delegate resolve",
	"resolveDelegateForFormat": "delegate resolve-format",
	"resolveInterface":         "resolve",
	"setContext":               "context set",
	"setDelegatePreference":    "delegate prefer",
	"setMetadata":              "meta set",
	"setOperation":             "operation set",
	"setOperationCodegenName":  "operation codegen-name",
	"setOperationOutputSchema": "operation output-schema",
	"startMCPServer":           "mcp",
	"startServer":              "start",
	"synthesizeInterface":      "synthesize",
	"unbindOperation":          "operation unbind",
	"unregisterDelegate":       "delegate unregister",
	"validateInterface":        "validate",
}

// WireInputByShort names the machine-natured commands whose whole wire input
// rides one --input flag as JSON (the relocated wireInput props): binding
// invoke/prepare keep the flag by design; inspect/synthesize are audited in
// batch 5. setContext converted to stdin delivery in batch 3 (the Context
// value rides stdin so credentials never touch argv; see the adaptation and
// delivery tables in GenerateBoundCLI).
var WireInputByShort = map[string]string{
	"inspectSource":       "input",
	"invokeBinding":       "input",
	"prepareBinding":      "input",
	"synthesizeInterface": "input",
}

// exitOKByShort stamps diff(1)-convention exits: the code is a result, not a
// failure, and the -F json report carries the verdict either way.
var exitOKByShort = map[string][]int{
	"compareInterfaces":   {0, 1},
	"reportCompatibility": {0, 1},
	"validateInterface":   {0, 1},
}

// GenerateBoundCLI builds the bound CLI realization of ob's interface: the
// unbound contract's operations (keys, aliases, schemas — authoritative for
// operation identity) bound to the PRISTINE usage.kdl (the bare jdx
// artifact IS the source — no wrapper; specification + configuration =
// complete invocation). Refs are the format's own grammar (space-separated
// command paths from CommandByShort). The wire elections the artifact
// cannot express (JSON machine lane, diff(1) exits, filter routing) are
// CONSUMER CONFIGURATION: ob's own site-guarded hook table
// (BoundCLIHookTable, installed on ob's invokers) — published as the
// per-op recipe so other consumers can transcribe it. This is what
// `ob --openbindings` emits; regenerating it from the contract keeps it
// conformant instead of drifting as a hand-maintained file.
//
// The usage.kdl is embedded verbatim (source content) so the emitted OBI
// is self-contained and portable per OBI-D-05: it is consumed away from
// this repo (delegate registration, agents), where a relative path would
// not resolve.
func GenerateBoundCLI(contractPath, usagePath string) (*openbindings.Interface, error) {
	contract, err := loadInterfaceFile(contractPath)
	if err != nil {
		return nil, fmt.Errorf("load contract: %w", err)
	}

	usageData, err := os.ReadFile(usagePath)
	if err != nil {
		return nil, fmt.Errorf("read usage source: %w", err)
	}
	usageText := string(usageData)
	spec, err := usage.ParseKDL(usageData)
	if err != nil {
		return nil, fmt.Errorf("parse usage spec: %w", err)
	}

	// Per-operation adaptation transforms: where a wire input field cannot
	// ride the transport's field-name mapping as-is (a name the CLI spells
	// differently, or one shadowed by a root flag like -F/--format), the
	// BINDING adapts the wire shape to the CLI's natural surface. Transforms
	// are spec-level and stay on the OBI entry; transport mechanics
	// (routing, decode, classify) are the hook table's.
	adaptationByShort := map[string]string{
		// resolveDelegateForFormat: wire `format` → the <format-token> arg
		// (the bare name would map to the root --format flag).
		"resolveDelegateForFormat": `{"format-token": $$.format, "format": "json"}`,
		// setDelegatePreference: wire `format` → --source-format, same shadow.
		"setDelegatePreference": `$merge([$sift($$, function($v, $k) { $k != "format" }), $exists($$.format) ? {"source-format": $$.format} : {}, {"format": "json"}])`,
		// getContext/removeContext: the wire key is the CLI's <url> argument.
		"getContext":    `{"url": $$.key, "format": "json"}`,
		"removeContext": `{"url": $$.key, "format": "json"}`,

		// Read/analysis family (cohort C): document-in operations realized as
		// Unix filters. The contract's document-valued field(s) ride out of
		// band via the unit's `delivery` map (stdin-dash for the primary
		// document, file for a second); the transform renames each wire field
		// to the CLI's own natural arg/flag name and forces the machine lane
		// (-F json). Every field the transform emits must name a flag or arg of
		// the command, or be routed off argv by delivery — buildCLIArgs refuses
		// strays. See ob-pj/wire-conformance.md, batch 3.
		"validateInterface":     `{"locator": $$.interface, "strict": $$.strict, "format": "json"}`,
		"reportInterfaceStatus": `{"obi-path": $$.interface, "format": "json"}`,
		"purifyInterface":       `{"obi-path": $$, "format": "json"}`,
		"listSources":           `{"obi-path": $$.interface, "format": "json"}`,
		"listOperations":        `{"obi": $$.interface, "tag": $$.tag, "format": "json"}`,
		"listOperationAliases":  `{"obi": $$.interface, "operation": $$.operation, "format": "json"}`,
		"prepareOperation":      `{"obi": $$.interface, "operation": $$.operation, "binding": $$.binding, "format": "json"}`,
		"compareInterfaces":     `{"baseline": $$.baseline, "comparison": $$.comparison, "from-sources": $$.fromSources, "only": $$.only, "format": "json"}`,
		"reportCompatibility":   `{"target": $$.target, "candidate": $$.candidate, "format": "json"}`,

		// codegen: `language` → the CLI's --lang; -F json emits the CodegenOutput
		// envelope (the CLI defaults to raw source, its natural human lane).
		"codegen": `{"source": $$.interface, "lang": $$.language, "package": $$.package, "format": "json"}`,
		// conform/merge modify their target, so BOTH documents ride file
		// delivery (a written-back target cannot be stdin) and the transport
		// injects auto-accept (-y): the child's stdin never carries a document,
		// so nothing can be misread as a prompt answer, and the modified
		// interface returns inside the ConformResult/MergeResult report.
		"conform":         `{"interface": $$.interface, "target-obi": $$.target, "dry-run": $$.dryRun, "yes": true, "format": "json"}`,
		"mergeInterfaces": `{"target": $$.target, "source": $$.source, "from-sources": $$.fromSources, "only": $$.only, "op": $$.operations, "exclude-op": $$.excludeOperations, "ops-only": $$.opsOnly, "no-bindings": $$.noBindings, "no-sources": $$.noSources, "yes": true, "format": "json"}`,
		// setContext: the wire key is the CLI's <url> argument; the Context
		// value rides stdin (delivery below) so credentials never touch argv.
		"setContext": `{"url": $$.key, "value": $$.value}`,
	}
	_ = routesByShort // elections live in the hook table (BoundCLIHookTable), not the document

	// Machine-natured --input lanes (the relocated wireInput props): the
	// whole wire input JSON-serializes into one flag. $$ (the root of the
	// operation input), not $: inside an object constructor the context is
	// a sequence, and $string would serialize a one-element array.
	for short, flag := range WireInputByShort {
		adaptationByShort[short] = fmt.Sprintf("{ %q: $string($$) }", flag)
	}

	// Commands whose kdl declares a --format flag get the machine lane
	// forced (-F json) unless the operation's own input carries a `format`
	// field (root-flag shadowing; those ops are adapted individually above).
	formatFlagByPath := map[string]bool{}
	spec.Walk(func(path []string, cmd usage.Command) {
		for _, f := range cmd.Flags {
			for _, long := range f.ParseUsage().Long {
				if long == "format" {
					formatFlagByPath[strings.Join(path, " ")] = true
				}
			}
		}
	})

	bound := &openbindings.Interface{
		OpenBindings: contract.OpenBindings,
		Name:         contract.Name,
		Version:      contract.Version,
		Description:  contract.Description,
		Schemas:      contract.Schemas,
		Operations:   make(map[string]openbindings.Operation, len(contract.Operations)),
		Sources:      map[string]openbindings.Source{},
		Bindings:     map[string]openbindings.BindingEntry{},
	}

	for key, op := range contract.Operations {
		bound.Operations[key] = op
		short := key[strings.LastIndex(key, ".")+1:]
		cmdPath, ok := CommandByShort[short]
		if !ok {
			// Operations with no CLI command stay unbound; that's expected.
			continue
		}

		be := openbindings.BindingEntry{
			Operation: key,
			Source:    "usage",
			Ref:       cmdPath, // the format's own grammar: the command path
		}
		if adaptation, ok := adaptationByShort[short]; ok {
			be.InputTransform = &openbindings.TransformOrRef{Inline: adaptation}
		} else if formatFlagByPath[cmdPath] && !inputHasFormatField(contract, op) {
			be.InputTransform = &openbindings.TransformOrRef{
				Inline: `$merge([$type($$) = "object" ? $$ : {}, {"format": "json"}])`,
			}
		}
		bound.Bindings[key+".usage"] = be
	}

	bound.Sources["usage"] = openbindings.Source{
		Format:  "usage@" + usage.MaxTestedVersion,
		Content: usageText, // the pristine artifact, verbatim
	}

	return bound, nil
}

// inputHasFormatField reports whether an operation's input schema defines a
// `format` property (resolving a top-level #/schemas/ ref). Such a field maps
// to the CLI's root -F/--format flag under the usage transport's field-name
// mapping, so the -F json forcing transform must not clobber it.
func inputHasFormatField(contract *openbindings.Interface, op openbindings.Operation) bool {
	schema := map[string]any(op.Input)
	if schema == nil {
		return false
	}
	if ref, ok := schema["$ref"].(string); ok && strings.HasPrefix(ref, "#/schemas/") {
		name := strings.TrimPrefix(ref, "#/schemas/")
		resolved, ok := contract.Schemas[name]
		if !ok {
			return false
		}
		schema = map[string]any(resolved)
	}
	props, _ := schema["properties"].(map[string]any)
	_, has := props["format"]
	return has
}

// GenerateBoundServe builds the bound serve realization (serve.obi.json): the
// served subset of the contract's operations (authoritative for identity — keys,
// aliases, schemas) with transport bindings. HTTP binding refs are derived from
// openapi.yaml (operationId == operation short-name); the WS invoke binding is
// attached directly. Hand-tuned binding transforms are preserved from the
// existing serve OBI.
//
// Each source's location is an absolute URL under servedBaseURL (the default ob
// start address, e.g. "http://127.0.0.1:20290") pointing at this server's own
// live /openapi.yaml and /asyncapi.yaml. Unlike the bound CLI OBI, the served
// OBI is always fetched from a running server, so it points back at that server
// rather than embedding a frozen spec copy; handleOBI rewrites the host:port to
// the actual request address per request (deriveBaseURL). The committed
// default-port URL keeps the document OBI-D-05-valid without inlining the spec.
//
// MCP is not a served transport here: ob's served interface is exposed as an MCP
// server by pointing the generic bridge at a running server (`ob mcp <url>`), so
// this OBI carries no mcp source or bindings. Infra endpoints (healthz,
// /.well-known, oauth, spec resources) are filtered out automatically because
// they have no matching contract operation.
func GenerateBoundServe(contractPath, openapiPath, existingServePath, servedBaseURL string) (*openbindings.Interface, error) {
	contract, err := loadInterfaceFile(contractPath)
	if err != nil {
		return nil, fmt.Errorf("load contract: %w", err)
	}
	existing, err := loadInterfaceFile(existingServePath)
	if err != nil {
		return nil, fmt.Errorf("load existing serve OBI: %w", err)
	}

	httpDerived, err := DeriveFromSource(openbindings.Source{Format: "openapi@3.1", Location: openapiPath}, "openapi", "")
	if err != nil {
		return nil, fmt.Errorf("derive openapi: %w", err)
	}

	bound := &openbindings.Interface{
		OpenBindings: contract.OpenBindings,
		Name:         contract.Name,
		Version:      contract.Version,
		Description:  contract.Description,
		Schemas:      contract.Schemas,
		Operations:   map[string]openbindings.Operation{},
		Sources:      map[string]openbindings.Source{},
		Bindings:     map[string]openbindings.BindingEntry{},
	}

	// Point each served source at this server's own live spec endpoint via an
	// absolute URL (default port; handleOBI rewrites it to the request address).
	// No embedded content: the served OBI is always fetched from a running
	// server, so a frozen inline copy would only bloat the discovery document and,
	// per spec §401, shadow the live location. MCP is bridged at runtime
	// (`ob mcp <url>`), not a served transport, so its source is dropped.
	for key, src := range existing.Sources {
		if key == "mcp" {
			continue
		}
		src.Location = servedBaseURL + "/" + key + ".yaml"
		src.Content = nil
		bound.Sources[key] = src
	}

	include := func(opKey string) bool {
		op, ok := contract.Operations[opKey]
		if !ok {
			return false // not a contract op (filters infra endpoints)
		}
		bound.Operations[opKey] = op
		return true
	}
	// carry preserves hand-tuned transforms across regeneration. The original
	// hand-maintained serve OBI keyed bindings by bare short-name
	// (e.g. "getContext.openapi"); regenerated files key them by the full
	// contract key ("openbindings.ob.getContext.openapi"). Look the old binding
	// up under the full key first, then fall back to the legacy short-name key,
	// so transforms survive both the initial rekey and every later regen
	// (idempotent).
	carry := func(opKey, short, source string, be *openbindings.BindingEntry) {
		old, ok := existing.Bindings[opKey+"."+source]
		if !ok {
			old, ok = existing.Bindings[short+"."+source]
		}
		if ok {
			be.InputTransform = old.InputTransform
			be.OutputTransform = old.OutputTransform
		}
	}

	// HTTP (openapi) bindings — ref derived from openapi.yaml.
	for _, b := range httpDerived.Bindings {
		opKey := "openbindings.ob." + b.Operation
		if !include(opKey) {
			continue
		}
		be := openbindings.BindingEntry{Operation: opKey, Source: "openapi", Ref: b.Ref}
		carry(opKey, b.Operation, "openapi", &be)
		bound.Bindings[opKey+".openapi"] = be
	}

	// WS (asyncapi) invoke binding.
	const invKey = "openbindings.ob.invokeBinding"
	if include(invKey) {
		be := openbindings.BindingEntry{Operation: invKey, Source: "asyncapi", Ref: "#/operations/invokeBinding"}
		carry(invKey, "invokeBinding", "asyncapi", &be)
		bound.Bindings[invKey+".asyncapi"] = be
	}

	return bound, nil
}

// routesByShort routes document-valued POST-transform fields off argv:
// the primary document to the child's stdin (RouteStdinDash substitutes
// `-` in its slot; the CLI reads the doc as a `-` locator), a second
// document to a materialized temp file (its path fills the slot). Keys
// are the CLI arg names the binding transforms produce. CONSUMER
// CONFIGURATION (ob's own hook table + the published recipe), never
// document vocabulary.
var routesByShort = map[string]map[string]string{
	"validateInterface":     {"locator": usage.RouteStdinDash},
	"reportInterfaceStatus": {"obi-path": usage.RouteStdinDash},
	"purifyInterface":       {"obi-path": usage.RouteStdinDash},
	"listSources":           {"obi-path": usage.RouteStdinDash},
	"listOperations":        {"obi": usage.RouteStdinDash},
	"listOperationAliases":  {"obi": usage.RouteStdinDash},
	"prepareOperation":      {"obi": usage.RouteStdinDash},
	"compareInterfaces":     {"baseline": usage.RouteStdinDash, "comparison": usage.RouteFile},
	"reportCompatibility":   {"target": usage.RouteStdinDash, "candidate": usage.RouteFile},
	"codegen":               {"source": usage.RouteStdinDash},
	// Both documents to temp files: conform/merge write the target back
	// (impossible on stdin), and materialization keeps stdin clear.
	"conform":         {"interface": usage.RouteFile, "target-obi": usage.RouteFile},
	"mergeInterfaces": {"target": usage.RouteFile, "source": usage.RouteFile},
	// setContext: the Context value (credentials) rides stdin, never argv.
	"setContext": {"value": usage.RouteStdinDash},
}

// BoundCLIHookTable is ob's own consumer configuration for its bound CLI
// OBI — the elections the pristine artifact cannot express, exactly what
// the wrapper units carried, now consumer-side (specification +
// configuration = complete invocation). Site-guarded by construction:
// the compiled hooks fire only for usage-family sites whose Target is
// ob's own binary, so a foreign OBI declaring openbindings.ob.* keys
// never inherits these elections. Published as the per-op recipe
// (generated from this table, version-stamped) so other consumers can
// transcribe it into their own hooks.
func BoundCLIHookTable(contract *openbindings.Interface) usage.HookTable {
	table := usage.HookTable{
		OKExits: map[string][]int{},
		Routes:  map[string]map[string]string{},
	}
	for key := range contract.Operations {
		short := key[strings.LastIndex(key, ".")+1:]
		if _, bound := CommandByShort[short]; !bound {
			continue
		}
		// Every ob machine lane speaks JSON.
		table.DecodeJSON = append(table.DecodeJSON, key)
		if codes := exitOKByShort[short]; codes != nil {
			table.OKExits[key] = codes
		}
		if routes := routesByShort[short]; routes != nil {
			table.Routes[key] = routes
		}
	}
	return table
}

// InstallBoundCLIHooks compiles the table and installs it at invoker
// level, wrapped in the SITE GUARD (usage family + ob's own Target):
// elections never leak onto foreign bindings.
func InstallBoundCLIHooks(inv *openbindings.OperationInvoker, contract *openbindings.Interface) {
	decode, classify, route := BoundCLIHookTable(contract).Hooks()
	guard := func(site openbindings.InvokeSite) bool {
		return site.FormatName() == "usage" && (site.Target == "ob" || strings.HasSuffix(site.Target, "/ob"))
	}
	inv.OutputDecoder = func(site openbindings.InvokeSite, raw openbindings.RawResult) (any, error) {
		if !guard(site) {
			return nil, openbindings.ErrUseDefault
		}
		return decode(site, raw)
	}
	inv.ResultClassifier = func(site openbindings.InvokeSite, raw openbindings.RawResult) (bool, error) {
		if !guard(site) {
			return false, openbindings.ErrUseDefault
		}
		return classify(site, raw)
	}
	inv.FieldRouter = func(site openbindings.InvokeSite, field string, value any) string {
		if !guard(site) {
			return ""
		}
		return route(site, field, value)
	}
}

// GenerateBoundCLIRecipe renders ob's per-op consumer configuration — the
// internal hook table's elections plus the floor assumptions that answer
// everything else — as reference markdown. GENERATED, never hand-synced
// (hand-synced docs of a live table are a known drift failure): genbound
// writes it beside the bound OBI, stamped with the contract version it was
// derived from. This is the published recipe another consumer transcribes
// into its own hooks to invoke ob's CLI conformantly.
func GenerateBoundCLIRecipe(contract *openbindings.Interface) string {
	table := BoundCLIHookTable(contract)
	decodeJSON := make(map[string]bool, len(table.DecodeJSON))
	for _, k := range table.DecodeJSON {
		decodeJSON[k] = true
	}

	keys := make([]string, 0, len(contract.Operations))
	for key := range contract.Operations {
		short := key[strings.LastIndex(key, ".")+1:]
		if _, bound := CommandByShort[short]; bound {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("# ob bound-CLI invocation recipe\n\n")
	sb.WriteString("<!-- GENERATED by `go generate ./internal/app` (genbound) from the\n")
	sb.WriteString("     internal hook table — do not edit by hand. -->\n\n")
	fmt.Fprintf(&sb, "Derived from ob contract version **%s** (usage artifact `usage@%s`).\n\n", contract.Version, usage.MaxTestedVersion)
	sb.WriteString("ob's CLI is described by a pristine [jdx usage](https://usage.jdx.dev) artifact; the\n")
	sb.WriteString("wire questions the artifact cannot answer are answered by ob's own consumer\n")
	sb.WriteString("configuration (specification + configuration = complete invocation). This table\n")
	sb.WriteString("IS that configuration: any consumer invoking `ob` through the usage format\n")
	sb.WriteString("reproduces it with three hooks (or `usage.HookTable` in the Go SDK). Every\n")
	sb.WriteString("unlisted axis falls to the format's documented assumptions (stdout = text,\n")
	sb.WriteString("success = exit 0, fields ride argv).\n\n")
	sb.WriteString("| Operation | Command | Decode | Success exits | Routed fields |\n")
	sb.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, key := range keys {
		short := key[strings.LastIndex(key, ".")+1:]
		decode := "text (assumption)"
		if decodeJSON[key] {
			decode = "json"
		}
		exits := "0 (assumption)"
		if codes, ok := table.OKExits[key]; ok {
			parts := make([]string, len(codes))
			for i, c := range codes {
				parts[i] = fmt.Sprintf("%d", c)
			}
			exits = strings.Join(parts, ", ")
		}
		routed := "—"
		if routes, ok := table.Routes[key]; ok {
			fields := make([]string, 0, len(routes))
			for f := range routes {
				fields = append(fields, f)
			}
			sort.Strings(fields)
			pairs := make([]string, len(fields))
			for i, f := range fields {
				pairs[i] = f + "=" + routes[f]
			}
			routed = strings.Join(pairs, ", ")
		}
		fmt.Fprintf(&sb, "| `%s` | `ob %s` | %s | %s | %s |\n", key, CommandByShort[short], decode, exits, routed)
	}
	sb.WriteString("\nRoute tokens are the Go SDK's `usage.Route*` channel vocabulary: `stdin-dash`\n")
	sb.WriteString("(bytes to stdin, `-` in the field's slot), `stdin` (slotless pure channel),\n")
	sb.WriteString("`file` (temp-file path in the slot), `argv` (the default).\n")
	return sb.String()
}
