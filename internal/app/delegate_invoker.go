package app

import (
	"fmt"
	"os"
	"strconv"
	"sync"

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

// Delegation is recursive by construction: a registration is an interface
// value, and ob's own bound OBI corresponds to the binding-invoker role, so an
// operator may legitimately enroll ob as its own delegate. Nothing in a
// by-value registration says which executable a provider's binding names, so
// self-reference cannot be detected by inspecting the registration the way the
// retired location-based surface did. Bound the chain instead of forbidding
// it: each hop marks its children, and a process at the bound refuses to make
// another hop rather than spawning an unbounded chain of delegates.
//
// The marker rides the process environment because that is what a spawned
// delegate inherits (the Usage binding builds the child's environment from
// this process's, plus the invocation's own entries). A child that is not ob
// simply carries an unread variable.
const (
	delegateDepthVar = "OB_DELEGATE_DEPTH"
	maxDelegateDepth = 8
)

// A pointer so a test can install a fresh Once without copying a lock.
var delegateChildDepthOnce = new(sync.Once)

// processDelegateDepth is this process's own depth, captured before anything
// can mark children. Marking writes the same variable children inherit, so a
// process that re-read it would keep counting its own marker as its own depth.
var processDelegateDepth = readDelegateDepth()

// readDelegateDepth reads the inherited marker. An absent, unreadable or
// negative marker is depth zero: a marker can only ever add hops to the count,
// never grant a process more room than it actually has.
func readDelegateDepth() int {
	depth, err := strconv.Atoi(os.Getenv(delegateDepthVar))
	if err != nil || depth < 0 {
		return 0
	}
	return depth
}

// delegateDepth reports how many delegate hops led to this process.
func delegateDepth() int { return processDelegateDepth }

// armDelegateChildDepth refuses a hop past the bound and otherwise marks every
// delegate this process spawns with the next depth. The value is a constant
// for the life of the process, so concurrent invocations set the same marker.
func armDelegateChildDepth() error {
	depth := delegateDepth()
	if depth >= maxDelegateDepth {
		return fmt.Errorf("delegate chain reached its %d-hop bound; a registration that resolves back to this ob would not terminate, so no further delegate is invoked", maxDelegateDepth)
	}
	delegateChildDepthOnce.Do(func() {
		_ = os.Setenv(delegateDepthVar, strconv.Itoa(depth+1))
	})
	return nil
}

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
