package app

import (
	"context"
	"fmt"
	"strings"
	"sync"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/invoke"
	obsdk "github.com/openbindings/openbindings-go/sdk"

	"github.com/openbindings/openbindings-go/synthesize"

	"github.com/openbindings/openbindings-go/formats/asyncapi"
	connectbinding "github.com/openbindings/openbindings-go/formats/connect"
	graphqlbinding "github.com/openbindings/openbindings-go/formats/graphql"
	"github.com/openbindings/openbindings-go/formats/grpc"
	"github.com/openbindings/openbindings-go/formats/mcp"
	"github.com/openbindings/openbindings-go/formats/openapi"
	operationgraph "github.com/openbindings/openbindings-go/formats/operationgraph"
	"github.com/openbindings/openbindings-go/formats/usage"
)

var (
	defaultRuntime     *cliRuntime
	defaultRuntimeOnce sync.Once

	// defaultInvokerOverride preserves the app package's narrow test seam.
	// Production always reads the invoker owned by defaultRuntime.
	defaultInvokerOverride *invoke.OperationInvoker
)

// cliRuntime is ob's composition root. The SDK runtime owns the cohesive
// OpenAPI provider; the synthesis view temporarily includes the remaining
// split format implementations until those packages expose cohesive providers
// of their own. Both views contain the exact same OpenAPI Adapter instance.
type cliRuntime struct {
	SDK         *obsdk.Runtime
	Synthesizer synthesize.InterfaceSynthesizer
	OpenAPI     *openapi.Adapter
	Recognizers []func([]byte) (string, bool, error)

	preparedMu        sync.Mutex
	preparedProviders map[string]*invoke.PreparedProvider
	preparedOrder     []string
}

const maxPreparedProviders = 64

func newDefaultRuntime() *cliRuntime {
	openAPI := openapi.NewAdapterWithOptions(openapi.AdapterOptions{
		Invoker: openapi.InvokerOptions{ParameterConversion: openapi.DecimalParameterConversion},
		// Authoring reads are distinct from live API calls. Reuse ob's
		// redirect-hop guard for remote OpenAPI documents; live invocation
		// retains the adapter's context-cancelled client with no overall
		// timeout, as required by the standalone client contract.
		SynthesisHTTPClient: GuardedHTTPClient(0),
	})
	runtime, err := obsdk.New(obsdk.RuntimeOptions{
		Providers:          []obsdk.BindingProvider{openAPI},
		HTTPClient:         GuardedHTTPClient(0),
		TransformEvaluator: &jsonataEvaluator{},
		ContextResolver:    CLIContextResolver(),
	})
	if err != nil {
		// The provider list above is a compile-time composition invariant, not
		// user input. Failing here means the binary was assembled incorrectly.
		panic(fmt.Sprintf("assemble OpenBindings runtime: %v", err))
	}

	invoker := runtime.OperationInvoker()
	// The other format packages still publish split invoker/synthesizer
	// implementations. Register only their invocation halves here; OpenAPI is
	// already present through the cohesive provider and MUST NOT be added again.
	invoker.AddBindingInvoker(grpc.NewInvoker())
	invoker.AddBindingInvoker(connectbinding.NewInvoker())
	invoker.AddBindingInvoker(mcp.NewInvoker(mcp.WithClientVersion(OBVersion)))
	invoker.AddBindingInvoker(asyncapi.NewInvoker())
	invoker.AddBindingInvoker(graphqlbinding.NewInvoker())
	invoker.AddBindingInvoker(newUsageInvoker())
	// Operation graph invoker needs the OperationInvoker itself (recursive:
	// operation nodes invoke sub-operations). Register after construction.
	invoker.AddBindingInvoker(operationgraph.NewInvoker(invoker))
	// ob's own consumer configuration: the site-guarded hook table for the
	// bound CLI OBI (specification + configuration = complete invocation —
	// the elections the pristine usage.kdl cannot express). Guarded to
	// usage-family sites targeting ob's own binary, so foreign bindings
	// see only the SDK defaults.
	if bound, err := OpenBindingsInterface(); err == nil {
		InstallBoundCLIHooks(invoker, &bound)
	}

	synthesizer := synthesize.CombineSynthesizers(
		openAPI,
		asyncapi.NewSynthesizer(),
		grpc.NewSynthesizer(),
		connectbinding.NewSynthesizer(),
		mcp.NewSynthesizer(mcp.WithSynthesizerClientVersion(OBVersion)),
		graphqlbinding.NewSynthesizer(),
		newUsageSynthesizer(),
	)
	return &cliRuntime{
		SDK:               runtime,
		Synthesizer:       synthesizer,
		OpenAPI:           openAPI,
		Recognizers:       []func([]byte) (string, bool, error){openAPI.RecognizeRepresentation},
		preparedProviders: make(map[string]*invoke.PreparedProvider),
	}
}

// prepareProvider snapshots an interface once per canonical revision and
// retains the generic provider catalog for this process. The catalog closes
// exact realizations lazily; live credentials and configuration remain
// per-invocation and are never cached here.
func (r *cliRuntime) prepareProvider(iface *openbindings.Interface) (*invoke.PreparedProvider, error) {
	prepared, err := openbindings.PrepareInterface(iface)
	if err != nil {
		return nil, err
	}
	revision := prepared.Revision()
	r.preparedMu.Lock()
	defer r.preparedMu.Unlock()
	if provider := r.preparedProviders[revision]; provider != nil {
		return provider, nil
	}
	provider, err := invoke.PrepareProvider(invoke.PreparedProviderOptions{
		Key:       revision,
		Label:     "ob interface " + revision,
		Interface: prepared,
		Runtime:   DefaultInvoker(),
	})
	if err != nil {
		return nil, err
	}
	r.preparedProviders[revision] = provider
	r.preparedOrder = append(r.preparedOrder, revision)
	if len(r.preparedOrder) > maxPreparedProviders {
		oldest := r.preparedOrder[0]
		r.preparedOrder = r.preparedOrder[1:]
		delete(r.preparedProviders, oldest)
	}
	return provider, nil
}

func defaultCLIRuntime() *cliRuntime {
	defaultRuntimeOnce.Do(func() {
		defaultRuntime = newDefaultRuntime()
	})
	return defaultRuntime
}

// DefaultRuntime returns ob's process-wide protocol-neutral SDK runtime. It is
// the owner of OpenAPI invocation, synthesis, and inspection configuration.
func DefaultRuntime() *obsdk.Runtime {
	return defaultCLIRuntime().SDK
}

// DefaultSynthesizer returns the singleton combined InterfaceSynthesizer wired with
// all built-in format synthesizers.
func DefaultSynthesizer() synthesize.InterfaceSynthesizer {
	return defaultCLIRuntime().Synthesizer
}

// SynthesizeInterfaceFromSource routes interface creation to the appropriate
// synthesizer by format, falling through to a synthesize-capable delegate when the
// format is not natively supported.
func SynthesizeInterfaceFromSource(ctx context.Context, input *synthesize.SynthesizeInput) (*openbindings.Interface, error) {
	if iface, routed, err := synthesizeViaDelegate(ctx, input); routed {
		return iface, err
	}
	if runtimeOwnsSynthesis(input) {
		return DefaultRuntime().SynthesizeInterface(ctx, input)
	}
	return DefaultSynthesizer().SynthesizeInterface(ctx, input)
}

// InspectSource returns bindable targets for a source, delegating to the
// matching native inspector, or to an inspect-capable delegate when the format
// is not natively supported.
func InspectSource(ctx context.Context, source *openbindings.Source) (*synthesize.SourceInspection, error) {
	if ins, routed, err := inspectViaDelegate(ctx, source); routed {
		return ins, err
	}
	if source != nil && DefaultRuntime().SupportsBindingSpec(source.BindingSpec) {
		return DefaultRuntime().InspectSource(ctx, source)
	}
	synthesizer := DefaultSynthesizer()
	inspector, ok := synthesizer.(synthesize.SourceInspector)
	if !ok {
		return nil, fmt.Errorf("synthesizer does not support source inspection")
	}
	return inspector.InspectSource(ctx, source)
}

func runtimeOwnsSynthesis(input *synthesize.SynthesizeInput) bool {
	if input == nil || len(input.Sources) == 0 {
		return false
	}
	for _, source := range input.Sources {
		if !DefaultRuntime().SupportsBindingSpec(source.BindingSpec) {
			return false
		}
	}
	return true
}

// DefaultInvoker returns the OperationInvoker owned by the singleton SDK
// runtime and wired with all built-in binding invokers.
func DefaultInvoker() *invoke.OperationInvoker {
	if defaultInvokerOverride != nil {
		return defaultInvokerOverride
	}
	return DefaultRuntime().OperationInvoker()
}

// ResetDefaultInvoker clears the cached composition root. Intended for tests.
func ResetDefaultInvoker() {
	defaultRuntimeOnce = sync.Once{}
	defaultRuntime = nil
	defaultInvokerOverride = nil
}

// OverrideInvokerForTest replaces the default invoker with the given one
// and returns a cleanup function that restores the original constructor.
// Also resets the cached native-token list so BuiltinSupportsFormat picks
// up the new invoker's formats. Intended for tests only.
func OverrideInvokerForTest(invoker *invoke.OperationInvoker) func() {
	old := defaultInvokerOverride
	ResetDefaultInvoker()
	resetNativeTokens()
	defaultInvokerOverride = invoker
	return func() {
		ResetDefaultInvoker()
		defaultInvokerOverride = old
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
