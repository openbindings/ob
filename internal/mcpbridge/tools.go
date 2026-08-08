// Package mcpbridge maps OBI interfaces to MCP servers.
package mcpbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
	mcpbinding "github.com/openbindings/openbindings-go/formats/mcp"
)

// DefaultToolDeadline bounds a single bridged tool/resource/prompt call when
// the caller does not configure one. MCP primitives are request-scoped; an
// operation whose binding streams without end (a subscription) can never
// complete a tool call, so the drain is bounded rather than left hanging
// until the agent's own client gives up.
const DefaultToolDeadline = 60 * time.Second

// MCPBindingSpec is the exact binding-specification identifier whose
// protocol-native results and primitive families this bridge knows how to
// preserve. Binding-spec identifiers are opaque and exact in OpenBindings;
// shorthand and prefix matching would accidentally claim compatibility with
// unevaluated revisions.
const MCPBindingSpec = "openbindings.mcp@1"

// RegisterOptions configures how an interface is bridged.
type RegisterOptions struct {
	// ToolDeadline bounds each bridged call's drain (zero = DefaultToolDeadline).
	// The bridge adapts stream-scoped invocations to request-scoped MCP
	// primitives; boundedness is that adapter's job, not a service timeout.
	ToolDeadline time.Duration
}

// RegistrationEntry records the bridge's disposition of one OBI operation.
// It is adapter evidence: it reports only facts knowable from the interface
// and installed invokers, never guesses a binding's runtime cardinality.
type RegistrationEntry struct {
	Operation string `json:"operation"`
	Name      string `json:"name,omitempty"`
	Primitive string `json:"primitive,omitempty"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
}

// RegistrationReport is the complete, deterministic admission report for one
// bridged interface.
type RegistrationReport struct {
	Entries    []RegistrationEntry `json:"entries"`
	Registered int                 `json:"registered"`
	Excluded   int                 `json:"excluded"`
}

func (o RegisterOptions) deadline() time.Duration {
	if o.ToolDeadline > 0 {
		return o.ToolDeadline
	}
	return DefaultToolDeadline
}

type drainedOperation struct {
	Outputs []any
	Error   *openbindings.InvocationError
}

// drainOperation drives one cardinality-agnostic OpenBindings invocation to
// completion. The complete output sequence is retained even when the
// invocation later terminates with an error; OpenBindings does not retract
// already-emitted values, so an adapter must not discard them either.
func drainOperation(ctx context.Context, call openbindings.Invocation[any, any], input operationInput, opKey string, deadline time.Duration) drainedOperation {
	dctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	if input.Present {
		_ = call.Write(dctx, input.Value)
	}
	_ = call.Close()
	out := call.Outputs()
	var outputs []any
	for {
		v, err := out.Read(dctx)
		if errors.Is(err, io.EOF) {
			return drainedOperation{Outputs: outputs}
		}
		if err != nil {
			call.Cancel()
			if dctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
				return drainedOperation{Outputs: outputs, Error: &openbindings.InvocationError{
					Code: openbindings.ErrCodeTimeout,
					Message: fmt.Sprintf(
						"operation %q streamed for %s without completing (%d event(s) collected); MCP tool calls are request-scoped, and a subscription-style operation cannot complete as a tool — invoke it through an OpenBindings consumer that speaks streams (ob operation invoke, the SDKs)",
						opKey, deadline, len(outputs)),
				}}
			}
			return drainedOperation{Outputs: outputs, Error: openbindings.AsInvocationError(err)}
		}
		outputs = append(outputs, v)
	}
}

// RegisterInterface maps a single OBI's operations to MCP primitives on the
// given server. Operations with MCP bindings are registered as the correct
// primitive type based on binding ref prefix (tools/, resources/, resourceTemplates/, prompts/);
// operations without MCP bindings are registered as tools. Tool/resource names
// are the operation key, sanitized to the protocol's charset (see toolNames) —
// the bridge does not namespace by interface: federating multiple services is
// done by composing an aggregate OBI (deliberate keys/aliases) and bridging
// that, not by merging at runtime.
//
// Returns the number of primitives registered.
// baseContext is per-call invocation context (e.g. a bearer credential for the
// bridged remote) merged into every operation invocation. nil when none.
func RegisterInterface(
	srv *mcp.Server,
	iface *openbindings.Interface,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
) int {
	return RegisterInterfaceWithReport(srv, iface, invoker, baseContext, opts).Registered
}

// RegisterInterfaceWithReport is RegisterInterface with explicit evidence for
// every advertised or excluded operation.
func RegisterInterfaceWithReport(
	srv *mcp.Server,
	iface *openbindings.Interface,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
) RegistrationReport {
	if iface == nil {
		return RegistrationReport{}
	}
	names := toolNames(iface)

	// Deterministic registration order (map iteration is randomized).
	opKeys := make([]string, 0, len(iface.Operations))
	for k := range iface.Operations {
		opKeys = append(opKeys, k)
	}
	sort.Strings(opKeys)

	report := RegistrationReport{Entries: make([]RegistrationEntry, 0, len(opKeys))}
	for _, opKey := range opKeys {
		op := iface.Operations[opKey]
		// Spec-loyal binding selection (see selection.go): exclude only the
		// genuinely unadvertisable; a multi-binding operation is advertised and
		// its choice exposed in-band rather than silently dropped.
		res := resolveOperationBinding(iface, opKey, invoker, baseContext)
		if res.exclude {
			report.Entries = append(report.Entries, RegistrationEntry{
				Operation: opKey,
				Status:    "excluded",
				Reason:    res.reason,
			})
			report.Excluded++
			continue
		}
		binding, nativeMCP := findMCPBindingDetails(iface, opKey)
		// A transformed MCP binding no longer exposes the protocol-native
		// argument/result boundary. Present it as the abstract OBI operation,
		// just like any other binding, rather than pretending its transformed
		// value is still a CallToolResult/ReadResourceResult/GetPromptResult.
		if nativeMCP && (binding.entry.InputTransform != nil || binding.entry.OutputTransform != nil) {
			nativeMCP = false
			binding = mcpBinding{kind: "tools"}
		}
		ref, kind := binding.ref, binding.kind
		name := names[opKey]
		if nativeMCP && kind == "tools" {
			name = strings.TrimPrefix(ref, "tools/")
		}

		switch kind {
		case "resources":
			registerStaticResource(srv, name, op, iface, opKey, binding, invoker, baseContext, opts)
		case "resourceTemplates":
			registerResourceTemplate(srv, name, op, iface, opKey, binding, invoker, baseContext, opts)
		case "prompts":
			registerPrompt(srv, name, op, iface, opKey, binding, invoker, baseContext, opts)
		default:
			registerTool(srv, name, op, iface, opKey, binding, nativeMCP, invoker, baseContext, opts, res)
		}
		report.Entries = append(report.Entries, RegistrationEntry{
			Operation: opKey,
			Name:      name,
			Primitive: kind,
			Status:    "registered",
		})
		report.Registered++
	}
	return report
}

func contextSelection(ctx map[string]any) []string {
	configuration, _ := ctx["configuration"].(map[string]any)
	switch values := configuration["selection"].(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if key, ok := value.(string); ok {
				out = append(out, key)
			}
		}
		return out
	default:
		return nil
	}
}

// toolNames assigns each operation an MCP tool name that stays as close to the
// operation key as the protocol's charset allows. The bridge makes NO assumption
// about an interface's key convention — dotted reverse-DNS keys
// ("openbindings.ob.describe") are the OpenBindings Project's choice, not
// something the spec mandates — so it sanitizes whatever key an author used
// rather than stripping a presumed namespace. Names are restricted to
// [A-Za-z0-9_-] (every other rune, including the dots in a namespaced key,
// becomes "_") and capped at 64 characters, the constraint LLM tool-calling APIs
// (Anthropic, OpenAI) impose. So "openbindings.ob.describe" becomes
// "openbindings_ob_describe" and a bare key like "echo" is unchanged. A numeric
// suffix disambiguates the rare case where two keys sanitize to the same name.
func toolNames(iface *openbindings.Interface) map[string]string {
	opKeys := make([]string, 0, len(iface.Operations))
	for k := range iface.Operations {
		opKeys = append(opKeys, k)
	}
	sort.Strings(opKeys) // deterministic suffixing

	out := make(map[string]string, len(opKeys))
	used := map[string]bool{}
	for _, k := range opKeys {
		name := sanitizeName(k)
		base := name
		for i := 2; used[name]; i++ {
			suffix := fmt.Sprintf("_%d", i)
			prefix := base
			if len(prefix)+len(suffix) > 64 {
				prefix = prefix[:64-len(suffix)]
			}
			name = prefix + suffix
		}
		used[name] = true
		out[k] = name
	}
	return out
}

// sanitizeName restricts s to [A-Za-z0-9_-] (replacing other runes with "_") and
// caps it at 64 characters, matching the tool-name constraint common to LLM
// tool-calling APIs.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		name = "op"
	}
	return name
}

// findMCPBinding looks for an MCP binding for the given operation and returns
// the ref value and the entity kind (tools, resources, prompts). If no MCP
// binding exists, returns ("", "tools").
type mcpBinding struct {
	entry  openbindings.BindingEntry
	source openbindings.Source
	ref    string
	kind   string
}

func findMCPBindingDetails(iface *openbindings.Interface, opKey string) (mcpBinding, bool) {
	var candidate mcpBinding
	bindingCount := 0
	for _, be := range iface.Bindings {
		if be.Operation != opKey {
			continue
		}
		bindingCount++
		src, ok := iface.Sources[be.Source]
		if !ok {
			continue
		}
		if src.BindingSpec != MCPBindingSpec {
			continue
		}
		// Found an MCP binding. Parse the ref prefix. resourceTemplates/ is
		// checked before resources/ for clarity (the two cannot prefix-collide).
		for _, prefix := range []string{"resourceTemplates/", "resources/", "prompts/", "tools/"} {
			if strings.HasPrefix(be.Ref, prefix) {
				candidate = mcpBinding{
					entry: be, source: src, ref: be.Ref,
					kind: strings.TrimSuffix(prefix, "/"),
				}
				break
			}
		}
		if candidate.kind == "" {
			candidate = mcpBinding{entry: be, source: src, ref: be.Ref, kind: "tools"}
		}
	}
	// Re-emitting a protocol-native primitive also commits the invocation to
	// the MCP binding whose descriptor supplied that primitive. OpenBindings
	// deliberately refuses to choose among several valid bindings, so the
	// bridge may take this lane only when the operation has one unambiguous
	// binding. Multi-binding operations stay generic and preserve normal
	// selection semantics instead of acquiring an MCP-preference convention.
	if bindingCount == 1 && candidate.kind != "" {
		return candidate, true
	}
	return mcpBinding{kind: "tools"}, false
}

func findMCPBinding(iface *openbindings.Interface, opKey string) (ref string, kind string) {
	binding, _ := findMCPBindingDetails(iface, opKey)
	return binding.ref, binding.kind
}

func registerTool(
	srv *mcp.Server,
	toolName string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	binding mcpBinding,
	nativeMCP bool,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
	res bindingResolution,
) {
	projection := projectGenericTool(op, iface.Schemas)
	descriptor := &mcp.Tool{
		Name:         toolName,
		Description:  op.Description,
		InputSchema:  projection.InputSchema,
		OutputSchema: projection.OutputSchema,
	}
	if nativeMCP {
		descriptor.OutputSchema = nil
		if pinned := pinnedTool(binding.source.Content, strings.TrimPrefix(binding.ref, "tools/")); pinned != nil {
			descriptor = pinned
		} else if outputSchema := toolStructuredOutputSchema(op.Output); outputSchema != nil {
			descriptor.OutputSchema = outputSchema
		}
	}
	if descriptor.InputSchema == nil {
		descriptor.InputSchema = bundleInputSchema(op.Input, iface.Schemas)
	}
	// An operation realized over several bindings is advertised (never dropped)
	// and ALWAYS exposes the choice in-band via an optional `_binding` argument
	// — including when the bridge has a loyal preselect. Honor AND expose: the
	// spec calls `preference` a signal and defines no selection algorithm, so
	// acting on it must never remove the caller's ability to choose otherwise.
	if len(res.candidates) > 1 {
		descriptor.InputSchema = withBindingArg(descriptor.InputSchema, res.candidates, res.preselect)
	}

	srv.AddTool(descriptor, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Resolve the binding for THIS call, spec-loyally, caller first:
		//   explicit `_binding` -> always wins (validated against candidates)
		//   preselect           -> a loyal auto-choice (context sel / single / preference)
		//   neither             -> a LOUD in-band refusal naming the valid keys
		rawArgs := req.Params.Arguments
		var selection []string
		if len(res.candidates) > 1 {
			choice, cleaned, xerr := extractBindingArg(rawArgs)
			if xerr != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: xerr.Error()}}}, nil
			}
			rawArgs = cleaned
			switch {
			case choice != "":
				valid := false
				for _, c := range res.candidates {
					if c == choice {
						valid = true
						break
					}
				}
				if !valid {
					return invalidBindingResult(choice, res.candidates), nil
				}
				selection = []string{choice}
			case res.preselect != "":
				selection = []string{res.preselect}
			default:
				return ambiguousBindingResult(res.candidates), nil
			}
		} else if res.preselect != "" {
			selection = []string{res.preselect}
		}

		input, err := projection.decodeInput(rawArgs)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil
		}

		invokeCtx := contextWithSelection(baseContext, selection)
		call := openbindings.Invoke(ctx, invoker, iface,
			openbindings.NewOperationSignature[any, any](opKey),
			openbindings.WithContext(mcpToolContext(invokeCtx, nativeMCP && req.Params.GetProgressToken() != nil)))
		var genericResult drainedOperation
		var lastData any
		var nativeFinal bool
		var ierr *openbindings.InvocationError
		if nativeMCP && req.Params.GetProgressToken() != nil && req.Session != nil {
			lastData, nativeFinal, ierr = drainMCPTool(ctx, call, input, opKey, opts.deadline(), req)
		} else {
			genericResult = drainOperation(ctx, call, input, opKey, opts.deadline())
			ierr = genericResult.Error
			if len(genericResult.Outputs) == 1 {
				lastData = genericResult.Outputs[0]
				nativeFinal = true
			}
		}
		if ierr != nil {
			if nativeMCP {
				if result := mcpErrorResult(ierr); result != nil {
					return result, nil
				}
				genericResult.Error = ierr
			}
			return genericToolResult(genericResult), nil
		}

		if nativeMCP {
			if !nativeFinal {
				return nil, fmt.Errorf("MCP binding %q emitted %d final results; exactly one CallToolResult is required", binding.ref, len(genericResult.Outputs))
			}
			result := &mcp.CallToolResult{}
			if err := remarshal(lastData, result); err != nil {
				return nil, fmt.Errorf("MCP binding %q returned an invalid CallToolResult: %w", binding.ref, err)
			}
			return result, nil
		}
		return genericToolResult(genericResult), nil
	})
}

func registerStaticResource(
	srv *mcp.Server,
	name string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	binding mcpBinding,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
) {
	uri := strings.TrimPrefix(binding.ref, "resources/")
	descriptor := &mcp.Resource{
		URI:         uri,
		Name:        name,
		Description: op.Description,
		MIMEType:    guessMIME(uri),
	}
	if pinned := pinnedResource(binding.source.Content, uri); pinned != nil {
		descriptor = pinned
	}

	srv.AddResource(descriptor, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		call := openbindings.Invoke(ctx, invoker, iface,
			openbindings.NewOperationSignature[any, any](opKey),
			openbindings.WithContext(baseContext))
		// A static MCP resource's URI lives in the binding ref. The binding
		// intentionally takes no OpenBindings input value.
		drained := drainOperation(ctx, call, operationInput{}, opKey, opts.deadline())
		if drained.Error != nil {
			return nil, fmt.Errorf("%s: %s", drained.Error.Code, drained.Error.Message)
		}
		if len(drained.Outputs) != 1 {
			return nil, fmt.Errorf("MCP binding %q emitted %d results; exactly one ReadResourceResult is required", binding.ref, len(drained.Outputs))
		}
		result := &mcp.ReadResourceResult{}
		if err := remarshal(drained.Outputs[0], result); err != nil {
			return nil, fmt.Errorf("MCP binding %q returned an invalid ReadResourceResult: %w", binding.ref, err)
		}
		return result, nil
	})
}

func registerResourceTemplate(
	srv *mcp.Server,
	name string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	binding mcpBinding,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
) {
	uriTemplate := strings.TrimPrefix(binding.ref, "resourceTemplates/")
	descriptor := &mcp.ResourceTemplate{
		URITemplate: uriTemplate,
		Name:        name,
		Description: op.Description,
		MIMEType:    guessMIME(uriTemplate),
	}
	if pinned := pinnedResourceTemplate(binding.source.Content, uriTemplate); pinned != nil {
		descriptor = pinned
	}

	srv.AddResourceTemplate(descriptor, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		// resources/read carries the expanded URI, while the OpenBindings
		// operation takes RFC 6570 variables. RFC 6570 does not define a
		// generally reversible matching algorithm. For an untransformed native
		// MCP binding, forwarding the protocol request is the only faithful
		// adapter; an authored transform would make that bypass incorrect.
		if binding.entry.InputTransform != nil || binding.entry.OutputTransform != nil {
			return nil, fmt.Errorf("MCP resource-template bridge cannot reverse an expanded URI through authored transforms")
		}
		return readMCPResource(ctx, binding.source.Location, req.Params.URI, baseContext)
	})
}

func registerPrompt(
	srv *mcp.Server,
	name string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	binding mcpBinding,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
) {
	promptName := strings.TrimPrefix(binding.ref, "prompts/")

	descriptor := pinnedPrompt(binding.source.Content, promptName)
	if descriptor == nil {
		var args []*mcp.PromptArgument
		inputObj, _ := op.Input.(map[string]any)
		required := stringSet(inputObj["required"])
		if props, ok := inputObj["properties"].(map[string]any); ok {
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				arg := &mcp.PromptArgument{Name: k, Required: required[k]}
				if schema, ok := props[k].(map[string]any); ok {
					arg.Description, _ = schema["description"].(string)
				}
				args = append(args, arg)
			}
		}
		descriptor = &mcp.Prompt{Name: promptName, Description: op.Description, Arguments: args}
	}

	srv.AddPrompt(descriptor, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		var input operationInput
		if len(req.Params.Arguments) > 0 {
			m := make(map[string]any, len(req.Params.Arguments))
			for k, v := range req.Params.Arguments {
				m[k] = v
			}
			input = operationInput{Value: m, Present: true}
		}

		call := openbindings.Invoke(ctx, invoker, iface,
			openbindings.NewOperationSignature[any, any](opKey),
			openbindings.WithContext(baseContext))
		drained := drainOperation(ctx, call, input, opKey, opts.deadline())
		if drained.Error != nil {
			return nil, fmt.Errorf("%s: %s", drained.Error.Code, drained.Error.Message)
		}
		if len(drained.Outputs) != 1 {
			return nil, fmt.Errorf("MCP binding %q emitted %d results; exactly one GetPromptResult is required", binding.ref, len(drained.Outputs))
		}

		// The operation invoker returns the prompt result as an object with
		// "messages" and optional "description".
		result := &mcp.GetPromptResult{}
		if err := remarshal(drained.Outputs[0], result); err != nil {
			return nil, fmt.Errorf("MCP binding %q returned an invalid GetPromptResult: %w", binding.ref, err)
		}
		return result, nil
	})
}

func remarshal(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func mcpErrorResult(ierr *openbindings.InvocationError) *mcp.CallToolResult {
	evidence, ok := mcpbinding.FailureEvidenceFrom(ierr)
	if !ok || evidence.Result == nil {
		return nil
	}
	result := &mcp.CallToolResult{}
	if remarshal(evidence.Result, result) != nil {
		return nil
	}
	return result
}

func toolStructuredOutputSchema(output openbindings.JSONSchema) any {
	root, ok := output.(map[string]any)
	if !ok {
		return nil
	}
	if properties, ok := root["properties"].(map[string]any); ok {
		if schema := properties["structuredContent"]; schema != nil {
			return schema
		}
	}
	if alternatives, ok := root["anyOf"].([]any); ok {
		for _, alternative := range alternatives {
			if schema := toolStructuredOutputSchema(alternative); schema != nil {
				return schema
			}
		}
	}
	return nil
}

func mcpToolContext(base map[string]any, solicit bool) map[string]any {
	if !solicit {
		return base
	}
	out := make(map[string]any, len(base)+1)
	for key, value := range base {
		out[key] = value
	}
	configuration := map[string]any{}
	if existing, ok := base["configuration"].(map[string]any); ok {
		for key, value := range existing {
			configuration[key] = value
		}
	}
	configuration["solicit"] = true
	out["configuration"] = configuration
	return out
}

// drainMCPTool preserves the MCP stream shape. Each non-final OpenBindings
// output is a progress value and is re-correlated to the downstream client's
// token; the last output is the complete CallToolResult.
func drainMCPTool(
	ctx context.Context,
	call openbindings.Invocation[any, any],
	input operationInput,
	opKey string,
	deadline time.Duration,
	req *mcp.CallToolRequest,
) (any, bool, *openbindings.InvocationError) {
	dctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	if input.Present {
		_ = call.Write(dctx, input.Value)
	}
	_ = call.Close()

	var pending any
	pendingPresent := false
	outputs := call.Outputs()
	for {
		value, err := outputs.Read(dctx)
		if errors.Is(err, io.EOF) {
			return pending, pendingPresent, nil
		}
		if err != nil {
			call.Cancel()
			if dctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
				return nil, false, &openbindings.InvocationError{
					Code:    openbindings.ErrCodeTimeout,
					Message: fmt.Sprintf("operation %q did not complete within %s", opKey, deadline),
				}
			}
			return nil, false, openbindings.AsInvocationError(err)
		}
		if pendingPresent {
			progress := &mcp.ProgressNotificationParams{}
			if err := remarshal(pending, progress); err != nil {
				call.Cancel()
				return nil, false, &openbindings.InvocationError{
					Code: openbindings.ErrCodeProtocol, Message: "MCP binding emitted an invalid progress value",
					Details: err.Error(),
				}
			}
			progress.ProgressToken = req.Params.GetProgressToken()
			if err := req.Session.NotifyProgress(dctx, progress); err != nil {
				call.Cancel()
				return nil, false, &openbindings.InvocationError{
					Code: openbindings.ErrCodeProtocol, Message: "failed to forward MCP progress",
					Details: err.Error(),
				}
			}
		}
		pending = value
		pendingPresent = true
	}
}

func stringSet(value any) map[string]bool {
	out := map[string]bool{}
	switch values := value.(type) {
	case []string:
		for _, value := range values {
			out[value] = true
		}
	case []any:
		for _, value := range values {
			if s, ok := value.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

type pinnedMCPListing struct {
	Tools             []*mcp.Tool             `json:"tools"`
	Resources         []*mcp.Resource         `json:"resources"`
	ResourceTemplates []*mcp.ResourceTemplate `json:"resourceTemplates"`
	Prompts           []*mcp.Prompt           `json:"prompts"`
}

func decodePinnedMCPListing(content json.RawMessage) *pinnedMCPListing {
	if content == nil {
		return nil
	}
	var listing pinnedMCPListing
	if json.Unmarshal(content, &listing) != nil {
		return nil
	}
	return &listing
}

func pinnedTool(content json.RawMessage, name string) *mcp.Tool {
	listing := decodePinnedMCPListing(content)
	if listing == nil {
		return nil
	}
	for _, descriptor := range listing.Tools {
		if descriptor != nil && descriptor.Name == name {
			return descriptor
		}
	}
	return nil
}

func pinnedResource(content json.RawMessage, uri string) *mcp.Resource {
	listing := decodePinnedMCPListing(content)
	if listing == nil {
		return nil
	}
	for _, descriptor := range listing.Resources {
		if descriptor != nil && descriptor.URI == uri {
			return descriptor
		}
	}
	return nil
}

func pinnedResourceTemplate(content json.RawMessage, uriTemplate string) *mcp.ResourceTemplate {
	listing := decodePinnedMCPListing(content)
	if listing == nil {
		return nil
	}
	for _, descriptor := range listing.ResourceTemplates {
		if descriptor != nil && descriptor.URITemplate == uriTemplate {
			return descriptor
		}
	}
	return nil
}

func pinnedPrompt(content json.RawMessage, name string) *mcp.Prompt {
	listing := decodePinnedMCPListing(content)
	if listing == nil {
		return nil
	}
	for _, descriptor := range listing.Prompts {
		if descriptor != nil && descriptor.Name == name {
			return descriptor
		}
	}
	return nil
}

func guessMIME(uri string) string {
	switch {
	case strings.HasSuffix(uri, ".json"):
		return "application/json"
	case strings.HasSuffix(uri, ".md"):
		return "text/markdown"
	case strings.HasSuffix(uri, ".yaml"), strings.HasSuffix(uri, ".yml"):
		return "text/yaml"
	default:
		return "text/plain"
	}
}

// bundleInputSchema returns a self-contained JSON Schema for an operation's
// input, suitable as a standalone MCP tool inputSchema. OBI op inputs commonly
// reference shared schemas via "#/schemas/X" (good DRY authoring), but an MCP
// tool schema stands alone, so those refs would dangle and an agent couldn't see
// the fields. This deep-copies the input (never mutating the contract), resolves
// a top-level schema ref so the root is a concrete object schema, and bundles
// every transitively-referenced shared schema under "$defs", rewriting each
// "#/schemas/X" to its collision-safe bundled entry. Cyclic schemas are handled
// (each is added once).
func bundleInputSchema(input openbindings.JSONSchema, schemas map[string]openbindings.JSONSchema) any {
	root, ok := bundleValueSchema(input, schemas).(map[string]any)
	if !ok {
		return map[string]any{"type": "object"}
	}
	if _, ok := root["type"]; !ok {
		root["type"] = "object"
	}
	return root
}

// bundleValueSchema makes any OBI per-value schema self-contained without
// changing its accepted JSON value domain. Unlike bundleInputSchema it does
// not force an object root, so it is suitable inside the generic bridge's
// reversible input/output envelopes.
func bundleValueSchema(schema openbindings.JSONSchema, schemas map[string]openbindings.JSONSchema) any {
	if schema == nil {
		return map[string]any{}
	}
	if value, ok := schema.(bool); ok {
		return value
	}
	schemaObj, isObj := schema.(map[string]any)
	if !isObj {
		return map[string]any{}
	}
	root, ok := deepCopyJSON(schemaObj).(map[string]any)
	if !ok {
		return map[string]any{}
	}

	// Resolve a top-level $ref to a shared schema so the root is concrete
	// and then bundle every nested document schema reference.
	if name, ok := schemaRefName(root["$ref"]); ok {
		if target, exists := schemas[name]; exists {
			switch cp := deepCopyJSON(target).(type) {
			case map[string]any:
				root = cp
			case bool:
				return cp
			}
		}
	}

	// Discover the complete reachable shared-schema set before allocating
	// definition names. Allocation is sorted, so a collision with an authored
	// local $defs entry never makes output depend on Go map iteration.
	reachable := map[string]bool{}
	var collect func(node any)
	collect = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if name, ok := schemaRefName(n["$ref"]); ok {
				if !reachable[name] {
					if target, exists := schemas[name]; exists {
						reachable[name] = true
						collect(target)
					}
				}
			}
			for _, v := range n {
				collect(v)
			}
		case []any:
			for _, v := range n {
				collect(v)
			}
		}
	}
	collect(root)

	if len(reachable) == 0 {
		return root
	}

	defs := map[string]any{}
	if authored, ok := root["$defs"].(map[string]any); ok {
		for key, value := range authored {
			defs[key] = value
		}
	}
	names := make([]string, 0, len(reachable))
	for name := range reachable {
		names = append(names, name)
	}
	sort.Strings(names)

	allocated := make(map[string]string, len(names))
	for _, name := range names {
		key := name
		if _, collision := defs[key]; collision {
			base := "__openbindings_" + name
			key = base
			for suffix := 2; ; suffix++ {
				if _, exists := defs[key]; !exists {
					break
				}
				key = fmt.Sprintf("%s_%d", base, suffix)
			}
		}
		allocated[name] = key
		defs[key] = deepCopyJSON(schemas[name])
	}
	root["$defs"] = defs

	// Rewrite only resolvable OBI document-schema refs. Boolean shared schemas
	// are definitions too; treating only object targets would leave a dangling
	// ref for a valid `schemas: {"Never": false}` contract.
	var rewrite func(node any)
	rewrite = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if name, ok := schemaRefName(n["$ref"]); ok {
				if key, exists := allocated[name]; exists {
					n["$ref"] = "#/$defs/" + jsonPointerToken(key)
				}
			}
			for _, value := range n {
				rewrite(value)
			}
		case []any:
			for _, value := range n {
				rewrite(value)
			}
		}
	}
	rewrite(root)
	return root
}

// schemaRefName returns the shared-schema name X from a whole-schema reference
// "#/schemas/X". It rejects non-string refs, non-"#/schemas/" refs, and
// sub-paths ("#/schemas/X/...") which can't be bundled as a unit.
func schemaRefName(ref any) (string, bool) {
	s, ok := ref.(string)
	if !ok {
		return "", false
	}
	const prefix = "#/schemas/"
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	name := s[len(prefix):]
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	var decoded strings.Builder
	for index := 0; index < len(name); index++ {
		if name[index] != '~' {
			decoded.WriteByte(name[index])
			continue
		}
		if index+1 >= len(name) {
			return "", false
		}
		index++
		switch name[index] {
		case '0':
			decoded.WriteByte('~')
		case '1':
			decoded.WriteByte('/')
		default:
			return "", false
		}
	}
	return decoded.String(), true
}

func jsonPointerToken(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

// deepCopyJSON returns a deep copy of a JSON-serializable value via a marshal
// round-trip, so rewrites never mutate the source schema maps.
func deepCopyJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}
