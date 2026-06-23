package app

import (
	"fmt"
	"os"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
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
		bound.Bindings[key+".usage"] = openbindings.BindingEntry{
			Operation: key,
			Source:    "usage",
			Ref:       ref,
		}
	}
	// Operations with no CLI command (e.g. nothing in usage.kdl) stay unbound;
	// that's expected, not an error.
	_ = unbound

	return bound, nil
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
