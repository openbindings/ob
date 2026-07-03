package app

import (
	"fmt"
	"os"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/usage"
)

// GenerateBoundCLI builds the bound CLI realization of ob's interface: the
// unbound contract's operations (keys, aliases, schemas — authoritative for
// operation identity) with usage-transport bindings attached by short-name from
// usage.kdl (authoritative for the CLI wire ref). This is what
// `ob --openbindings` emits; regenerating it from the contract keeps it
// conformant instead of drifting as a hand-maintained file.
//
// contractPath and usagePath are read for generation; usageFormat is the usage
// format token (e.g. "usage@2.13.1"). The usage source is embedded as `content`
// (not a relative `location`) so the emitted OBI is self-contained and portable
// per the spec's context-free reference guarantee (OBI-D-05): `ob --openbindings`
// is consumed away from this repo (delegate registration, agents), where a
// relative path would not resolve.
func GenerateBoundCLI(contractPath, usagePath, usageFormat string) (*openbindings.Interface, error) {
	contract, err := loadInterfaceFile(contractPath)
	if err != nil {
		return nil, fmt.Errorf("load contract: %w", err)
	}

	// Derive the CLI's bindable targets from usage.kdl. Derived bindings are
	// keyed by the usage opKey, i.e. the operation short-name.
	derived, err := DeriveFromSource(openbindings.Source{Format: usageFormat, Location: usagePath}, "usage", "")
	if err != nil {
		return nil, fmt.Errorf("derive usage: %w", err)
	}
	refByShort := make(map[string]string, len(derived.Bindings))
	for _, b := range derived.Bindings {
		refByShort[b.Operation] = b.Ref
	}

	// Embed the usage spec inline so the bound OBI carries no out-of-document
	// reference. usage.kdl is text, so this embeds as its UTF-8 source string.
	usageData, err := os.ReadFile(usagePath)
	if err != nil {
		return nil, fmt.Errorf("read usage source: %w", err)
	}
	usageContent, err := ParseContentForEmbed(usageData, usageFormat)
	if err != nil {
		return nil, fmt.Errorf("embed usage source: %w", err)
	}

	// Machine-lane transforms. A usage.kdl command may declare
	// wireInput="<flag>", meaning the command carries the operation's whole
	// wire input as one JSON string in that flag (the `binding invoke --input`
	// pattern). For those bindings we attach an inputTransform that JSON-
	// serializes the operation input into the flag, so generic operation-
	// invocation — in particular a registrar operation-invoking ob as an exec
	// delegate — produces argv the CLI actually parses. Ops without wireInput
	// rely on the usage transport's field-name mapping, which only carries
	// flat, scalar-shaped inputs.
	spec, err := usage.ParseKDL(usageData)
	if err != nil {
		return nil, fmt.Errorf("parse usage spec: %w", err)
	}
	wireInputByShort := map[string]string{}
	formatFlagByShort := map[string]bool{}
	spec.Walk(func(_ []string, cmd usage.Command) {
		opKey := cmd.Node.Props["opKey"].String()
		if opKey == "" {
			return
		}
		if flag := cmd.Node.Props["wireInput"].String(); flag != "" {
			wireInputByShort[opKey] = flag
		}
		for _, f := range cmd.Flags {
			for _, long := range f.ParseUsage().Long {
				if long == "format" {
					formatFlagByShort[opKey] = true
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
		Sources: map[string]openbindings.Source{
			"usage": {Format: usageFormat, Content: usageContent},
		},
		Bindings: map[string]openbindings.BindingEntry{},
	}

	var unbound []string
	for key, op := range contract.Operations {
		bound.Operations[key] = op
		short := key[strings.LastIndex(key, ".")+1:]
		ref, ok := refByShort[short]
		if !ok {
			unbound = append(unbound, key)
			continue
		}
		be := openbindings.BindingEntry{
			Operation: key,
			Source:    "usage",
			Ref:       ref,
		}
		if flag, ok := wireInputByShort[short]; ok {
			// $$ (the root of the operation input), not $: inside an object
			// constructor the context is a sequence, and $string would
			// serialize a one-element array.
			be.InputTransform = &openbindings.TransformOrRef{
				Inline: fmt.Sprintf("{ %q: $string($$) }", flag),
			}
		} else if formatFlagByShort[short] && !inputHasFormatField(contract, op) {
			// Exec-lane output conformance: the CLI renders text by default,
			// and the usage transport wraps non-JSON stdout as {stdout: ...} —
			// which fails the operation's output schema (OBI-T-08). Forcing
			// the machine format makes commands whose -F json lane is
			// wire-true emit contract-shaped output. Skipped when the
			// operation's own input carries a `format` field (root-flag
			// shadowing; those ops are handled individually).
			be.InputTransform = &openbindings.TransformOrRef{
				Inline: `$merge([$type($$) = "object" ? $$ : {}, {"format": "json"}])`,
			}
		}
		bound.Bindings[key+".usage"] = be
	}
	// Operations with no CLI command (e.g. nothing in usage.kdl) stay unbound;
	// that's expected, not an error.
	_ = unbound

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
