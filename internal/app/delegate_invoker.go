package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/coder/websocket"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
	"github.com/openbindings/ob/internal/frames"
	"github.com/openbindings/openbindings-go/formats/asyncapi"
	"github.com/openbindings/openbindings-go/formats/usage"
)

// Delegate-backed binding invocation. A resolved delegate's OBI declares how
// its invokeBinding operation is reachable; ob exposes that as a normal
// openbindings.BindingInvoker:
//
//   - asyncapi source with an http(s) location: the binding-invoker frame
//     protocol over WebSocket (internal/frames). Full Invocation-handle
//     semantics — every cardinality crosses the delegate boundary.
//   - usage source: the delegate's CLI realization (`<delegate> binding
//     invoke`). Unary: one input, one output.
//
// The frame transport is preferred when both are advertised and reachable.

// frameDocFetchTimeout bounds the fetch of a delegate's AsyncAPI document
// during frame-endpoint resolution.
const frameDocFetchTimeout = 10 * time.Second

// DelegateBindingInvoker returns a BindingInvoker that routes invocations to
// the resolved delegate via its advertised invokeBinding binding.
func DelegateBindingInvoker(resolved delegates.Resolved) (openbindings.BindingInvoker, error) {
	if resolved.OBI == nil {
		return nil, fmt.Errorf("delegate %q has no OBI", resolved.Delegate)
	}
	iface := &resolved.OBI.Interface

	// Resolve the delegate's own key for the invoke operation by key or alias
	// (OBI-T-12): a delegate may name it bare ("invokeBinding"), under its own
	// namespace with the published alias (ob's bound OBI:
	// "openbindings.ob.invokeBinding"), or any key aliased to the
	// binding-invoker interface.
	invokeKey, ok := delegateOpKey(iface, invokeOpNames...)
	if !ok {
		return nil, fmt.Errorf("delegate %q does not carry an invokeBinding operation (by key or alias)", resolved.Delegate)
	}

	var frameBinding, cliBinding *openbindings.BindingEntry
	for _, key := range sortedBindingKeys(iface) {
		b := iface.Bindings[key]
		if b.Operation != invokeKey {
			continue
		}
		source, ok := iface.Sources[b.Source]
		if !ok {
			continue
		}
		switch {
		case (strings.HasPrefix(source.BindingSpec, "openbindings.asyncapi") || strings.HasPrefix(source.BindingSpec, "asyncapi")) && delegates.IsHTTPURL(source.Location):
			if frameBinding == nil {
				bc := b
				frameBinding = &bc
			}
		case source.BindingSpec == usage.BindingSpec || strings.HasPrefix(source.BindingSpec, "usage@") || source.BindingSpec == "usage":
			if cliBinding == nil {
				bc := b
				cliBinding = &bc
			}
		case strings.HasPrefix(source.BindingSpec, "openbindings.usage"):
			// Wrapper-era registration (the retired openbindings.usage@0.x
			// WRAPPER format — distinct from the published openbindings.usage@1
			// bare-artifact spec matched exactly above): loud migration, never
			// silent non-matching — the delegate must be re-registered so its
			// pinned OBI carries the source the current dispatch speaks.
			return nil, fmt.Errorf("delegate %q was registered under the retired openbindings.usage wrapper format; re-register it (`ob delegate register %s`) to refresh its pinned interface", resolved.Delegate, resolved.Location)
		}
	}

	switch {
	case frameBinding != nil:
		return &delegateFrameInvoker{
			delegate: resolved.Delegate,
			format:   resolved.Format,
			docURL:   iface.Sources[frameBinding.Source].Location,
			ref:      frameBinding.Ref,
		}, nil
	case cliBinding != nil:
		return &delegateCLIInvoker{
			delegate: resolved.Delegate,
			format:   resolved.Format,
			iface:    iface,
			binding:  cliBinding,
			source:   iface.Sources[cliBinding.Source],
		}, nil
	default:
		return nil, fmt.Errorf("delegate %q advertises no usable invokeBinding binding (need an asyncapi source with an http(s) location, or a usage source)", resolved.Delegate)
	}
}

// sortedBindingKeys returns the interface's binding keys in stable order so
// binding selection is deterministic across runs.
func sortedBindingKeys(iface *openbindings.Interface) []string {
	keys := make([]string, 0, len(iface.Bindings))
	for k := range iface.Bindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Frame transport (WebSocket)
// ---------------------------------------------------------------------------

// delegateFrameInvoker speaks the binding-invoker frame protocol to a remote
// delegate. Endpoint resolution and the dial happen on the invocation's
// goroutine (creation stays inert); one connection carries one invocation.
type delegateFrameInvoker struct {
	delegate string
	format   string
	docURL   string // the delegate's AsyncAPI document
	ref      string // e.g. #/operations/invokeBinding
}

func (d *delegateFrameInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: d.format}}
}

func (d *delegateFrameInvoker) InvokeBinding(ctx context.Context, args *openbindings.BindingInvocationArgs) openbindings.Invocation[any, any] {
	input := &frames.BindingInvocationInput{
		Source: frames.InvokeSource{
			BindingSpec: args.Source.BindingSpec,
			Location:    args.Source.Location,
			Content:     args.Source.Content,
		},
		Ref: args.Ref,
		// Caller's per-call context only. ob does not pre-load the store for
		// delegate formats (PrepareBinding returns nil for them), so a delegate,
		// like any interface client, raises CONTEXT_REQUIRED for what it needs and
		// ob's resolver answers scoped (ScopeContext).
		Context: args.Context,
	}
	return frames.Invoke(ctx, d.dial, input)
}

// dial resolves the delegate's frame endpoint from its AsyncAPI document and
// opens the WebSocket. Transport auth to the delegate itself (e.g. an `ob
// serve` session token) is read from the context store under the delegate
// host's key and presented as a bearer on the upgrade request — it never
// mixes with the downstream context carried by the open frame.
func (d *delegateFrameInvoker) dial(ctx context.Context) (*websocket.Conn, *openbindings.InvocationError) {
	endpoint, err := resolveFrameEndpoint(ctx, d.docURL, d.ref)
	if err != nil {
		return nil, &openbindings.InvocationError{
			Code:    openbindings.ErrCodeSourceConfigError,
			Message: fmt.Sprintf("delegate %q: %v", d.delegate, err),
		}
	}

	header := http.Header{}
	store := NewCLIContextStore()
	if stored, _ := store.Get(ctx, openbindings.NormalizeEndpoint(endpoint)); stored != nil {
		if token := openbindings.ContextBearerToken(stored); token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
	}

	conn, resp, dialErr := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: header})
	if dialErr != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			host := openbindings.NormalizeEndpoint(endpoint)
			return nil, &openbindings.InvocationError{
				Code:    openbindings.ErrCodeAuthRequired,
				Message: fmt.Sprintf("delegate %q rejected the stored credentials (HTTP %d); store its token with `ob context set %s`", d.delegate, resp.StatusCode, host),
			}
		}
		return nil, &openbindings.InvocationError{
			Code:    openbindings.ErrCodeConnectFailed,
			Message: fmt.Sprintf("delegate %q: dialing %s: %v", d.delegate, endpoint, dialErr),
		}
	}
	conn.SetReadLimit(maxFrameBytes)
	return conn, nil
}

// maxFrameBytes bounds a single frame read from a delegate, mirroring the
// serve side's request-body cap.
const maxFrameBytes = 2 << 20 // 2 MiB

// resolveFrameEndpoint fetches the delegate's AsyncAPI document and derives
// the ws(s) URL of the operation the ref names. Everything AsyncAPI —
// document parsing, the ref grammar (ASYNC-D-03), and the pinned
// server-selection and address rules (openbindings.asyncapi@1 §9.2,
// ASYNC-P-04) — lives behind the SDK's format seam
// (asyncapi.ParseDocument / Document.ResolveEndpoint), never re-derived
// here. What stays on this side is ob's own: fetching the document (the
// frame lane's timeout and size policy) and spelling the upgrade scheme.
func resolveFrameEndpoint(ctx context.Context, docURL, ref string) (string, error) {
	data, err := fetchFrameDoc(ctx, docURL)
	if err != nil {
		return "", err
	}
	doc, err := asyncapi.ParseDocument(data)
	if err != nil {
		return "", fmt.Errorf("parsing AsyncAPI document %s: %w", docURL, err)
	}
	endpoint, err := doc.ResolveEndpoint(ref, nil)
	if err != nil {
		return "", fmt.Errorf("resolving %s in %s: %w", ref, docURL, err)
	}

	// The frame lane is a WebSocket lane: an http(s)-protocol server takes
	// the upgrade on the same URL, spelled ws(s). Scheme spelling is frame-
	// transport mechanics, not AsyncAPI knowledge — the endpoint itself came
	// from the seam, which only ever yields the four bound protocols.
	switch endpoint.Protocol {
	case "http":
		return "ws://" + strings.TrimPrefix(endpoint.URL, "http://"), nil
	case "https":
		return "wss://" + strings.TrimPrefix(endpoint.URL, "https://"), nil
	default: // ws, wss
		return endpoint.URL, nil
	}
}

// fetchFrameDoc fetches a delegate's AsyncAPI document bytes under the frame
// lane's fetch policy (frameDocFetchTimeout, maxFrameBytes).
func fetchFrameDoc(ctx context.Context, docURL string) ([]byte, error) {
	if _, err := url.Parse(docURL); err != nil {
		return nil, fmt.Errorf("invalid AsyncAPI document URL %q: %w", docURL, err)
	}
	// SSRF guard: a delegate's document URL may be attacker-influenced; same
	// outbound policy as the other document fetches.
	if err := ValidateOutboundURL(docURL); err != nil {
		return nil, err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, frameDocFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, docURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := GuardedHTTPClient(0).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching AsyncAPI document %s: %w", docURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching AsyncAPI document %s: HTTP %d", docURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFrameBytes))
	if err != nil {
		return nil, fmt.Errorf("reading AsyncAPI document %s: %w", docURL, err)
	}
	return body, nil
}

// ---------------------------------------------------------------------------
// CLI transport (usage binding)
// ---------------------------------------------------------------------------

// delegateCLIInvoker invokes a delegate's invokeBinding through its usage
// (CLI) binding — the unary realization of the contract. The handle accepts
// at most one input; the delegate's inputTransform shapes the payload (e.g.
// `{ "input": $string($) }` stringifies it into a --input flag).
type delegateCLIInvoker struct {
	delegate string
	format   string
	iface    *openbindings.Interface
	binding  *openbindings.BindingEntry
	source   openbindings.Source
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
func delegateExecInvoker(delegate string) *openbindings.OperationInvoker {
	lane := DefaultInvoker().WithRuntime(nil) // blessed shallow copy; runtime fields ride
	lane.OutputDecoder = func(site openbindings.InvokeSite, raw openbindings.RawResult) (any, error) {
		if site.FamilyName() != "usage" {
			return nil, openbindings.ErrUseDefault
		}
		if len(raw.Body) == 0 {
			return nil, nil
		}
		var v any
		if err := json.Unmarshal(raw.Body, &v); err != nil {
			return nil, &openbindings.InvocationError{
				Code:    openbindings.ErrCodeResponseError,
				Message: fmt.Sprintf("delegate %q: binding-invoker machine lane must emit JSON, got: %v", delegate, err),
				Details: map[string]any{"stdout": string(raw.Body)},
			}
		}
		return v, nil
	}
	return lane
}

func (d *delegateCLIInvoker) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: d.format}}
}

func (d *delegateCLIInvoker) InvokeBinding(ctx context.Context, args *openbindings.BindingInvocationArgs) openbindings.Invocation[any, any] {
	impl := openbindings.NewInvocationImpl[any, any](ctx)

	go func() {
		// Unary: read at most one input, then close the input side so the
		// caller observes the unary shape through the handle.
		var input any
		v, err := impl.ReadInput(ctx)
		switch {
		case err == nil:
			input = v
		case errors.Is(err, io.EOF):
			// no-input invocation
		default:
			return // invocation already terminal
		}
		_ = impl.CloseInput()

		// The delegate's invokeBinding payload, shaped by its inputTransform.
		var payload any = InvocationInput{
			Source: InvokeSource{
				BindingSpec: args.Source.BindingSpec,
				Location:    args.Source.Location,
				Content:     args.Source.Content,
			},
			Ref:     args.Ref,
			Input:   input,
			Context: args.Context,
		}
		if d.binding.InputTransform != nil {
			transformed, tErr := ApplyTransform(d.iface.Transforms, d.binding.InputTransform, payload)
			if tErr != nil {
				impl.FireError(&openbindings.InvocationError{
					Code:    openbindings.ErrCodeTransformError,
					Message: fmt.Sprintf("delegate %q: input transform failed: %v", d.delegate, tErr),
				})
				return
			}
			payload = transformed
		}

		es, esErr := resolveSourceLocation(d.source)
		if esErr != nil {
			impl.FireError(&openbindings.InvocationError{
				Code:    openbindings.ErrCodeSourceConfigError,
				Message: esErr.Error(),
			})
			return
		}
		inner := delegateExecInvoker(d.delegate).InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
			Source:  es,
			Ref:     d.binding.Ref,
			Context: args.Context,
		})
		_ = inner.Write(ctx, payload)
		_ = inner.Close()

		out := inner.Outputs()
		for {
			v, rerr := out.Read(ctx)
			if errors.Is(rerr, io.EOF) {
				impl.CloseOutput()
				return
			}
			if rerr != nil {
				impl.FireError(openbindings.AsInvocationError(rerr))
				return
			}
			if impl.EmitOutput(v) != nil {
				inner.Cancel()
				return
			}
		}
	}()

	return impl
}
