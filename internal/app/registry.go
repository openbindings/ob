package app

import (
	"context"
	"fmt"
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
		usage.NewInvoker(),
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
	// Least privilege: the resolver hands back only challenge-scoped context
	// (openbindings.ScopeContext) — the credentials the satisfied alternative
	// needs plus non-secret config, never other stored credentials — and a
	// binding invoker never gets raw store access.
	invoker.ContextResolver = CLIContextResolver()
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
		usage.NewSynthesizer(),
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
