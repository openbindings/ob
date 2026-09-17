package app

import (
	"github.com/openbindings/openbindings-go/formats/usage"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// Delegate-local codes describe this implementation's trust and connection
// mechanics. They are intentionally not exported as a cross-binding SDK
// taxonomy.
const (
	errCodeDelegateTargetRefused = "ERR_DELEGATE_TARGET_REFUSED"
	errCodeDelegateAuthRequired  = "ERR_DELEGATE_AUTH_REQUIRED"
)

// The retired locator-era frame/CLI transports lived here. Role-aware dispatch
// invokes a registration's retained OBI through the SDK's prepared provider
// routes (role_runtime.go, role_binding_invoker.go); nothing here re-derives a
// provider by location or collapses the frame protocol into a unary command.

// delegateExecInvoker returns a shallow copy of the default invoker
// configured for the delegate exec lane. ob-as-consumer of the published
// binding-invoker interface knows the delegate CLI's machine lane emits
// JSON (`binding invoke` prints JSON values on stdout), so the copy
// carries a strict JSON decoder for the lane's usage sites — consumer
// configuration (specification + configuration = complete invocation),
// never a payload sniff in the format builtin. Classification stays the
// builtin exit-0 rule: a delegate's handled failures surface as non-zero
// exits with the captured output in Details.
func delegateExecInvoker(delegate string) *invoke.OperationInvoker {
	lane := DefaultInvoker().WithRuntime(nil) // blessed shallow copy; runtime fields ride
	lane.OutputDecoder = func(site invoke.InvokeSite, raw invoke.RawResult) (any, error) {
		if site.BindingSpec != usage.BindingSpec {
			return nil, invoke.ErrUseDefault
		}
		if len(raw.Body) == 0 {
			return nil, nil
		}
		var v any
		if err := jsonvalue.Unmarshal(raw.Body, &v); err != nil {
			return nil, &invoke.InvocationError{
				Code: invoke.ErrCodeResponseError,
			}
		}
		return v, nil
	}
	return lane
}
