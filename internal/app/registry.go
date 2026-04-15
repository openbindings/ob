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
	operationgraph "github.com/openbindings/openbindings-go/formats/operationgraph"
	"github.com/openbindings/openbindings-go/formats/openapi"
	"github.com/openbindings/openbindings-go/formats/usage"
	workersrpc "github.com/openbindings/openbindings-go/formats/workersrpc"
)

var (
	defaultExecutor     *openbindings.OperationExecutor
	defaultExecutorOnce sync.Once

	defaultCreator     openbindings.InterfaceCreator
	defaultCreatorOnce sync.Once

	// newExecutorFunc builds the OperationExecutor. Override in tests to
	// inject a custom set of binding executors.
	newExecutorFunc = newDefaultExecutor

	// newCreatorFunc builds the combined InterfaceCreator. Override in tests
	// to inject a custom set of creators.
	newCreatorFunc = newDefaultCreator
)

func newDefaultExecutor() *openbindings.OperationExecutor {
	exec := openbindings.NewOperationExecutor(
		openapi.NewExecutor(),
		grpc.NewExecutor(),
		connectbinding.NewExecutor(),
		mcp.NewExecutor(mcp.WithClientVersion(OBVersion)),
		asyncapi.NewExecutor(),
		graphqlbinding.NewExecutor(),
		usage.NewExecutor(),
		// workers-rpc executor stub: ob recognizes the format token and
		// codegen produces clients for workers-rpc OBIs, but actual
		// dispatch is impossible from Go (Workers RPC requires the
		// Workers runtime). Real dispatch happens via @openbindings/workers-rpc
		// from inside a Cloudflare Worker. See workers-rpc-go/executor.go.
		workersrpc.NewExecutor(),
	)
	// Operation graph executor needs the OperationExecutor itself (recursive:
	// operation nodes invoke sub-operations). Register after construction.
	exec.AddBindingExecutor(operationgraph.NewExecutor(exec))
	exec.TransformEvaluator = &jsonataEvaluator{}
	exec.ContextStore = NewCLIContextStore()
	exec.PlatformCallbacks = CLIPlatformCallbacks()
	return exec
}

func newDefaultCreator() openbindings.InterfaceCreator {
	return openbindings.CombineCreators(
		openapi.NewCreator(),
		asyncapi.NewCreator(),
		grpc.NewCreator(),
		connectbinding.NewCreator(),
		mcp.NewCreator(mcp.WithCreatorClientVersion(OBVersion)),
		graphqlbinding.NewCreator(),
		usage.NewCreator(),
		// workers-rpc creator stub: workers-rpc OBIs are hand-authored
		// (the contract is the WorkerEntrypoint TS class on the target
		// Worker, not a machine-readable spec) so the creator returns
		// an error directing users to write the OBI manually. The
		// registration here makes ob recognize the format token without
		// rejecting it as unknown.
		workersrpc.NewCreator(),
	)
}

// DefaultCreator returns the singleton combined InterfaceCreator wired with
// all built-in format creators.
func DefaultCreator() openbindings.InterfaceCreator {
	defaultCreatorOnce.Do(func() {
		defaultCreator = newCreatorFunc()
	})
	return defaultCreator
}

// CreateInterfaceFromSource routes interface creation to the appropriate
// creator by format.
func CreateInterfaceFromSource(ctx context.Context, input *openbindings.CreateInput) (*openbindings.Interface, error) {
	return DefaultCreator().CreateInterface(ctx, input)
}

// ListBindableRefs returns all bindable refs for a source by delegating to
// the matching creator's RefLister implementation.
func ListBindableRefs(ctx context.Context, source *openbindings.Source) (*openbindings.ListRefsResult, error) {
	creator := DefaultCreator()
	lister, ok := creator.(openbindings.RefLister)
	if !ok {
		return nil, fmt.Errorf("creator does not support ref listing")
	}
	return lister.ListBindableRefs(ctx, source)
}

// DefaultExecutor returns the singleton OperationExecutor wired with all
// built-in binding executors. In tests, override newExecutorFunc
// before calling DefaultExecutor to inject a custom executor.
func DefaultExecutor() *openbindings.OperationExecutor {
	defaultExecutorOnce.Do(func() {
		defaultExecutor = newExecutorFunc()
	})
	return defaultExecutor
}

// ResetDefaultExecutor clears the cached executor and creator so the next
// call to DefaultExecutor/DefaultCreator re-initialises them. Intended for
// tests only.
func ResetDefaultExecutor() {
	defaultExecutorOnce = sync.Once{}
	defaultExecutor = nil
	defaultCreatorOnce = sync.Once{}
	defaultCreator = nil
}

// OverrideExecutorForTest replaces the default executor with the given one
// and returns a cleanup function that restores the original constructor.
// Also resets the cached native-token list so BuiltinSupportsFormat picks
// up the new executor's formats. Intended for tests only.
func OverrideExecutorForTest(exec *openbindings.OperationExecutor) func() {
	old := newExecutorFunc
	ResetDefaultExecutor()
	resetNativeTokens()
	newExecutorFunc = func() *openbindings.OperationExecutor { return exec }
	return func() {
		newExecutorFunc = old
		ResetDefaultExecutor()
		resetNativeTokens()
	}
}
