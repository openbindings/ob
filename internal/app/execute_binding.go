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
	OBIDir    string
	Interface *openbindings.Interface
	InputData map[string]any
}

// InvokeBindingResult holds the output and error from invoking a binding.
type InvokeBindingResult struct {
	Output string
	Error  error
}

// InvokeBinding resolves the default binding for an operation, applies
// input/output transforms, and invokes the operation. This is the domain
// logic that both the TUI and CLI can share.
//
// Context resolution is handled by the dispatcher via the ContextStore and
// PlatformCallbacks wired into the DefaultInvoker.
func InvokeBinding(ctx context.Context, in InvokeBindingInput) InvokeBindingResult {
	if in.Interface == nil {
		return InvokeBindingResult{Error: fmt.Errorf("no interface")}
	}

	resolved, err := resolveBindingAndSource(in.Interface, in.OpKey, "", in.InputData)
	if err != nil {
		return InvokeBindingResult{Error: err}
	}

	es := resolveSourceLocation(resolved.source, in.OBIDir)

	if es.Location == "" && es.Content == nil {
		return InvokeBindingResult{Error: fmt.Errorf("binding source %q has no artifact or inline content", resolved.binding.Source)}
	}

	execInput := InvokeOperationInput{
		Source: InvokeSource{Format: es.Format, Location: es.Location, Content: es.Content},
		Ref:    resolved.binding.Ref,
		Input:  resolved.input,
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
