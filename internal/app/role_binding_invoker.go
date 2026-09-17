package app

import (
	"context"

	"github.com/openbindings/ob/internal/frames"
	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/asyncapi"
	"github.com/openbindings/openbindings-go/invoke"
)

// roleBindingInvoker is a consumer of the selected Binding Invoker operation,
// not a protocol driver. Its route pins the admitted operation and realization;
// generic SDK machinery invokes that realization, whatever its binding spec.
type roleBindingInvoker struct {
	spec  string
	route *invoke.PreparedDependencyRoute[any, any]
}

func (r *roleBindingInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: r.spec}}
}
func (r *roleBindingInvoker) CheckBindingSpecs(tokens []string) []openbindings.BindingSpecVerdict {
	return openbindings.CheckBindingSpecs(tokens, r.BindingSpecs())
}
func (r *roleBindingInvoker) InvokeBinding(ctx context.Context, args *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	var options []invoke.InvokeOption
	if r.route.BindingSpec == asyncapi.BindingSpec {
		// OB's frame consumer elects JSON text messages. This is consumer
		// configuration, not a guessed artifact default or caller credentials.
		options = append(options, invoke.WithContext(map[string]any{"configuration": map[string]any{"websocketMessageType": "text"}}))
	}
	return frames.InvokeOperation(ctx, r.route.Invoke(ctx, options...), &frames.BindingInvocationInput{
		Source:   frames.InvokeSource{BindingSpec: args.Source.BindingSpec, Location: args.Source.Location, Content: args.Source.Content},
		Selector: args.Selector, Context: args.Context,
	})
}
