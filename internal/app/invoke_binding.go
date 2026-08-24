package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// InvokeBindingInput holds the parameters for invoking an operation via its binding.
type InvokeBindingInput struct {
	OpKey     string
	Interface *openbindings.Interface
	InputData map[string]any
}

// InvokeBindingResult holds the output and error from invoking a binding.
type InvokeBindingResult struct {
	Output string
	Error  error
}

// InvokeBinding resolves the sole invocable binding for an operation, applies
// input/output transforms, and invokes the operation. Ambiguous operations
// require the caller to use the explicit-binding invocation surface. This is
// the domain logic that both the TUI and CLI can share.
//
// Context resolution is handled by the operation invoker via the ContextStore
// and PlatformCallbacks wired into the DefaultInvoker.
func InvokeBinding(ctx context.Context, in InvokeBindingInput) InvokeBindingResult {
	if in.Interface == nil {
		return InvokeBindingResult{Error: fmt.Errorf("no interface")}
	}

	resolved, err := resolveBindingAndSource(in.Interface, in.OpKey, "", in.InputData)
	if err != nil {
		return InvokeBindingResult{Error: err}
	}

	es, err := resolveSourceLocation(resolved.source)
	if err != nil {
		return InvokeBindingResult{Error: err}
	}

	if es.Location == "" && es.Content == nil {
		return InvokeBindingResult{Error: fmt.Errorf("binding source %q has no artifact or inline content", resolved.binding.Source)}
	}

	execInput := InvocationInput{
		Source:   InvokeSource{BindingSpec: es.BindingSpec, Location: es.Location, Content: es.Content},
		Selector: resolved.binding.Selector,
		Input:    resolved.input,
	}

	result := InvokeOperationWithContext(ctx, execInput)

	output := result.Output
	if resolved.binding.OutputTransform != nil && result.Error == nil {
		transformed, tErr := ApplyTransform(in.Interface.Transforms, resolved.binding.OutputTransform, output)
		if tErr != nil {
			return InvokeBindingResult{
				Output: FormatOpOutput(output),
				Error:  fmt.Errorf("output transform failed: %w", tErr),
			}
		}
		output = transformed
	}

	res := InvokeBindingResult{
		Output: FormatOpOutput(output),
	}

	if result.Error != nil {
		res.Error = fmt.Errorf("%s", result.Error.Message)
	} else if result.Status != 0 {
		res.Error = fmt.Errorf("exit status %d", result.Status)
	}

	return res
}

// FormatOpOutput converts an operation output value to a display string.
func FormatOpOutput(output any) string {
	if output == nil {
		return ""
	}

	switch o := output.(type) {
	case string:
		return o
	case map[string]any:
		var result strings.Builder
		if stdout, ok := o["stdout"].(string); ok && stdout != "" {
			result.WriteString(stdout)
		}
		if stderr, ok := o["stderr"].(string); ok && stderr != "" {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(stderr)
		}
		if result.Len() > 0 {
			return result.String()
		}
		b, err := json.MarshalIndent(o, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", o)
		}
		return string(b)
	default:
		b, err := json.MarshalIndent(o, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", o)
		}
		return string(b)
	}
}

// ResolveBindingInvocation builds the wire-lane invocation input for a named
// binding in a document: the source resolved (embedded content, or a paired
// or absolute location), the binding's declared inputTransform applied (it
// is part of the binding itself, not of operation validation, and required
// to construct a valid wire request), and NO operation contract threaded.
// The result invokes BELOW the operation boundary — this is not an operation
// invocation, so OBI-T-07/T-08, which apply when invoking an operation, have
// no subject here — and its output is the source's own value, post-decode,
// pre-outputTransform: the wire truth.
// This is `ob binding invoke <obi> <binding-key>`, the porcelain twin of the
// machine lane's wholesale --input envelope.
func ResolveBindingInvocation(obiPath, bindingKey string, input any) (InvocationInput, error) {
	iface, err := resolveInterface(obiPath)
	if err != nil {
		return InvocationInput{}, fmt.Errorf("load OBI %q: %w", obiPath, err)
	}
	resolved, err := resolveBindingAndSource(iface, "", bindingKey, input)
	if err != nil {
		return InvocationInput{}, err
	}
	es, err := resolveSourceLocation(resolved.source)
	if err != nil {
		return InvocationInput{}, err
	}
	return InvocationInput{
		Source:   InvokeSource{BindingSpec: es.BindingSpec, Location: es.Location, Content: es.Content},
		Selector: resolved.binding.Selector,
		Input:    resolved.input,
	}, nil
}
