package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"nhooyr.io/websocket"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/delegates"
	"github.com/openbindings/ob/internal/frames"
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

	var frameBinding, cliBinding *openbindings.BindingEntry
	for _, key := range sortedBindingKeys(iface) {
		b := iface.Bindings[key]
		if b.Operation != "invokeBinding" {
			continue
		}
		source, ok := iface.Sources[b.Source]
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(source.Format, "asyncapi") && delegates.IsHTTPURL(source.Location):
			if frameBinding == nil {
				bc := b
				frameBinding = &bc
			}
		case strings.HasPrefix(source.Format, "usage"):
			if cliBinding == nil {
				bc := b
				cliBinding = &bc
			}
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

func (d *delegateFrameInvoker) Formats() []openbindings.FormatInfo {
	return []openbindings.FormatInfo{{Token: d.format}}
}

func (d *delegateFrameInvoker) InvokeBinding(ctx context.Context, args *openbindings.BindingInvocationArgs) openbindings.Invocation[any, any] {
	input := &frames.BindingInvocationInput{
		Source: frames.InvokeSource{
			Format:   args.Source.Format,
			Location: args.Source.Location,
			Content:  args.Source.Content,
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

// asyncDoc is the minimal AsyncAPI 3 slice needed to locate the frame
// channel: servers (host/protocol/pathname), channel addresses, and the
// operation -> channel mapping.
type asyncDoc struct {
	Servers map[string]struct {
		Host     string `yaml:"host"`
		Protocol string `yaml:"protocol"`
		Pathname string `yaml:"pathname"`
	} `yaml:"servers"`
	Channels map[string]struct {
		Address string `yaml:"address"`
	} `yaml:"channels"`
	Operations map[string]struct {
		Channel struct {
			Ref string `yaml:"$ref"`
		} `yaml:"channel"`
	} `yaml:"operations"`
}

// resolveFrameEndpoint fetches the delegate's AsyncAPI document and derives
// the ws(s) URL of the operation the ref names.
func resolveFrameEndpoint(ctx context.Context, docURL, ref string) (string, error) {
	doc, err := fetchAsyncDoc(ctx, docURL)
	if err != nil {
		return "", err
	}

	opID := strings.TrimPrefix(strings.TrimSpace(ref), "#/operations/")
	op, ok := doc.Operations[opID]
	if !ok {
		return "", fmt.Errorf("operation %q not found in %s", opID, docURL)
	}
	channelName := op.Channel.Ref
	if idx := strings.LastIndex(channelName, "/"); idx >= 0 {
		channelName = channelName[idx+1:]
	}
	channel, ok := doc.Channels[channelName]
	if !ok || channel.Address == "" {
		return "", fmt.Errorf("channel %q has no address in %s", channelName, docURL)
	}

	// First supported server in stable name order, mirroring the SDK's
	// asyncapi invoker server selection.
	names := make([]string, 0, len(doc.Servers))
	for name := range doc.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		server := doc.Servers[name]
		var scheme string
		switch strings.ToLower(server.Protocol) {
		case "ws", "http":
			scheme = "ws"
		case "wss", "https":
			scheme = "wss"
		default:
			continue
		}
		base := scheme + "://" + server.Host + server.Pathname
		return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(channel.Address, "/"), nil
	}
	return "", fmt.Errorf("no ws-capable server in %s", docURL)
}

func fetchAsyncDoc(ctx context.Context, docURL string) (*asyncDoc, error) {
	if _, err := url.Parse(docURL); err != nil {
		return nil, fmt.Errorf("invalid AsyncAPI document URL %q: %w", docURL, err)
	}
	fetchCtx, cancel := context.WithTimeout(ctx, frameDocFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, docURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
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

	var doc asyncDoc
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parsing AsyncAPI document %s: %w", docURL, err)
	}
	return &doc, nil
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

func (d *delegateCLIInvoker) Formats() []openbindings.FormatInfo {
	return []openbindings.FormatInfo{{Token: d.format}}
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
		var payload any = InvokeOperationInput{
			Source: InvokeSource{
				Format:   args.Source.Format,
				Location: args.Source.Location,
				Content:  args.Source.Content,
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

		es := resolveSourceLocation(d.source, "")
		inner := DefaultInvoker().InvokeBinding(ctx, &openbindings.BindingInvocationArgs{
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
