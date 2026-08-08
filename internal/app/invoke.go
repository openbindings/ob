package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
	"github.com/openbindings/ob/internal/execref"
)

// InvokeSource represents the binding source for invocation. Content is
// raw JSON with the core's presence semantics (nil = absent member, a
// `null` literal = present null), mirroring the SDK's InvocationSource.
type InvokeSource struct {
	BindingSpec string          `json:"bindingSpec"`
	Location    string          `json:"location,omitempty"`
	Content     json.RawMessage `json:"content,omitempty"`
	Binary      string          `json:"binary,omitempty"` // Optional: binary name hint for CLI invocation
}

// InvocationInput is the app-level invocation carrier for both lanes:
// a raw binding invocation (source+ref) or an operation-resolved one
// (Interface/Binding populated).
type InvocationInput struct {
	Source    InvokeSource            `json:"source"`
	Ref       string                  `json:"ref"`
	Input     any                     `json:"input,omitempty"`
	Context   map[string]any          `json:"context,omitempty"`
	Interface *openbindings.Interface `json:"interface,omitempty"`
	// Binding is the selected binding entry (the operation identity the
	// hook seam's InvokeSite derives from). Populated on the
	// operation-resolved paths; nil for raw binding invocations.
	// Process-local — never wire.
	Binding *openbindings.BindingEntry `json:"-"`
	// InputSchema is the operation's input schema. ALWAYS thread it with
	// Binding: a non-nil Binding with a nil InputSchema means "no-input
	// operation" to the usage run loop (the recorded discriminator).
	InputSchema openbindings.JSONSchema `json:"-"`
	// Hooks is the data face's per-invocation seam carrier (compiled from
	// `op invoke` flags), composed over ob's standing invoker-level table.
	// Nil = no per-invocation configuration; the invoker's own snapshot
	// (its site-guarded table) still applies. Process-local — never wire.
	Hooks *openbindings.InvokeHooks `json:"-"`
}

// InvocationResult is the app-level invocation result for both lanes.
type InvocationResult struct {
	Output     any    `json:"output,omitempty"`
	Status     int    `json:"status,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Error      *Error `json:"error,omitempty"`
	BindingKey string `json:"bindingKey,omitempty"`
}

// operationKeyForName resolves a caller-supplied operation name to its
// canonical key: the name itself when it is a key, else the key of the
// operation that carries it as an alias (key + aliases are one namespace,
// OBI-D-04, so at most one matches), else "".
func operationKeyForName(name string, iface *openbindings.Interface) string {
	if _, ok := iface.Operations[name]; ok {
		return name
	}
	for key, op := range iface.Operations {
		for _, a := range op.Aliases {
			if a == name {
				return key
			}
		}
	}
	return ""
}

// DefaultBindingForOp applies the operation-invoker contract's automatic
// resolution rule over bindings ob can act on: exactly one candidate resolves;
// zero or several return the SDK's distinct not-found or selection-required
// error. Preference, deprecation, source order, and key order never invent a
// choice.
func DefaultBindingForOp(opKey string, iface *openbindings.Interface) (string, *openbindings.BindingEntry, error) {
	return selectBindingForOp(opKey, iface, nil)
}

// bindingByKey looks up a binding by its key.
// Returns nil if the key does not exist.
func bindingByKey(bindingKey string, iface *openbindings.Interface) *openbindings.BindingEntry {
	if iface == nil {
		return nil
	}
	b, ok := iface.Bindings[bindingKey]
	if !ok {
		return nil
	}
	return &b
}

// withBinaryMetadata returns a context with the binary hint set under metadata.
// It clones the input context (and the metadata sub-map) so existing callers
// don't observe a mutation.
func withBinaryMetadata(ctx map[string]any, binary string) map[string]any {
	out := make(map[string]any, len(ctx)+1)
	for k, v := range ctx {
		out[k] = v
	}
	var meta map[string]any
	if existing, ok := out["metadata"].(map[string]any); ok {
		meta = make(map[string]any, len(existing)+1)
		for k, v := range existing {
			meta[k] = v
		}
	} else {
		meta = make(map[string]any, 1)
	}
	meta["binary"] = binary
	out["metadata"] = meta
	return out
}

// resolvedBinding holds the resolved components for an OBI operation invocation.
type resolvedBinding struct {
	bindingKey string
	binding    *openbindings.BindingEntry
	source     openbindings.Source
	input      any
}

// resolveBindingAndSource resolves a binding, source, and input transform
// from an OBI interface. Context resolution is handled by the invoker and
// per-format invokers via the ContextStore.
func resolveBindingAndSource(iface *openbindings.Interface, opKey, bindingKey string, input any) (*resolvedBinding, error) {
	return resolveBindingAndSourceWithContext(iface, opKey, bindingKey, input, nil)
}

// resolveBindingAndSourceWithContext is the operation-invoker resolution
// surface used by invocation and preflight. An explicit binding bypasses
// selection; otherwise context.configuration.selection is the caller's ordered
// choice and sole-candidate inference is the only automatic resolution.
func resolveBindingAndSourceWithContext(iface *openbindings.Interface, opKey, bindingKey string, input any, callerContext map[string]any) (*resolvedBinding, error) {
	if opKey != "" && bindingKey != "" {
		return nil, fmt.Errorf("operation key and binding key are mutually exclusive")
	}

	var binding *openbindings.BindingEntry
	var resolvedKey string
	if bindingKey != "" {
		binding = bindingByKey(bindingKey, iface)
		if binding == nil {
			return nil, fmt.Errorf("binding %q not found", bindingKey)
		}
		resolvedKey = bindingKey
		opKey = binding.Operation
		if _, ok := iface.Operations[opKey]; !ok {
			return nil, fmt.Errorf("operation %q (referenced by binding %q) not found", opKey, bindingKey)
		}
	} else {
		if _, ok := iface.Operations[opKey]; !ok {
			// An operation answers to its key AND any of its aliases equally
			// (core OBI-D-04 / OBI-T-12). The mutation commands honor this;
			// invoke resolves an alias to its canonical key so it does too.
			if canonical := operationKeyForName(opKey, iface); canonical != "" {
				opKey = canonical
			} else {
				return nil, fmt.Errorf("operation %q not found", opKey)
			}
		}
		var selectionErr error
		resolvedKey, binding, selectionErr = selectBindingForOp(opKey, iface, contextSelection(callerContext))
		if selectionErr != nil {
			return nil, selectionErr
		}
	}

	source, ok := iface.Sources[binding.Source]
	if !ok {
		return nil, fmt.Errorf("binding source %q not found", binding.Source)
	}

	execInput := input
	if binding.InputTransform != nil {
		transformed, tErr := ApplyTransform(iface.Transforms, binding.InputTransform, input)
		if tErr != nil {
			return nil, fmt.Errorf("input transform failed: %w", tErr)
		}
		execInput = transformed
	}

	return &resolvedBinding{
		bindingKey: resolvedKey,
		binding:    binding,
		source:     source,
		input:      execInput,
	}, nil
}

// contextSelection reads the operation-invoker contract's selection point.
// Malformed values provide no effective choice.
func contextSelection(ctx map[string]any) []string {
	configuration, _ := ctx["configuration"].(map[string]any)
	switch raw := configuration["selection"].(type) {
	case []string:
		return append([]string(nil), raw...)
	case []any:
		out := make([]string, len(raw))
		for index, value := range raw {
			s, ok := value.(string)
			if !ok {
				return nil
			}
			out[index] = s
		}
		return out
	default:
		return nil
	}
}

// selectBindingForOp applies an ordered caller choice, then the
// policy-neutral sole-invocable-candidate rule. "Invocable" is evaluated
// against ob's builtin and registered-delegate reach, not merely document
// presence.
func selectBindingForOp(opKey string, iface *openbindings.Interface, ordered []string) (string, *openbindings.BindingEntry, error) {
	if iface == nil {
		return "", nil, fmt.Errorf("%w: %s", openbindings.ErrBindingNotFound, opKey)
	}
	invocable := func(binding openbindings.BindingEntry) bool {
		source, ok := iface.Sources[binding.Source]
		if !ok {
			// Preserve the document error for the selected candidate. The
			// operation-invoker contract assumes a valid OBI; treating a
			// dangling source as "unsupported" would mask the real defect.
			return true
		}
		return BuiltinSupportsFormat(source.BindingSpec) || selectDelegate(CapInvoke, source.BindingSpec) != nil
	}

	for _, key := range ordered {
		binding, ok := iface.Bindings[key]
		if ok && binding.Operation == opKey && invocable(binding) {
			copy := binding
			return key, &copy, nil
		}
	}

	var selectedKey string
	var selected *openbindings.BindingEntry
	candidates := make([]string, 0, len(iface.Bindings))
	for key, binding := range iface.Bindings {
		if binding.Operation != opKey || !invocable(binding) {
			continue
		}
		candidates = append(candidates, key)
		copy := binding
		selectedKey, selected = key, &copy
	}
	sort.Strings(candidates)
	switch len(candidates) {
	case 0:
		return "", nil, fmt.Errorf("%w: %s", openbindings.ErrBindingNotFound, opKey)
	case 1:
		return selectedKey, selected, nil
	default:
		// Name the exact candidates, not merely how many. A refusal that makes
		// the caller re-derive the valid set is a refusal withholding its own
		// remedy, and the MCP bridge already lists them (details.bindings) —
		// the two surfaces must not disagree about the same artifact.
		// Resolution policy is unchanged: several candidates still refuse.
		return "", nil, fmt.Errorf("%w: operation %q has %d invocable bindings (%s); choose one with --binding or --select-binding",
			openbindings.ErrBindingSelectionRequired, opKey, len(candidates), strings.Join(candidates, ", "))
	}
}

// resolveSourceLocation builds the InvocationSource for a binding's source.
// exec: refs, URIs, absolute paths, and host:port addresses pass through
// unchanged; embedded content rides as-is. A RELATIVE file location is
// refused: the deleted courtesy lane silently resolved it against the OBI's
// directory, which made nonconformant documents (OBI-D-05) invoke fine in
// place and die with a bare file error everywhere else. The remedy is the
// D-05 ruling's local lane — embed the artifact — or an absolute URI.
func resolveSourceLocation(source openbindings.Source) (openbindings.InvocationSource, error) {
	es := openbindings.InvocationSource{BindingSpec: source.BindingSpec}
	if source.Location != "" {
		loc := source.Location
		if !execref.IsExec(loc) && !strings.Contains(loc, "://") && !filepath.IsAbs(loc) && !isHostPort(loc) {
			return es, fmt.Errorf(
				"source location %q is a relative reference — not conformant (OBI-D-05) and not portable; embed the artifact instead ('ob source add --resolve content', or synthesize with '?embed'), or set an absolute URI ('ob source add --uri')",
				loc)
		}
		es.Location = loc
	} else if source.Content != nil {
		es.Content = source.Content
	}
	return es, nil
}

// isHostPort returns true if s looks like a host:port network address.
func isHostPort(s string) bool {
	_, _, err := net.SplitHostPort(s)
	return err == nil
}

// InvocationOutput is the app-layer event shape: one output value, or a
// terminal error. The SDK's invocation handle yields bare output values and a
// separate terminal error; ob's internal consumers (serve, mcpbridge,
// operation, TUI) work over a channel of these, so this type bridges the two.
// Status is best-effort (derived from an HTTP error's Details when present);
// DurationMs is filled by the caller.
type InvocationOutput struct {
	Output     any                           `json:"output,omitempty"`
	Error      *openbindings.InvocationError `json:"error,omitempty"`
	Status     int                           `json:"status,omitempty"`
	DurationMs int64                         `json:"durationMs,omitempty"`
	// Terminal marks the final metadata-only event a clean stream emits:
	// nil Output and Error, carrying the invocation's trailing Metadata
	// (the provenance stamps the format-conventions record recommends —
	// x-ob-decode/-classify/-route — and exec's x-exit-code). Forwarders
	// pass it through untouched; output consumers skip it.
	Terminal bool                  `json:"-"`
	Metadata openbindings.Metadata `json:"-"`
}

// maxBindingContextRounds caps CONTEXT_REQUIRED resolve-and-retry rounds for
// the app-level binding paths, mirroring the SDK operation layer's cap.
const maxBindingContextRounds = 3

// deriveSourceTarget derives a source's AUTHORITATIVE target — the network host
// ob can determine from the source itself, independently of whatever target an
// invoker asserts. ob holds the source in both the builtin and delegate paths,
// so where the source names a concrete network endpoint ob can check a
// delegate's asserted CONTEXT_REQUIRED target against it without trusting the
// delegate's word (the binding-invoker contract's confused-deputy defense).
//
// It returns the host-normalized location (openbindings.NormalizeEndpoint — the
// same origin identity the context store keys on) when the location is a
// concrete network endpoint: an http(s)/ws(s) URL, or a bare host:port address
// (a gRPC-style location). It returns "" when no network host is readable
// without family knowledge — inline content (only a family processor can read
// the artifact's declared servers), an exec ref, or a relative/opaque location.
// An empty result means the assertion is UNVERIFIABLE: ob must then treat the
// invoker as untrusted for credential provisioning rather than trust its word.
func deriveSourceTarget(source InvokeSource) string {
	loc := strings.TrimSpace(source.Location)
	if loc == "" {
		return "" // inline content or no location
	}
	if execref.IsExec(loc) {
		return "" // exec ref: no network host
	}
	if isHostPort(loc) {
		return openbindings.NormalizeEndpoint(loc) // the location IS the endpoint
	}
	if u, err := url.Parse(loc); err == nil && u.Host != "" {
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ws", "wss":
			return openbindings.NormalizeEndpoint(loc)
		}
	}
	return "" // file path, relative ref, or opaque scheme: no derivable host
}

// sourceLabel names a source for a diagnostic message: its location, or
// "inline content" when the artifact rides in the document.
func sourceLabel(source InvokeSource) string {
	if loc := strings.TrimSpace(source.Location); loc != "" {
		return loc
	}
	return "inline content"
}

// delegateProvisionGuard hardens CONTEXT_REQUIRED credential provisioning when
// driveBinding drives an UNTRUSTED invoker — a delegate, which is a separate
// (possibly third-party) process. ob's own in-process builtin invoker passes no
// guard: it runs inside ob's trust boundary and derives the challenge's target
// authentically from the same source ob holds, so its assertion needs no
// independent check.
//
// Two runtime-enforced limits from the binding-invoker contract live here, and
// together they bound what a delegate can obtain:
//
//   - Target validation (confused-deputy defense). The `target` in a
//     CONTEXT_REQUIRED challenge is ASSERTED by the invoker. A misreporting
//     delegate could name one host's target to make ob look up and forward
//     ANOTHER host's stored credentials. Before any credential lookup, the
//     guard validates the asserted target against the source's authoritative
//     target and refuses a mismatch — no lookup, no merge.
//
//   - Least privilege. Every context ob provisions to the delegate is scoped to
//     the one challenge it answered (openbindings.ScopeContext), so a delegate
//     never receives more credential material than its own challenge named.
type delegateProvisionGuard struct {
	// authoritativeTarget is deriveSourceTarget(source): the source's target
	// host as ob derives it independently of the invoker. Empty when ob cannot
	// derive one without family knowledge, in which case a delegate's target is
	// unverifiable and no stored credentials are provisioned for it.
	authoritativeTarget string
	// sourceLabel describes the source in the refusal message.
	sourceLabel string
}

// newDelegateProvisionGuard builds the guard for a delegate invocation from the
// source ob is invoking through.
func newDelegateProvisionGuard(source InvokeSource) *delegateProvisionGuard {
	return &delegateProvisionGuard{
		authoritativeTarget: deriveSourceTarget(source),
		sourceLabel:         sourceLabel(source),
	}
}

// vetTarget decides whether ob may provision credentials for a delegate's
// asserted CONTEXT_REQUIRED target. It returns:
//
//   - provision=true, refusal=nil  — the asserted target matches the source's
//     authoritative target; ob may resolve, scope, and forward credentials.
//   - provision=false, refusal!=nil — the asserted target MISMATCHES the
//     source's authoritative target (the confused-deputy case): a loud terminal
//     refusal to surface, with no credential lookup and no merge.
//   - provision=false, refusal=nil  — ob could not derive an authoritative
//     target to verify against; it withholds stored-credential provisioning for
//     the unverifiable target and lets the delegate's own challenge surface, so
//     a caller can still supply per-call context explicitly.
func (g *delegateProvisionGuard) vetTarget(asserted string) (provision bool, refusal *openbindings.InvocationError) {
	if g.authoritativeTarget == "" {
		return false, nil
	}
	if openbindings.NormalizeEndpoint(asserted) != g.authoritativeTarget {
		return false, &openbindings.InvocationError{
			Code: openbindings.ErrCodePermissionDenied,
			Message: fmt.Sprintf(
				"refusing to provision credentials to delegate: it asserted context target %q, but the source ob is invoking (%s) authoritatively addresses %q — a misreporting invoker must not name one host's target to obtain another host's stored credentials (binding-invoker confused-deputy defense)",
				asserted, g.sourceLabel, g.authoritativeTarget),
		}
	}
	return true, nil
}

// driveBinding invokes a binding (via the supplied invoke function), writes
// the single input (when non-nil), closes the input side, and streams the
// handle's outputs (and any terminal error) onto a channel of app-layer
// InvocationOutput.
//
// ob's app layer drives the BINDING layer directly (it resolves the binding,
// source, and transforms itself), so it owns the CONTEXT_REQUIRED negotiation
// the SDK's operation layer would otherwise provide: a challenge raised before
// any output is resolved through the configured resolver and the binding is
// re-invoked with the merged context, replaying the input. Once the binding
// shows observable progress, challenges surface to the caller instead.
//
// Write errors are not reported here — the output read loop owns terminal
// reporting (matching the SDK's own pattern). The channel closes when the
// invocation ends.
//
// guard is nil for ob's own in-process builtin invoker (trusted: it derives the
// challenge target authentically from the same source ob holds). It is non-nil
// on the delegate path, where the invoker is untrusted: the guard validates the
// delegate-asserted CONTEXT_REQUIRED target against the source's authoritative
// target before any credential lookup (confused-deputy defense) and scopes every
// provisioned context to the challenge (least privilege).
func driveBinding(
	ctx context.Context,
	invoke func(context.Context, map[string]any) openbindings.Invocation[any, any],
	contextData map[string]any,
	input any,
	resolver openbindings.ContextResolver,
	guard *delegateProvisionGuard,
) <-chan InvocationOutput {
	ch := make(chan InvocationOutput, 16)
	go func() {
		defer close(ch)

		for round := 0; ; round++ {
			call := invoke(ctx, contextData)
			if input != nil {
				_ = call.Write(ctx, input)
			}
			_ = call.Close()

			out := call.Outputs()
			emitted := false
			for {
				v, err := out.Read(ctx)
				if errors.Is(err, io.EOF) {
					// Clean end: surface the invocation's trailer (the
					// provenance stamps, exec's x-exit-code) as a terminal
					// metadata marker so the data face's -F json envelope
					// can carry the verdict block.
					if md := call.Trailer(); len(md) > 0 {
						select {
						case ch <- InvocationOutput{Terminal: true, Metadata: md}:
						case <-ctx.Done():
						}
					}
					return
				}
				if err != nil {
					ie := openbindings.AsInvocationError(err)
					// Resolve-and-retry: only before any observable progress,
					// only with a resolver, and only a bounded number of times.
					if details := openbindings.ContextRequiredFrom(ie); details != nil &&
						!emitted && resolver != nil && round < maxBindingContextRounds {
						// Confused-deputy defense (delegate path only): before any
						// credential lookup, validate the invoker-asserted target
						// against the source's authoritative target. A mismatch is
						// a loud terminal refusal — no lookup, no merge.
						provision := true
						if guard != nil {
							var refusal *openbindings.InvocationError
							provision, refusal = guard.vetTarget(details.Target)
							if refusal != nil {
								select {
								case ch <- InvocationOutput{Error: refusal, Status: statusFromError(refusal)}:
								case <-ctx.Done():
								}
								return
							}
							// provision==false with no refusal: target unverifiable;
							// skip the lookup and let the challenge surface below.
						}
						if provision {
							resolved, rerr := resolver(ctx, details)
							if rerr == nil && len(resolved) > 0 {
								merged := make(map[string]any, len(contextData)+len(resolved))
								for k, val := range contextData {
									merged[k] = val
								}
								for k, val := range resolved {
									merged[k] = val
								}
								// Least privilege on the untrusted (delegate) path:
								// hand the delegate only the context its challenge
								// scoped, never the caller's full per-call profile.
								if guard != nil {
									merged = openbindings.ScopeContext(merged, details)
								}
								contextData = merged
								break // next round re-invokes with merged context
							}
						}
					}
					// Select on ctx so an abandoned consumer (e.g. a WS client
					// that disconnected) cannot strand this goroutine on a full,
					// unread channel. Cancelling ctx tears the binding down via
					// the SDK's terminal model; we exit rather than block.
					select {
					case ch <- InvocationOutput{Error: ie, Status: statusFromError(ie)}:
					case <-ctx.Done():
					}
					return
				}
				emitted = true
				select {
				case ch <- InvocationOutput{Output: v}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch
}

// statusFromError extracts an HTTP status from a terminal error's Details
// (HTTP-based format invokers carry {"status": N}); returns 0 when absent.
func statusFromError(err *openbindings.InvocationError) int {
	if err == nil {
		return 0
	}
	d, ok := err.Details.(map[string]any)
	if !ok {
		return 0
	}
	switch s := d["status"].(type) {
	case int:
		return s
	case float64:
		return int(s)
	}
	return 0
}

// InvokeOBIOperation invokes an operation from an OBI file and returns a
// stream of events. Every operation is a stream — unary calls produce one
// event. Input/output transforms are applied as declared in the binding entry.
//
// If the resolved format has a builtin streaming invoker, it is used.
// Otherwise, the unary invocation path is used and its result is wrapped as
// a single InvocationOutput.
//
// Exactly one of opKey or bindingKey must be non-empty:
//   - opKey: resolves the sole invocable binding for that operation and
//     returns ERR_BINDING_SELECTION_REQUIRED when several remain.
//   - bindingKey: looks up the binding directly (operation is read from the entry).
//
// The string result is the key of the binding the invocation resolved to —
// the selection outcome a caller can surface (e.g. `ob op invoke -v`).
func InvokeOBIOperation(ctx context.Context, obiPath string, opKey string, bindingKey string, input any) (<-chan InvocationOutput, string, error) {
	run, err := InvokeOBIOperationConfigured(ctx, obiPath, opKey, bindingKey, input, nil)
	if err != nil {
		return nil, "", err
	}
	// The terminal metadata marker is a data-face affordance
	// (InvokeOBIOperationConfigured + renderInvokeJSON); legacy callers of
	// this back-compat entry see only outputs and errors.
	return withoutTerminalMarkers(run.Events), run.BindingKey, nil
}

// withoutTerminalMarkers strips the terminal metadata marker from an event
// stream so consumers that treat every event as an output (or an error)
// never observe a nil-output marker.
func withoutTerminalMarkers(src <-chan InvocationOutput) <-chan InvocationOutput {
	out := make(chan InvocationOutput)
	go func() {
		defer close(out)
		for ev := range src {
			if ev.Terminal {
				continue
			}
			out <- ev
		}
	}()
	return out
}

// ConfiguredInvocation is the data face's invocation result: the event
// stream, the resolved binding key, and — when a winning EXTERNAL delegate
// displaces ob's standing internal-table elections (the table published as
// docs/bound-cli-recipe.md) — the loud attributed displacement warning and
// its verbose detail (which elections).
type ConfiguredInvocation struct {
	Events           <-chan InvocationOutput
	BindingKey       string
	DisplacedWarning string
	DisplacedDetail  []string
}

// InvokeOBIOperationConfigured is the data-face entry: it invokes an
// operation with an optional per-invocation InvokeConfig (--decode/
// --ok-exit/--route), computing delegate selection PRE-DISPATCH so the
// displacement split (flags refuse, standing elections warn) is decidable
// before anything runs.
func InvokeOBIOperationConfigured(ctx context.Context, obiPath, opKey, bindingKey string, input any, config *InvokeConfig) (*ConfiguredInvocation, error) {
	iface, err := resolveInterface(obiPath)
	if err != nil {
		return nil, fmt.Errorf("load OBI %q: %w", obiPath, err)
	}
	return invokeOnInterface(ctx, iface, opKey, bindingKey, input, config)
}

// invokeOnInterface invokes an operation (or a specific binding) on an
// already-resolved interface, resolving relative source locations against
// obiDir. It is the core shared by file-backed invocation (InvokeOBIOperation)
// and delegate invocation: ob operation-invokes a delegate's operation against
// the delegate's own resolved OBI through this same path.
//
// Dispatch is UNIFIED under delegate selection: the winner is
// computed before anything runs. When the self-delegate wins, the config's
// per-invocation hooks are compiled and threaded, and the streaming lane is
// available. When an external delegate wins the hop, displaced FLAGS refuse
// loudly (explicit intent that cannot cross the boundary) and displaced
// STANDING elections proceed with a loud attributed warning. Every emitted
// output is T-08-validated against the operation's declared output schema
// before it reaches the caller (stop-and-return on nonconformant emission).
func invokeOnInterface(ctx context.Context, iface *openbindings.Interface, opKey, bindingKey string, input any, config *InvokeConfig) (*ConfiguredInvocation, error) {
	callerContext := config.context()
	resolved, err := resolveBindingAndSourceWithContext(iface, opKey, bindingKey, input, callerContext)
	if err != nil {
		return nil, err
	}

	es, err := resolveSourceLocation(resolved.source)
	if err != nil {
		return nil, err
	}
	opCanonical := resolved.binding.Operation
	outputSchema := iface.Operations[opCanonical].Output

	// OBI-T-07 on the app-driven path: ob drives the binding layer directly
	// (bypassing the SDK operation layer's per-message validation), so the
	// caller's message is validated against the operation's input schema
	// BEFORE any dispatch. Schema-violating input must never reach the wire —
	// by the time output validation fails, the side effect has happened.
	// The check runs on the caller's message, pre-transform: the operation
	// schema describes the caller's shape, the transform's result is the
	// binding's business.
	if inSchema := iface.Operations[opCanonical].Input; inSchema != nil && input != nil {
		if verr := openbindings.ValidateAgainstSchema(input, inSchema, iface.Schemas); verr != nil {
			return nil, fmt.Errorf("input validation failed for %q: %w", resolved.bindingKey, verr)
		}
	}

	lowLevel := InvocationInput{
		Source:      InvokeSource{BindingSpec: es.BindingSpec, Location: es.Location, Content: es.Content},
		Ref:         resolved.binding.Ref,
		Input:       resolved.input,
		Context:     callerContext,
		Interface:   iface,
		Binding:     resolved.binding,
		InputSchema: effectiveInputSchema(iface, resolved.binding, resolved.input),
	}

	run := &ConfiguredInvocation{BindingKey: resolved.bindingKey}

	// Pre-dispatch delegate selection (deterministic): the split is decided
	// before any side effect.
	chosen := selectDelegate(CapInvoke, es.BindingSpec)
	if chosen != nil && !chosen.builtin {
		// An external delegate owns the binding hop. Displaced FLAGS cannot
		// apply across the boundary — refuse the explicit intent loudly.
		if !config.empty() {
			return nil, fmt.Errorf("op invoke: --decode/--ok-exit/--route configure ob's built-in handling, which delegate %q displaces for format %q (the delegate dispatches the binding itself); unset them, or run in-process with `ob delegate prefer ob --operation %s`",
				chosen.name(), es.BindingSpec, opCanonical)
		}
		// Displaced STANDING elections proceed with a loud attributed warning.
		run.DisplacedWarning, run.DisplacedDetail = displacedElectionsWarning(opCanonical, chosen.name())

		delegateIface, rerr := chosen.resolveInterface()
		if rerr != nil {
			return nil, fmt.Errorf("resolve delegate %q: %w", chosen.name(), rerr)
		}
		out := invokeViaExternalDelegate(ctx, delegates.Resolved{
			Format:   es.BindingSpec,
			Delegate: chosen.name(),
			Location: chosen.location(),
			OBI:      &delegates.ResolvedOBI{Interface: *delegateIface},
		}, lowLevel)
		run.Events = applyT08(unaryChannel(iface, resolved, out), outputSchema, iface.Schemas, resolved.bindingKey)
		return run, nil
	}

	// Self-delegate / builtin: compile the config's per-invocation hooks
	// (composed over ob's standing table) and thread them.
	lowLevel.Hooks = config.perInvocationHooks(DefaultInvoker())

	// Streaming lane (builtin drivers only) when the self-delegate wins.
	if BuiltinSupportsFormat(es.BindingSpec) {
		src, sErr := SubscribeOperationWithContext(ctx, lowLevel)
		if sErr == nil {
			run.Events = applyT08(transformEventStream(src, iface, resolved), outputSchema, iface.Schemas, resolved.bindingKey)
			return run, nil
		}
	}

	// Unary fallback (in-process builtin, or a self-delegate non-streaming path).
	out := InvokeOperationWithContext(ctx, lowLevel)
	out.BindingKey = resolved.bindingKey
	run.Events = applyT08(unaryChannel(iface, resolved, out), outputSchema, iface.Schemas, resolved.bindingKey)
	return run, nil
}

// unaryChannel collapses a unary InvocationResult into a one-event
// channel, applying the binding's output transform on success (the
// streaming lane applies it via transformEventStream).
func unaryChannel(iface *openbindings.Interface, resolved *resolvedBinding, result InvocationResult) <-chan InvocationOutput {
	if resolved.binding.OutputTransform != nil && result.Error == nil {
		transformed, tErr := ApplyTransform(iface.Transforms, resolved.binding.OutputTransform, result.Output)
		if tErr != nil {
			result.Error = &Error{Code: "output_transform_error", Message: fmt.Sprintf("output transform failed: %v", tErr)}
		} else {
			result.Output = transformed
		}
	}
	ch := make(chan InvocationOutput, 1)
	if result.Error != nil {
		ch <- InvocationOutput{Error: &openbindings.InvocationError{Code: result.Error.Code, Message: result.Error.Message}, Status: result.Status}
	} else {
		ch <- InvocationOutput{Output: result.Output, Status: result.Status}
	}
	close(ch)
	return ch
}

// applyT08 is ob's adoption of OBI-T-08 on the app-driven binding path
// (which bypasses the SDK operation layer's own validation): every output
// is validated against the operation's declared output schema BEFORE it
// reaches the caller. A nonconformant output is not emitted — the stream
// terminates with the validation error (stop-and-return), carrying
// the contract-decided election teaching when the schema is floor-stamped
// (the derived contract still declares the floor; the remedy is the schema
// election, not the decode).
func applyT08(src <-chan InvocationOutput, schema openbindings.JSONSchema, schemas map[string]openbindings.JSONSchema, bindingKey string) <-chan InvocationOutput {
	out := make(chan InvocationOutput)
	go func() {
		defer close(out)
		for ev := range src {
			if ev.Terminal {
				// Assumption warning (the format-conventions record
				// recommends warning when an assumption lane decoded into
				// a contract): ob drives the binding layer (the SDK
				// operation layer's warning point is bypassed), so the
				// warning is appended here — keyed on the format's own
				// decode stamp, riding the terminal metadata into the
				// envelope. Only an assumption lane can trigger it.
				stamp := ""
				if v := ev.Metadata["x-ob-decode"]; len(v) > 0 {
					stamp = v[0]
				}
				if w := openbindings.AssumptionWarning(stamp, schema); w != "" {
					md := make(openbindings.Metadata, len(ev.Metadata)+1)
					for k, v := range ev.Metadata {
						md[k] = v
					}
					md["x-ob-warning"] = append(md["x-ob-warning"], w)
					ev.Metadata = md
				}
				out <- ev
				continue
			}
			if ev.Error != nil {
				out <- ev
				continue
			}
			if schema == nil {
				out <- ev
				continue
			}
			if verr := openbindings.ValidateAgainstSchema(ev.Output, schema, schemas); verr != nil {
				out <- InvocationOutput{Error: t08Failure(ev, verr, schema, bindingKey)}
				return
			}
			out <- ev
		}
	}()
	return out
}

// t08Failure builds the stop-and-return terminal for a nonconformant
// output. The offending payload (truncated) and the decode lane ride the
// error — the failure moment is exactly when the user needs to see what
// the service actually returned, and without a window here the only
// recourse is abandoning ob for curl (which gRPC/Connect/MCP bindings do
// not have).
func t08Failure(ev InvocationOutput, verr error, schema openbindings.JSONSchema, bindingKey string) *openbindings.InvocationError {
	msg := fmt.Sprintf("output validation failed for %q: %v", bindingKey, verr)
	if openbindings.FloorStamped(schema) {
		msg += " — the synthesized schema still declares the floor's string; elect the real output schema (`ob operation output-schema`)"
	}
	details := map[string]any{}
	if stamp := firstMetaValue(ev.Metadata, "x-ob-decode"); stamp != "" {
		details["decodedBy"] = stamp
	}
	if ct := firstMetaValue(ev.Metadata, "Content-Type"); ct != "" {
		details["contentType"] = ct
	}
	if snippet := payloadSnippet(ev.Output); snippet != "" {
		details["received"] = snippet
		msg += "\nreceived: " + snippet
	}
	// The wire lane sits below the operation boundary where T-08 attaches:
	// point at it, with the exact binding already selected, so a drifted
	// service stays READABLE while it stays nonconformant.
	msg += fmt.Sprintf("\nto see what the service actually returned: ob binding invoke <obi> %s", bindingKey)
	ie := &openbindings.InvocationError{Code: openbindings.ErrCodeValidationFailed, Message: msg}
	if len(details) > 0 {
		ie.Details = details
	}
	return ie
}

func firstMetaValue(md openbindings.Metadata, key string) string {
	if vs := md[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// payloadSnippet renders an output value for diagnostics, truncated so a
// large payload cannot flood the terminal.
func payloadSnippet(v any) string {
	if v == nil {
		return "null"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%.512v", v)
	}
	const maxLen = 2048
	if len(data) > maxLen {
		return fmt.Sprintf("%s… (%d bytes total)", data[:maxLen], len(data))
	}
	return string(data)
}

// PrepareOperation is the operation-level preflight: it resolves an operation
// (or a specific binding) on an interface to a concrete binding, then reports
// the context that binding would require before invocation, without invoking it
// or causing side effects. It is the by-reference counterpart to PrepareBinding.
// Returns nil when requirements cannot be determined without invoking (the
// always-satisfiable answer). Context resolution is not performed here; any
// supplied callerContext narrows the reported requirements to what is still
// unsatisfied, exactly as for PrepareBinding.
func PrepareOperation(ctx context.Context, obiPath string, opKey string, bindingKey string, callerContext map[string]any) (*openbindings.ContextRequiredDetails, error) {
	iface, err := resolveInterface(obiPath)
	if err != nil {
		return nil, fmt.Errorf("load OBI %q: %w", obiPath, err)
	}
	return PrepareInterfaceOperation(ctx, iface, opKey, bindingKey, callerContext)
}

// PrepareInterfaceOperation is PrepareOperation's document-valued form. It is
// used by remote APIs, where the interface is request data rather than a local
// path, while preserving the CLI path's acquisition and preflight semantics.
func PrepareInterfaceOperation(ctx context.Context, iface *openbindings.Interface, opKey string, bindingKey string, callerContext map[string]any) (*openbindings.ContextRequiredDetails, error) {
	if iface == nil {
		return nil, fmt.Errorf("interface is required")
	}
	resolved, err := resolveBindingAndSourceWithContext(iface, opKey, bindingKey, nil, callerContext)
	if err != nil {
		return nil, err
	}

	es, err := resolveSourceLocation(resolved.source)
	if err != nil {
		return nil, err
	}

	// The binding-layer preflight performs no I/O (its contract), so a
	// location-only source would answer "unknown" from a cold cache even
	// when auth is statically declared in the document. Document
	// ACQUISITION is the CLI's job: read or fetch the source exactly as
	// invoke would (read-only, side-effect-free) and ask the question
	// against the materialized content.
	if es.Content == nil && es.Location != "" {
		if data := acquireSourceDocument(ctx, es.Location); data != nil {
			// The acquired document is artifact TEXT; text rides the content
			// member as a JSON string (the carrier every family decodes).
			es.Content = openbindings.TextContent(string(data))
		}
	}

	return PrepareBinding(ctx, InvocationInput{
		Source:      InvokeSource{BindingSpec: es.BindingSpec, Location: es.Location, Content: es.Content},
		Ref:         resolved.binding.Ref,
		Context:     callerContext,
		Interface:   iface,
		Binding:     resolved.binding,
		InputSchema: effectiveInputSchema(iface, resolved.binding, resolved.input),
	})
}

// acquireSourceDocument materializes a source artifact for the preflight:
// local files are read, http(s) locations fetched (size-capped). Locations
// that are not documents (exec: refs, host:port service addresses) and any
// failure return nil — the preflight then answers from what it has, which
// is the pre-acquisition behavior.
func acquireSourceDocument(ctx context.Context, location string) []byte {
	const maxDocBytes = 1 << 20
	switch {
	case execref.IsExec(location) || isHostPort(location):
		return nil
	case strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://"):
		// SSRF guard: this source location may come from an untrusted OBI;
		// same outbound policy as /resolve and fetchSourceContent. On refusal
		// the preflight answers from what it has (its existing nil behavior).
		if err := ValidateOutboundURL(location); err != nil {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return nil
		}
		resp, err := GuardedHTTPClient(0).Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxDocBytes+1))
		if err != nil || len(data) > maxDocBytes {
			return nil
		}
		return data
	case strings.Contains(location, "://"):
		return nil
	default:
		data, err := os.ReadFile(location)
		if err != nil {
			return nil
		}
		return data
	}
}

// RenderContextRequirements renders prepareOperation/prepareBinding details
// for humans. Nil means no requirements could be determined without invoking
// (the always-conformant answer). The wire shape is the details themselves
// (or null) per the contract's oneOf — no envelope.
func RenderContextRequirements(details *openbindings.ContextRequiredDetails) string {
	s := Styles
	if details == nil {
		return s.Dim.Render("No context requirements (none determinable without invoking)")
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Context required"))
	sb.WriteString("\n  ")
	sb.WriteString(s.Dim.Render("target: "))
	sb.WriteString(s.Key.Render(details.Target))
	for i, alt := range details.Alternatives {
		sb.WriteString("\n\n  ")
		sb.WriteString(s.Dim.Render(fmt.Sprintf("alternative %d (all required):", i+1)))
		for _, req := range alt.Requirements {
			sb.WriteString("\n    - ")
			sb.WriteString(s.Key.Render(req.Type))
			if req.Description != "" {
				sb.WriteString(s.Dim.Render(" — " + req.Description))
			}
		}
	}
	return sb.String()
}

// transformEventStream applies the binding's outputTransform to each event.
// Returns the source channel directly if no transform is configured.
func transformEventStream(src <-chan InvocationOutput, iface *openbindings.Interface, resolved *resolvedBinding) <-chan InvocationOutput {
	if resolved.binding.OutputTransform == nil {
		return src
	}
	out := make(chan InvocationOutput)
	go func() {
		defer close(out)
		for ev := range src {
			if ev.Terminal || ev.Error != nil || ev.Output == nil {
				out <- ev
				continue
			}
			transformed, err := ApplyTransform(iface.Transforms, resolved.binding.OutputTransform, ev.Output)
			if err != nil {
				out <- InvocationOutput{Error: &openbindings.InvocationError{
					Code:    "output_transform_error",
					Message: fmt.Sprintf("output transform failed: %v", err),
				}}
				continue
			}
			out <- InvocationOutput{Output: transformed}
		}
	}()
	return out
}

// SubscribeOBIOperationDirect opens a streaming subscription using
// pre-resolved binding components. Used by the TUI which already has the
// interface, binding, and source loaded.
func SubscribeOBIOperationDirect(ctx context.Context, binding *openbindings.BindingEntry, source openbindings.Source) (<-chan InvocationOutput, error) {
	es, err := resolveSourceLocation(source)
	if err != nil {
		return nil, err
	}
	invoker := DefaultInvoker()
	invoke := func(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
		return invoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source:  es,
			Ref:     binding.Ref,
			Context: ctxData,
		})
	}
	// Builtin in-process invoker: trusted (no guard).
	return driveBinding(ctx, invoke, nil, nil, invoker.ContextResolver, nil), nil
}

var (
	selfPath     string
	selfPathOnce sync.Once
)

// isSelf checks if a delegate location refers to the current binary.
// Used for recursion prevention and in-process optimization.
func isSelf(location string) bool {
	if !execref.IsExec(location) {
		return false
	}
	cmd, err := execref.RootCommand(location)
	if err != nil {
		return false
	}

	selfPathOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		selfPath, _ = filepath.EvalSymlinks(exe)
	})
	if selfPath == "" {
		return false
	}

	resolved, err := exec.LookPath(cmd)
	if err != nil {
		return false
	}
	resolved, _ = filepath.EvalSymlinks(resolved)
	return resolved == selfPath
}

// InvokeOperationWithContext invokes an operation with cancellation support.
// Pass a cancellable context to allow aborting long-running operations.
func InvokeOperationWithContext(ctx context.Context, input InvocationInput) InvocationResult {
	start := time.Now()

	// Validate input
	if input.Source.BindingSpec == "" {
		return InvocationResult{
			Error: &Error{
				Code:    "invalid_input",
				Message: "source.bindingSpec is required",
			},
		}
	}
	if input.Ref == "" {
		return InvocationResult{
			Error: &Error{
				Code:    "invalid_input",
				Message: "ref is required",
			},
		}
	}

	// Unified delegate selection (capability + format, preference, self-first
	// ties). Native formats select the self-delegate (iface nil → in-process);
	// non-native formats select an external delegate when one is registered.
	chosen := selectDelegate(CapInvoke, input.Source.BindingSpec)

	var output InvocationResult
	if chosen == nil || chosen.builtin {
		// Self-delegate or nothing: invoke in-process when ob supports the
		// format natively, else there is nowhere to route.
		if BuiltinSupportsFormat(input.Source.BindingSpec) {
			output = invokeViaBuiltin(ctx, input)
		} else {
			return InvocationResult{
				Error: &Error{
					Code:    "delegate_resolution_failed",
					Message: fmt.Sprintf("no invoker or delegate handles format %q", input.Source.BindingSpec),
				},
			}
		}
	} else {
		// Resolve the chosen delegate's interface at use, verified against its
		// registration pin (match and invoke the same document).
		iface, rerr := chosen.resolveInterface()
		if rerr != nil {
			return InvocationResult{
				Error: &Error{Code: "delegate_resolution_failed", Message: rerr.Error()},
			}
		}
		output = invokeViaExternalDelegate(ctx, delegates.Resolved{
			Format:   input.Source.BindingSpec,
			Delegate: chosen.name(),
			Location: chosen.location(),
			OBI:      &delegates.ResolvedOBI{Interface: *iface},
		}, input)
	}

	output.DurationMs = time.Since(start).Milliseconds()
	return output
}

// SubscribeOperationWithContext opens a streaming subscription with cancellation
// support. Mirrors InvokeOperationWithContext but returns a channel of events
// instead of a single output. External delegates are not supported (streaming
// across process boundaries requires a transport protocol; use builtin drivers).
func SubscribeOperationWithContext(ctx context.Context, input InvocationInput) (<-chan InvocationOutput, error) {
	if input.Source.BindingSpec == "" {
		return nil, fmt.Errorf("source.bindingSpec is required")
	}
	if input.Ref == "" {
		return nil, fmt.Errorf("ref is required")
	}

	if !BuiltinSupportsFormat(input.Source.BindingSpec) {
		return nil, fmt.Errorf("streaming not supported for format %q (no builtin invoker)", input.Source.BindingSpec)
	}

	invoker := DefaultInvoker()
	invoke := func(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
		return invoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source: openbindings.InvocationSource{
				BindingSpec: input.Source.BindingSpec,
				Location:    input.Source.Location,
				Content:     input.Source.Content,
			},
			Ref:         input.Ref,
			Context:     ctxData,
			Interface:   input.Interface,
			Binding:     input.Binding,
			InputSchema: input.InputSchema,
			Hooks:       input.Hooks,
		})
	}
	// Builtin in-process invoker: trusted (no guard).
	return driveBinding(ctx, invoke, input.Context, input.Input, invoker.ContextResolver, nil), nil
}

// invokeViaBuiltin invokes an operation using the built-in OperationInvoker.
func invokeViaBuiltin(ctx context.Context, input InvocationInput) InvocationResult {
	bindCtx := input.Context
	if input.Source.Binary != "" {
		bindCtx = withBinaryMetadata(bindCtx, input.Source.Binary)
	}

	invoker := DefaultInvoker()
	invoke := func(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
		return invoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source: openbindings.InvocationSource{
				BindingSpec: input.Source.BindingSpec,
				Location:    input.Source.Location,
				Content:     input.Source.Content,
			},
			Ref:         input.Ref,
			Context:     ctxData,
			Interface:   input.Interface,
			Binding:     input.Binding,
			InputSchema: input.InputSchema,
			Hooks:       input.Hooks,
		})
	}

	// Builtin in-process invoker: trusted (no guard).
	return reduceUnaryInvocation(driveBinding(ctx, invoke, bindCtx, input.Input, invoker.ContextResolver, nil))
}

// reduceUnaryInvocation collapses an invocation event stream to the unary
// output shape: the last output wins; a terminal error takes precedence.
func reduceUnaryInvocation(events <-chan InvocationOutput) InvocationResult {
	var last *InvocationOutput
	for ev := range events {
		ev := ev
		if ev.Terminal {
			continue // metadata-only marker; not an output
		}
		last = &ev
	}
	if last == nil {
		return InvocationResult{}
	}
	if last.Error != nil {
		status := last.Status
		if status == 0 {
			status = 1
		}
		return InvocationResult{
			Status:     status,
			DurationMs: last.DurationMs,
			Error:      last.Error,
		}
	}
	return InvocationResult{
		Output:     last.Output,
		Status:     last.Status,
		DurationMs: last.DurationMs,
	}
}

// invokeViaExternalDelegate invokes an operation via an external delegate's
// invokeBinding capability (the binding-invoker frame protocol over WebSocket,
// or the delegate's CLI realization; see DelegateBindingInvoker), driving the
// unary shape through the invocation handle. CONTEXT_REQUIRED challenges from
// the delegate (or the downstream binding behind it) resolve through the
// configured resolver, exactly as for in-process invokers.
func invokeViaExternalDelegate(ctx context.Context, resolved delegates.Resolved, input InvocationInput) InvocationResult {
	delegateInvoker, err := DelegateBindingInvoker(resolved)
	if err != nil {
		return InvocationResult{
			Error: &Error{Code: "delegate_error", Message: err.Error()},
		}
	}

	invoke := func(ctx context.Context, ctxData map[string]any) openbindings.Invocation[any, any] {
		return delegateInvoker.InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source: openbindings.InvocationSource{
				BindingSpec: input.Source.BindingSpec,
				Location:    input.Source.Location,
				Content:     input.Source.Content,
			},
			Ref:     input.Ref,
			Context: ctxData,
		})
	}
	// Delegate path: the invoker is untrusted. Guard credential provisioning
	// against the source's authoritative target (confused-deputy defense) and
	// scope every provisioned context to the challenge (least privilege).
	guard := newDelegateProvisionGuard(input.Source)
	return reduceUnaryInvocation(driveBinding(ctx, invoke, input.Context, input.Input, DefaultInvoker().ContextResolver, guard))
}

// effectiveInputSchema is the no-input-convention discriminator's honest
// input: the operation's declared schema — or, when the operation declares
// none but the BINDING's input transform injected a value (ob's -F json
// forcing transforms), a permissive schema, so the transport reads the
// injected input instead of running the bare command (Binding non-nil +
// InputSchema nil means "no-input operation" to the usage run loop).
func effectiveInputSchema(iface *openbindings.Interface, binding *openbindings.BindingEntry, transformedInput any) openbindings.JSONSchema {
	if schema := iface.Operations[binding.Operation].Input; schema != nil {
		return schema
	}
	if transformedInput != nil {
		return map[string]any{}
	}
	return nil
}
