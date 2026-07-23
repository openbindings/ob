package app

import (
	"context"
	"fmt"
	"strings"
	"sync"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/formats/asyncapi"
	connectbinding "github.com/openbindings/openbindings-go/formats/connect"
	graphqlbinding "github.com/openbindings/openbindings-go/formats/graphql"
	"github.com/openbindings/openbindings-go/formats/grpc"
	"github.com/openbindings/openbindings-go/formats/mcp"
	"github.com/openbindings/openbindings-go/formats/openapi"
	operationgraph "github.com/openbindings/openbindings-go/formats/operationgraph"
	"github.com/openbindings/openbindings-go/formats/usage"
	workersrpc "github.com/openbindings/openbindings-go/formats/workersrpc"
)

var (
	defaultInvoker     *openbindings.OperationInvoker
	defaultInvokerOnce sync.Once

	defaultSynthesizer     openbindings.InterfaceSynthesizer
	defaultSynthesizerOnce sync.Once

	// newInvokerFunc builds the OperationInvoker. Override in tests to
	// inject a custom set of binding invokers.
	newInvokerFunc = newDefaultInvoker

	// newSynthesizerFunc builds the combined InterfaceSynthesizer. Override in tests
	// to inject a custom set of synthesizers.
	newSynthesizerFunc = newDefaultSynthesizer
)

func newDefaultInvoker() *openbindings.OperationInvoker {
	invoker := openbindings.NewOperationInvoker(
		openapi.NewInvoker(),
		grpc.NewInvoker(),
		connectbinding.NewInvoker(),
		mcp.NewInvoker(mcp.WithClientVersion(OBVersion)),
		asyncapi.NewInvoker(),
		graphqlbinding.NewInvoker(),
		newUsageInvoker(),
		// workers-rpc invoker stub: ob recognizes the format token and
		// codegen produces clients for workers-rpc OBIs, but actual
		// dispatch is impossible from Go (Workers RPC requires the
		// Workers runtime). Real dispatch happens via @openbindings/workers-rpc
		// from inside a Cloudflare Worker. See workers-rpc-go/invoker.go.
		workersrpc.NewInvoker(),
	)
	// Operation graph invoker needs the OperationInvoker itself (recursive:
	// operation nodes invoke sub-operations). Register after construction.
	invoker.AddBindingInvoker(operationgraph.NewInvoker(invoker))
	invoker.TransformEvaluator = &jsonataEvaluator{}
	// ContextResolver drives CONTEXT_REQUIRED negotiation. NOTE: the app layer
	// reaches bindings via invoker.InvokeBinding and owns its own bounded
	// resolve-replay loop (driveBinding), so the SDK's operation-layer loop in
	// invoker.Invoke stays dormant on that path. If any app-layer code is ever
	// moved onto invoker.Invoke, remove driveBinding's loop first — otherwise
	// both loops fire and a single challenge is retried up to 3×3 times.
	//
	// Least privilege: the resolver hands back only context fields named by
	// the satisfied challenge alternative (openbindings.ScopeContext), and a
	// binding invoker never gets raw store access.
	invoker.ContextResolver = CLIContextResolver()
	// ob's own consumer configuration: the site-guarded hook table for the
	// bound CLI OBI (specification + configuration = complete invocation —
	// the elections the pristine usage.kdl cannot express). Guarded to
	// usage-family sites targeting ob's own binary, so foreign bindings
	// see only the SDK defaults.
	if bound, err := OpenBindingsInterface(); err == nil {
		InstallBoundCLIHooks(invoker, &bound)
	}
	return invoker
}

func newDefaultSynthesizer() openbindings.InterfaceSynthesizer {
	return openbindings.CombineSynthesizers(
		openapi.NewSynthesizer(),
		asyncapi.NewSynthesizer(),
		grpc.NewSynthesizer(),
		connectbinding.NewSynthesizer(),
		mcp.NewSynthesizer(mcp.WithSynthesizerClientVersion(OBVersion)),
		graphqlbinding.NewSynthesizer(),
		newUsageSynthesizer(),
		// workers-rpc synthesizer stub: workers-rpc OBIs are hand-authored
		// (the contract is the WorkerEntrypoint TS class on the target
		// Worker, not a machine-readable spec) so the synthesizer returns
		// an error directing users to write the OBI manually. The
		// registration here makes ob recognize the format token without
		// rejecting it as unknown.
		workersrpc.NewSynthesizer(),
	)
}

// DefaultSynthesizer returns the singleton combined InterfaceSynthesizer wired with
// all built-in format synthesizers.
func DefaultSynthesizer() openbindings.InterfaceSynthesizer {
	defaultSynthesizerOnce.Do(func() {
		defaultSynthesizer = newSynthesizerFunc()
	})
	return defaultSynthesizer
}

// SynthesizeInterfaceFromSource routes interface creation to the appropriate
// synthesizer by format, falling through to a synthesize-capable delegate when the
// format is not natively supported.
func SynthesizeInterfaceFromSource(ctx context.Context, input *openbindings.SynthesizeInput) (*openbindings.Interface, error) {
	if iface, routed, err := synthesizeViaDelegate(ctx, input); routed {
		return iface, err
	}
	return DefaultSynthesizer().SynthesizeInterface(ctx, input)
}

// InspectSource returns bindable targets for a source, delegating to the
// matching native inspector, or to an inspect-capable delegate when the format
// is not natively supported.
func InspectSource(ctx context.Context, source *openbindings.Source) (*openbindings.SourceInspection, error) {
	if ins, routed, err := inspectViaDelegate(ctx, source); routed {
		return ins, err
	}
	synthesizer := DefaultSynthesizer()
	inspector, ok := synthesizer.(openbindings.SourceInspector)
	if !ok {
		return nil, fmt.Errorf("synthesizer does not support source inspection")
	}
	return inspector.InspectSource(ctx, source)
}

// DefaultInvoker returns the singleton OperationInvoker wired with all
// built-in binding invokers. In tests, override newInvokerFunc
// before calling DefaultInvoker to inject a custom invoker.
func DefaultInvoker() *openbindings.OperationInvoker {
	defaultInvokerOnce.Do(func() {
		defaultInvoker = newInvokerFunc()
	})
	return defaultInvoker
}

// ResetDefaultInvoker clears the cached invoker and synthesizer so the next
// call to DefaultInvoker/DefaultSynthesizer re-initialises them. Intended for
// tests only.
func ResetDefaultInvoker() {
	defaultInvokerOnce = sync.Once{}
	defaultInvoker = nil
	defaultSynthesizerOnce = sync.Once{}
	defaultSynthesizer = nil
}

// OverrideInvokerForTest replaces the default invoker with the given one
// and returns a cleanup function that restores the original constructor.
// Also resets the cached native-token list so BuiltinSupportsFormat picks
// up the new invoker's formats. Intended for tests only.
func OverrideInvokerForTest(invoker *openbindings.OperationInvoker) func() {
	old := newInvokerFunc
	ResetDefaultInvoker()
	resetNativeTokens()
	newInvokerFunc = func() *openbindings.OperationInvoker { return invoker }
	return func() {
		newInvokerFunc = old
		ResetDefaultInvoker()
		resetNativeTokens()
	}
}

// authorizeExecAddress is ob's USAGE-P-02 policy: an exec address may be
// dereferenced when the operator explicitly authorized it — recorded in the
// environment config's authorizedExec list (intake records addresses the
// operator types), or standing via delegate registration (a registered
// delegate's own exec location, and exec sources in its pinned interface,
// were authorized by the explicit act of registering it). Anything else is
// refused, per the specification's default.
func authorizeExecAddress(argv []string) bool {
	address := "exec:" + strings.Join(argv, " ")
	if envPath, err := FindEnvPath(); err == nil {
		if config, err := LoadEnvConfig(envPath); err == nil {
			for _, allowed := range config.AuthorizedExec {
				if allowed == address {
					return true
				}
			}
			for _, rec := range config.Delegates {
				if rec.Location == address {
					return true
				}
			}
		}
	}
	return false
}

func newUsageInvoker() *usage.Invoker {
	inv := usage.NewInvoker()
	inv.AuthorizeExec = authorizeExecAddress
	return inv
}

func newUsageSynthesizer() *usage.Synthesizer {
	s := usage.NewSynthesizer()
	s.AuthorizeExec = authorizeExecAddress
	return s
}

// RecordAuthorizedExec records an exec address in the environment config's
// authorizedExec list — the durable form of the operator's explicit
// USAGE-P-02 authorization. Idempotent; the CLI layer calls it exactly when
// the OPERATOR typed the address (machine lanes never auto-authorize).
func RecordAuthorizedExec(address string) error {
	envPath, err := FindEnvPath()
	if err != nil {
		return err
	}
	config, err := LoadEnvConfig(envPath)
	if err != nil {
		return err
	}
	for _, existing := range config.AuthorizedExec {
		if existing == address {
			return nil
		}
	}
	config.AuthorizedExec = append(config.AuthorizedExec, address)
	return SaveEnvConfig(envPath, config)
}
