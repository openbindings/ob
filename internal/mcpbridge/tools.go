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
)

// DefaultToolDeadline bounds a single bridged tool/resource/prompt call when
// the caller does not configure one. MCP primitives are request-scoped; an
// operation whose binding streams without end (a subscription) can never
// complete a tool call, so the drain is bounded rather than left hanging
// until the agent's own client gives up.
const DefaultToolDeadline = 60 * time.Second

// RegisterOptions configures how an interface is bridged.
type RegisterOptions struct {
	// ToolDeadline bounds each bridged call's drain (zero = DefaultToolDeadline).
	// The bridge adapts stream-scoped invocations to request-scoped MCP
	// primitives; boundedness is that adapter's job, not a service timeout.
	ToolDeadline time.Duration
}

func (o RegisterOptions) deadline() time.Duration {
	if o.ToolDeadline > 0 {
		return o.ToolDeadline
	}
	return DefaultToolDeadline
}

// drainOperation drives an operation invocation to completion: it writes the
// input (when non-nil), closes the input side, and collects every output,
// bounded by the register options' tool deadline.
//
// MCP tool/resource/prompt results are request/response, so a streaming
// operation's outputs are surfaced as a JSON array — a unary operation's single
// output is returned as-is (a scalar), preserving the common shape, while a
// multi-output operation returns the FULL sequence rather than silently
// dropping all but the last value. A terminal error before EOF surfaces as the
// MCP error (collected outputs are discarded, matching how callers render it).
// A drain that outlives the deadline returns an honest refusal naming the
// mismatch: a subscription-style operation cannot complete as a tool call.
func drainOperation(ctx context.Context, call openbindings.Invocation[any, any], input any, opKey string, deadline time.Duration) (any, *openbindings.InvocationError) {
	dctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	if input != nil {
		_ = call.Write(dctx, input)
	}
	_ = call.Close()
	out := call.Outputs()
	var outputs []any
	for {
		v, err := out.Read(dctx)
		if errors.Is(err, io.EOF) {
			switch len(outputs) {
			case 0:
				return nil, nil
			case 1:
				return outputs[0], nil
			default:
				return outputs, nil
			}
		}
		if err != nil {
			if dctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
				call.Cancel()
				return nil, &openbindings.InvocationError{
					Code: openbindings.ErrCodeTimeout,
					Message: fmt.Sprintf(
						"operation %q streamed for %s without completing (%d event(s) collected); MCP tool calls are request-scoped, and a subscription-style operation cannot complete as a tool — invoke it through an OpenBindings consumer that speaks streams (ob operation invoke, the SDKs)",
						opKey, deadline, len(outputs)),
				}
			}
			return nil, openbindings.AsInvocationError(err)
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
	names := toolNames(iface)

	// Deterministic registration order (map iteration is randomized).
	opKeys := make([]string, 0, len(iface.Operations))
	for k := range iface.Operations {
		opKeys = append(opKeys, k)
	}
	sort.Strings(opKeys)

	count := 0
	for _, opKey := range opKeys {
		op := iface.Operations[opKey]
		ref, kind := findMCPBinding(iface, opKey)
		name := names[opKey]

		switch kind {
		case "resources", "resourceTemplates":
			registerResource(srv, name, op, iface, opKey, ref, invoker, baseContext, opts)
		case "prompts":
			registerPrompt(srv, name, op, iface, opKey, ref, invoker, baseContext, opts)
		default:
			registerTool(srv, name, op, iface, opKey, invoker, baseContext, opts)
		}
		count++
	}
	return count
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
			name = fmt.Sprintf("%s_%d", base, i)
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
func findMCPBinding(iface *openbindings.Interface, opKey string) (ref string, kind string) {
	for _, be := range iface.Bindings {
		if be.Operation != opKey {
			continue
		}
		src, ok := iface.Sources[be.Source]
		if !ok {
			continue
		}
		if !strings.HasPrefix(src.BindingSpec, "mcp") {
			continue
		}
		// Found an MCP binding. Parse the ref prefix. resourceTemplates/ is
		// checked before resources/ for clarity (the two cannot prefix-collide).
		for _, prefix := range []string{"resourceTemplates/", "resources/", "prompts/", "tools/"} {
			if strings.HasPrefix(be.Ref, prefix) {
				return be.Ref, strings.TrimSuffix(prefix, "/")
			}
		}
		return be.Ref, "tools"
	}
	return "", "tools"
}

func registerTool(
	srv *mcp.Server,
	toolName string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
) {
	srv.AddTool(&mcp.Tool{
		Name:        toolName,
		Description: op.Description,
		InputSchema: bundleInputSchema(op.Input, iface.Schemas),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "invalid arguments: " + err.Error()}},
				}, nil
			}
		}

		call := openbindings.Invoke(ctx, invoker, iface,
			openbindings.NewOperationSignature[any, any](opKey),
			openbindings.WithContext(baseContext))
		lastData, ierr := drainOperation(ctx, call, input, opKey, opts.deadline())
		if ierr != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: ierr.Message}},
			}, nil
		}

		data, err := json.Marshal(lastData)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to marshal output: %v", err)}},
			}, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil
	})
}

func registerResource(
	srv *mcp.Server,
	name string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	ref string,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
) {
	// Trim whichever resource-family prefix the ref carries: a static resource
	// (resources/<uri>) or a resource template (resourceTemplates/<uriTemplate>).
	uri := ref
	if strings.HasPrefix(uri, "resourceTemplates/") {
		uri = strings.TrimPrefix(uri, "resourceTemplates/")
	} else {
		uri = strings.TrimPrefix(uri, "resources/")
	}

	srv.AddResource(&mcp.Resource{
		URI:         uri,
		Name:        name,
		Description: op.Description,
		MIMEType:    guessMIME(uri),
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		call := openbindings.Invoke(ctx, invoker, iface,
			openbindings.NewOperationSignature[any, any](opKey),
			openbindings.WithContext(baseContext))
		lastData, ierr := drainOperation(ctx, call, map[string]any{"uri": req.Params.URI}, opKey, opts.deadline())
		if ierr != nil {
			return nil, fmt.Errorf("%s: %s", ierr.Code, ierr.Message)
		}

		text := ""
		switch v := lastData.(type) {
		case string:
			text = v
		default:
			b, _ := json.MarshalIndent(v, "", "  ")
			text = string(b)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: guessMIME(req.Params.URI),
				Text:     text,
			}},
		}, nil
	})
}

func registerPrompt(
	srv *mcp.Server,
	name string,
	op openbindings.Operation,
	iface *openbindings.Interface,
	opKey string,
	ref string,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
	opts RegisterOptions,
) {
	promptName := strings.TrimPrefix(ref, "prompts/")

	var args []*mcp.PromptArgument
	inputObj, _ := op.Input.(map[string]any)
	if props, ok := inputObj["properties"].(map[string]any); ok {
		for k := range props {
			args = append(args, &mcp.PromptArgument{Name: k})
		}
	}

	srv.AddPrompt(&mcp.Prompt{
		Name:        promptName,
		Description: op.Description,
		Arguments:   args,
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		var input any
		if len(req.Params.Arguments) > 0 {
			m := make(map[string]any, len(req.Params.Arguments))
			for k, v := range req.Params.Arguments {
				m[k] = v
			}
			input = m
		}

		call := openbindings.Invoke(ctx, invoker, iface,
			openbindings.NewOperationSignature[any, any](opKey),
			openbindings.WithContext(baseContext))
		lastData, ierr := drainOperation(ctx, call, input, opKey, opts.deadline())
		if ierr != nil {
			return nil, fmt.Errorf("%s: %s", ierr.Code, ierr.Message)
		}

		// The operation invoker returns the prompt result as an object with
		// "messages" and optional "description".
		result := &mcp.GetPromptResult{}
		b, _ := json.Marshal(lastData)
		json.Unmarshal(b, result)
		return result, nil
	})
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
// every transitively-referenced shared schema under "$defs", rewriting
// "#/schemas/X" -> "#/$defs/X". Cyclic schemas are handled (each is added once).
func bundleInputSchema(input openbindings.JSONSchema, schemas map[string]openbindings.JSONSchema) any {
	// Boolean and non-object schema forms carry no refs to bundle; MCP tool
	// schemas want a concrete object root, so treat them like an absent
	// contract (true/{} accept everything; false has no MCP rendering).
	inputObj, isObj := input.(map[string]any)
	if !isObj || len(inputObj) == 0 {
		return map[string]any{"type": "object"}
	}
	root, ok := deepCopyJSON(inputObj).(map[string]any)
	if !ok {
		return map[string]any{"type": "object"}
	}

	// Resolve a top-level $ref to a shared schema so the root is concrete
	// (type/properties/required), which is the most broadly-accepted tool shape.
	if name, ok := schemaRefName(root["$ref"]); ok {
		if target, ok := schemas[name].(map[string]any); ok {
			if cp, ok := deepCopyJSON(target).(map[string]any); ok {
				root = cp
			}
		}
	}

	// Bundle transitively-referenced shared schemas under $defs.
	defs := map[string]any{}
	var walk func(node any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if name, ok := schemaRefName(n["$ref"]); ok {
				n["$ref"] = "#/$defs/" + name
				if _, seen := defs[name]; !seen {
					if target, ok := schemas[name].(map[string]any); ok {
						cp, _ := deepCopyJSON(target).(map[string]any)
						defs[name] = cp
						walk(cp)
					}
				}
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(root)

	if len(defs) > 0 {
		root["$defs"] = defs
	}
	if _, ok := root["type"]; !ok {
		root["type"] = "object"
	}
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
	return name, true
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
