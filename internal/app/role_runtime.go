package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// roleRuntime retains one admitted application identity and the SDK's immutable
// provider/composition snapshots. There is no registry lookup inside invocation,
// no content-addressed provider identity, and no disposal on registry mutation.
// Future lookups construct fresh snapshots; already retained calls may finish.
type roleRuntime struct {
	candidate roleCandidate
	provider  *invoke.PreparedProvider
	session   *invoke.CompositionSession
}

// roleConsumerInterface is a real consuming OBI, distinct from the unbound
// accepted interface advertised by listRoles. No binding-spec restrictions are
// invented here: installed executable support is the provider runtime's job.
func roleConsumerInterface(catalogue *roleCatalogue, candidate roleCandidate) (*openbindings.Interface, error) {
	for _, alternative := range catalogue.alternatives[candidate.Role] {
		if alternative.identity != candidate.Admission.Alternative {
			continue
		}
		consumer, err := openbindings.ValidateDocument(alternative.value)
		if err != nil {
			return nil, errors.New("invalid retained role expectation")
		}
		consumer.Name = "OB " + candidate.Role + " consumer"
		consumer.Dependencies = make(map[string]openbindings.DependencyEntry, len(consumer.Operations))
		for key := range consumer.Operations {
			consumer.Dependencies[key] = openbindings.DependencyEntry{Operation: key}
		}
		return consumer, nil
	}
	return nil, errors.New("admitted alternative is no longer in the role catalogue")
}

func newRoleRuntime(catalogue *roleCatalogue, candidate roleCandidate, runtime invoke.ProviderRuntime, selector invoke.RealizationSelector) (*roleRuntime, error) {
	// Own the policy/correspondence values as well as the SDK document. A
	// caller retaining the candidate slice cannot mutate an in-flight route.
	snapshot, err := jsonvalue.Marshal(candidate)
	if err != nil {
		return nil, errors.New("cannot retain role candidate")
	}
	var retained roleCandidate
	if err := jsonvalue.Unmarshal(snapshot, &retained); err != nil {
		return nil, errors.New("cannot retain role candidate")
	}
	candidate = retained
	if candidate.CatalogueIdentity != catalogue.identity || candidate.Record.ID == "" || candidate.ScopeIdentity == "" || candidate.ConfigIdentity == "" {
		return nil, errors.New("role runtime needs a complete authoritative registry snapshot")
	}
	consumer, err := roleConsumerInterface(catalogue, candidate)
	if err != nil {
		return nil, err
	}
	preparedConsumer, err := openbindings.PrepareInterface(consumer)
	if err != nil {
		return nil, err
	}
	providerValue, err := openbindings.ValidateDocument(candidate.Record.Interface)
	if err != nil {
		return nil, errors.New("invalid retained provider interface")
	}
	preparedValue, err := openbindings.PrepareInterface(providerValue)
	if err != nil {
		return nil, err
	}
	identity, err := jsonvalue.Marshal([]any{candidate.ScopeIdentity, candidate.Record.ID, candidate.Role, candidate.RegistryRevision, candidate.CatalogueIdentity, candidate.ConfigIdentity})
	if err != nil {
		return nil, err
	}
	provider, err := invoke.PrepareProvider(invoke.PreparedProviderOptions{
		Key: HashContent(identity), Label: candidate.Record.ID, Interface: preparedValue,
		Runtime: runtime, SelectRealization: selector,
	})
	if err != nil {
		return nil, err
	}
	// OB ranks exact JSON preferences before selecting a provider. The SDK
	// session deliberately has one provider at baseline, never a rounded weight.
	session, err := invoke.NewCompositionSession(invoke.CompositionSessionOptions{
		Consumer: preparedConsumer, Providers: []invoke.ProviderRegistration{{Provider: provider}},
	})
	if err != nil {
		return nil, err
	}
	return &roleRuntime{candidate: candidate, provider: provider, session: session}, nil
}

func (r *roleRuntime) route(ctx context.Context, expectedKey string) (*invoke.PreparedDependencyRoute[any, any], error) {
	admittedKey, found := r.candidate.Admission.Operations[expectedKey]
	if !found {
		return nil, errors.New("operation was not admitted for the selected role")
	}
	result, err := invoke.ResolveDependency(ctx, r.session, invoke.NewDynamicDependencySignature(expectedKey))
	if err != nil {
		return nil, err
	}
	if result.Status != invoke.DependencyAvailable || result.Route == nil {
		// Do not dump embedded documents, inputs, credentials, or runtime error
		// payloads. Keep the refusal separate from an authoritative false verdict.
		return nil, fmt.Errorf("delegate %s has no executable unambiguous realization for %s (%s)", r.candidate.Record.ID, expectedKey, result.Status)
	}
	if result.Route.ProviderOperationKey != admittedKey || result.Route.ProviderKey != r.provider.Key() {
		return nil, errors.New("dependency resolution diverged from admitted correspondence")
	}
	return result.Route, nil
}

func (r *roleRuntime) unary(ctx context.Context, expectedKey string, input any) (any, error) {
	route, err := r.route(ctx, expectedKey)
	if err != nil {
		return nil, err
	}
	call := route.Invoke(ctx)
	defer call.Cancel()
	// An operation without an input (listBindingSpecs) is invoked by closing
	// the input side; nil is never written as a value.
	if input != nil {
		if err := call.Write(ctx, input); err != nil {
			return nil, err
		}
	}
	if err := call.Close(); err != nil {
		return nil, err
	}
	return invoke.Single(ctx, call.Outputs())
}

// explicitRoleRuntime prepares exactly one enrolled registration for a role
// without querying support: the seam for explicit-selection flows that must
// discover the provider's own advertised tokens first (source detection).
// Missing enrollment, wrong role, unbound or invalid state all refuse; there
// is no locator, display-name, native or alternate-provider fallback.
func explicitRoleRuntime(ctx context.Context, role DelegateCapability, registrationID string) (*roleRuntime, error) {
	if registrationID == "" {
		return nil, errors.New("registration ID is required")
	}
	if _, known := capabilityOperation[role]; !known {
		return nil, errors.New("unknown delegate role")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	envPath, _, err := FindEnvironment()
	if err != nil {
		return nil, err
	}
	catalogue, err := defaultRoleCatalogue()
	if err != nil {
		return nil, err
	}
	registry := &roleRegistry{path: envPath, catalogue: catalogue}
	rows, err := registry.candidates(string(role))
	if err != nil {
		return nil, err
	}
	for _, candidate := range rows {
		if candidate.Record.ID != registrationID {
			continue
		}
		runtime, err := newRoleRuntime(catalogue, candidate, delegateExecInvoker(candidate.Record.ID), nil)
		if err != nil {
			return nil, err
		}
		inspection, err := runtime.session.InspectDependency(ctx, capabilityOperation[role])
		if err != nil {
			return nil, err
		}
		if len(inspection.Providers) == 0 {
			return nil, errors.New("requested registration has no executable realization for this role")
		}
		return runtime, nil
	}
	return nil, errors.New("requested registration is not enrolled for this role in the active environment")
}

// decodeRoleSupport requires actual boolean verdicts; decoding straight into a
// Go bool would mistake a missing field for an authoritative supported:false.
func decodeRoleSupport(value any, tokens []string) ([]openbindings.BindingSpecVerdict, error) {
	raw, err := jsonvalue.Marshal(value)
	if err != nil {
		return nil, errors.New("support query returned an invalid value")
	}
	var rows []map[string]any
	if err := jsonvalue.Unmarshal(raw, &rows); err != nil || rows == nil {
		return nil, errors.New("support query must return an array of verdicts")
	}
	verdicts := make([]openbindings.BindingSpecVerdict, len(rows))
	for i, row := range rows {
		token, tokenOK := row["bindingSpec"].(string)
		supported, supportedOK := row["supported"].(bool)
		if !tokenOK || !supportedOK {
			return nil, errors.New("support verdict requires bindingSpec and boolean supported")
		}
		verdicts[i] = openbindings.BindingSpecVerdict{BindingSpec: token, Supported: supported}
	}
	if err := validateBindingSpecVerdicts(tokens, verdicts); err != nil {
		return nil, err
	}
	return verdicts, nil
}

func checkBindingSpecOperationNames(cap DelegateCapability) []string {
	switch cap {
	case CapInvoke:
		return []string{"openbindings.binding-invoker.checkBindingSpecs", "checkBindingSpecs"}
	case CapSynthesize, CapInspect:
		return []string{"openbindings.interface-synthesizer.checkBindingSpecs", "checkBindingSpecs"}
	default:
		return []string{"checkBindingSpecs"}
	}
}

// validateBindingSpecVerdicts requires one verdict per requested exact token in
// request order; a provider answering for other tokens is malformed.
func validateBindingSpecVerdicts(bindingSpecs []string, verdicts []openbindings.BindingSpecVerdict) error {
	expected := openbindings.CheckBindingSpecs(bindingSpecs, nil)
	if len(verdicts) != len(expected) {
		return fmt.Errorf("got %d verdicts, want %d", len(verdicts), len(expected))
	}
	for i := range expected {
		if verdicts[i].BindingSpec != expected[i].BindingSpec {
			return fmt.Errorf("verdict %d names %q, want exact token %q", i, verdicts[i].BindingSpec, expected[i].BindingSpec)
		}
	}
	return nil
}

// roleRoutingPath names OB policy, not an OBI or shared-interface concept.
// Keeping it explicit prevents a diagnostic from claiming to explain a path
// whose built-in/external precedence is different.
type roleRoutingPath string

const (
	roleRanked      roleRoutingPath = "ranked"
	roleNativeFirst roleRoutingPath = "native-first"
)

type roleSelection struct {
	Builtin bool
	Runtime *roleRuntime
	Work    *invoke.PreparedDependencyRoute[any, any]
}

type roleRuntimeFactory func(roleCandidate) (invoke.ProviderRuntime, invoke.RealizationSelector)

// selectInstalledRole is the application join: discover only the active
// environment and retain its by-value role records. A missing configuration is
// an empty inventory, but corrupt/legacy state is never swallowed. It does not
// resolve registration locators or recursively consult the delegate registry to
// execute a delegate's own bindings. Native-first paths do not query providers.
func selectInstalledRole(ctx context.Context, role DelegateCapability, token string, path roleRoutingPath) (*roleSelection, error) {
	selected, err := selectInstalledRoles(ctx, role, []string{token}, path)
	return selected[token], err
}

// selectInstalledRoles retains one registry snapshot and one support-to-work
// identity per provider across the whole batch. Operation binding selection
// uses these same routes at dispatch; it never elects a provider a second time.
func selectInstalledRoles(ctx context.Context, role DelegateCapability, tokens []string, path roleRoutingPath) (map[string]*roleSelection, error) {
	return selectInstalledRolesFrom(ctx, role, tokens, path, "")
}

// A registration constraint is an exact identity in the active registry, never
// a locator, display name, or x-ob source hint. Filter it before provider calls.
func selectInstalledRolesFrom(ctx context.Context, role DelegateCapability, tokens []string, path roleRoutingPath, registrationID string) (map[string]*roleSelection, error) {
	if err := validateRoleLookup(ctx, role, tokens, path); err != nil {
		return nil, err
	}
	if role == CapInvoke && ctx.Value(nativeInvocationRoutingKey{}) == true {
		if registrationID != "" {
			return nil, errors.New("pinned-provider work cannot select a registry invoker")
		}
		selected := make(map[string]*roleSelection, len(tokens))
		for _, token := range tokens {
			if BuiltinSupportsFormat(token) {
				selected[token] = &roleSelection{Builtin: true}
			}
		}
		return selected, nil
	}
	nativeOnly := registrationID == "" && path == roleNativeFirst
	for _, token := range tokens {
		nativeOnly = nativeOnly && BuiltinSupportsFormat(token)
	}
	if nativeOnly {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		selected := make(map[string]*roleSelection, len(tokens))
		for _, token := range tokens {
			selected[token] = &roleSelection{Builtin: true}
		}
		return selected, nil
	}
	envPath, _, err := FindEnvironment()
	if err != nil {
		return nil, err
	}
	catalogue, err := defaultRoleCatalogue()
	if err != nil {
		return nil, err
	}
	return selectRoleRuntimesFrom(ctx, &roleRegistry{path: envPath, catalogue: catalogue}, role, tokens, path, registrationID, BuiltinSupportsFormat,
		func(candidate roleCandidate) (invoke.ProviderRuntime, invoke.RealizationSelector) {
			// Retain the existing Usage machine-output configuration and native
			// exec/egress/context protections. Registration itself grants none.
			return delegateExecInvoker(candidate.Record.ID), nil
		})
}

func (s *roleSelection) unary(ctx context.Context, input any) (any, error) {
	if s == nil || s.Builtin || s.Work == nil {
		return nil, errors.New("no selected external operation")
	}
	call := s.Work.Invoke(ctx)
	defer call.Cancel()
	if err := call.Write(ctx, input); err != nil {
		return nil, err
	}
	if err := call.Close(); err != nil {
		return nil, err
	}
	return invoke.Single(ctx, call.Outputs())
}

func selectRoleRuntime(ctx context.Context, registry *roleRegistry, role DelegateCapability, token string, path roleRoutingPath, nativeSupported bool, factory roleRuntimeFactory) (*roleSelection, error) {
	selected, err := selectRoleRuntimes(ctx, registry, role, []string{token}, path, func(string) bool { return nativeSupported }, factory)
	return selected[token], err
}

func selectRoleRuntimes(ctx context.Context, registry *roleRegistry, role DelegateCapability, tokens []string, path roleRoutingPath, nativeSupports func(string) bool, factory roleRuntimeFactory) (map[string]*roleSelection, error) {
	return selectRoleRuntimesFrom(ctx, registry, role, tokens, path, "", nativeSupports, factory)
}

func validateRoleLookup(ctx context.Context, role DelegateCapability, tokens []string, path roleRoutingPath) error {
	if _, known := capabilityOperation[role]; !known {
		return errors.New("unknown delegate role")
	}
	if path != roleRanked && path != roleNativeFirst {
		return errors.New("unknown delegate routing policy")
	}
	for _, token := range tokens {
		if token == "" {
			return errors.New("binding specification is required")
		}
	}
	return ctx.Err()
}

func selectRoleRuntimesFrom(ctx context.Context, registry *roleRegistry, role DelegateCapability, tokens []string, path roleRoutingPath, registrationID string, nativeSupports func(string) bool, factory roleRuntimeFactory) (map[string]*roleSelection, error) {
	if err := validateRoleLookup(ctx, role, tokens, path); err != nil {
		return nil, err
	}
	selected := make(map[string]*roleSelection, len(tokens))
	preferences := make(map[string]json.Number, len(tokens))
	requested := make([]string, 0, len(tokens))
	for _, verdict := range openbindings.CheckBindingSpecs(tokens, nil) {
		token := verdict.BindingSpec
		if registrationID == "" && nativeSupports(token) {
			selected[token] = &roleSelection{Builtin: true}
			preferences[token] = "0"
			if path == roleNativeFirst {
				continue
			}
		}
		requested = append(requested, token)
	}
	if len(requested) == 0 {
		return selected, nil
	}
	rows, err := registry.candidates(string(role))
	if err != nil {
		return nil, err
	}
	if registrationID != "" {
		var enrolled []roleCandidate
		for _, candidate := range rows {
			if candidate.Record.ID == registrationID {
				enrolled = append(enrolled, candidate)
				break
			}
		}
		if len(enrolled) == 0 {
			return nil, errors.New("requested registration is not enrolled for this role in the active environment")
		}
		rows = enrolled
	}
	for _, candidate := range rows {
		runtime, selector := factory(candidate)
		provider, err := newRoleRuntime(registry.catalogue, candidate, runtime, selector)
		if err != nil {
			return nil, err
		}
		inspection, err := provider.session.InspectDependency(ctx, capabilityOperation[role])
		if err != nil {
			return nil, err
		}
		if len(inspection.Providers) == 0 {
			if registrationID != "" {
				return nil, errors.New("requested registration has no executable realization for this role")
			}
			continue
		}
		// Close the actual workload realization before asking for support. No
		// workload or context is disclosed by static closure. An admitted but
		// unbound provider cannot warrant executable handling here.
		work, err := provider.route(ctx, capabilityOperation[role])
		if err != nil {
			return nil, err
		}
		query := checkBindingSpecOperationNames(role)[0]
		value, err := provider.unary(ctx, query, map[string]any{"bindingSpecs": requested})
		if err != nil {
			return nil, fmt.Errorf("delegate %s support assessment failed", candidate.Record.ID)
		}
		verdicts, err := decodeRoleSupport(value, requested)
		if err != nil {
			return nil, fmt.Errorf("delegate %s support assessment is malformed", candidate.Record.ID)
		}
		for _, verdict := range verdicts {
			if !verdict.Supported {
				continue
			}
			token := verdict.BindingSpec
			p := candidate.preference(token)
			if selected[token] != nil {
				order, err := jsonvalue.CompareNumbers(p, preferences[token])
				if err != nil {
					return nil, errors.New("delegate preference cannot be compared exactly")
				}
				if order <= 0 {
					continue
				} // built-in first, then stable registration order
			}
			selected[token], preferences[token] = &roleSelection{Runtime: provider, Work: work}, p
		}
	}
	return selected, nil
}
