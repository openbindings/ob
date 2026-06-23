package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/codegen"
	"github.com/openbindings/ob/internal/frames"
	"github.com/openbindings/ob/internal/server"
)

// registerBindingRoutes adds binding invocation (the binding-invoker frame
// protocol), preflight, and interface creation endpoints, making ob start a
// binding invoker host.
func registerBindingRoutes(srv *server.Server, logger *slog.Logger) {
	mux := srv.Mux()
	mux.HandleFunc("GET /bindings/invoke", handleBindingInvoke(srv, logger))
	mux.HandleFunc("POST /bindings/prepare", handleBindingPrepare(logger))
	mux.HandleFunc("POST /interfaces/create", handleInterfaceCreate)
	mux.HandleFunc("POST /sources/inspect", handleSourceInspect)
}

// handleBindingInvoke serves invokeBinding as the binding-invoker frame
// protocol over WebSocket: the caller streams BindingInvokerInputFrame
// messages (open, input..., close) and receives BindingInvokerOutputFrame
// messages (output/input_closed..., then one terminal complete or error).
// One connection carries exactly one invocation.
func handleBindingInvoke(srv *server.Server, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !server.IsWebSocketUpgrade(r) {
			http.Error(w, "websocket upgrade required (binding-invoker frame protocol)", http.StatusUpgradeRequired)
			return
		}

		// Authenticate the upgrade request itself: Authorization header for
		// clients that can set one, `token` query parameter for browsers
		// (which can't set headers on WebSocket upgrades). The frame protocol
		// carries no transport credentials — the open frame's context is the
		// DOWNSTREAM binding's context, never this server's session token.
		if !srv.IsValidToken(wsAuthToken(r)) {
			logger.Warn("websocket auth failure", "remote_addr", r.RemoteAddr)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Origin checking is skipped to match the CORS policy (any HTTPS
		// origin + any localhost origin). The session token on the upgrade
		// request is the security boundary, not the origin header.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			logger.Error("websocket accept failed", "error", err)
			return
		}
		defer conn.Close(websocket.StatusInternalError, "unexpected close")

		conn.SetReadLimit(maxRequestBodyBytes)

		// The lifetime ctx must be cancelled when the client disconnects.
		// websocket.Accept hijacks the connection, so r.Context() is no
		// longer cancelled on client close; the frame reader cancels this
		// ctx when its read fails, tearing down the invocation and its
		// upstream transport.
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		serveFrameStream(ctx, cancel, conn, logger)
	}
}

// wsAuthToken extracts the session token from a WebSocket upgrade request:
// the Authorization header when present, else the `token` query parameter.
func wsAuthToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

// frameWriter serializes output-frame writes and latches the terminal frame:
// exactly one terminal frame is ever written and nothing follows it (rule 4).
type frameWriter struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	terminal bool
}

func (fw *frameWriter) write(ctx context.Context, frame frames.OutputFrame) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.terminal {
		return nil
	}
	if frame.Terminal() {
		fw.terminal = true
	}
	return wsjson.Write(ctx, fw.conn, frame)
}

// serveFrameStream drives one frame-protocol invocation over an accepted
// connection. Frame-protocol rules enforced here: the first frame must be
// `open` (rule 1), a second `open` is a violation (rule 2), input after input
// closure from either side is ignored with a diagnostic (rule 3), exactly one
// terminal frame ends the stream (rule 4), and strict decoding rejects
// unknown frame properties (rule 7). CONTEXT_REQUIRED and every other
// terminal from the invocation handle pass through as the error frame.
func serveFrameStream(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, logger *slog.Logger) {
	writer := &frameWriter{conn: conn}

	// Rule 1: the first frame must be `open`; anything else (including a
	// frame that fails strict decoding) is a terminal ERR_PROTOCOL and no
	// further input is processed.
	first, err := readInputFrame(ctx, conn)
	if err != nil {
		return // transport closed before any frame
	}
	var open frames.InputFrame
	if uerr := json.Unmarshal(first, &open); uerr != nil {
		_ = writer.write(ctx, frames.Error(&openbindings.InvocationError{
			Code: openbindings.ErrCodeProtocol, Message: uerr.Error(),
		}))
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if open.Kind != frames.KindOpen {
		_ = writer.write(ctx, frames.Error(&openbindings.InvocationError{
			Code:    openbindings.ErrCodeProtocol,
			Message: "first frame must be open, got " + open.Kind,
		}))
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}

	logger.Info("bindings/invoke (frames)", "format", open.Input.Source.Format, "ref", open.Input.Ref)

	inv := app.InvokeBindingHandle(ctx, app.InvokeOperationInput{
		Source: app.InvokeSource{
			Format:   open.Input.Source.Format,
			Location: open.Input.Source.Location,
			Content:  open.Input.Source.Content,
		},
		Ref:     open.Input.Ref,
		Context: open.Input.Context,
	})

	// A protocol violation after open (second open, undecodable frame)
	// terminates the invocation; the violation's error replaces the handle's
	// ERR_CANCELLED on the terminal frame.
	protoErr := make(chan *openbindings.InvocationError, 1)
	var callerClosed atomic.Bool

	// Input side: frames -> handle.
	go readInputFrames(ctx, cancel, conn, inv, &callerClosed, protoErr, logger)

	// `input_closed`: emitted once when the binding closes the input side
	// from below (a unary binding after its first read). The caller's own
	// close needs no echo; the terminal path is owned by the output pump
	// (writer latching keeps any race legal under rule 4).
	go func() {
		select {
		case <-inv.InputClosed():
			if !callerClosed.Load() {
				_ = writer.write(ctx, frames.InputClosed())
			}
		case <-ctx.Done():
		}
	}()

	// Output side: handle -> frames, ending in exactly one terminal frame.
	out := inv.Outputs()
	for {
		v, rerr := out.Read(ctx)
		if errors.Is(rerr, io.EOF) {
			_ = writer.write(ctx, frames.Complete())
			break
		}
		if rerr != nil {
			ie := openbindings.AsInvocationError(rerr)
			select {
			case pe := <-protoErr:
				ie = pe
			default:
			}
			_ = writer.write(ctx, frames.Error(ie))
			break
		}
		if werr := writer.write(ctx, frames.Output(v)); werr != nil {
			// Client unreachable: tear the invocation down and stop.
			logger.Error("websocket write failed", "error", werr)
			out.Stop()
			return
		}
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

// readInputFrames consumes the caller's frame stream after open, driving the
// invocation handle. It exits when the transport closes (cancelling the
// invocation's lifetime ctx) or on a protocol violation.
func readInputFrames(
	ctx context.Context,
	cancel context.CancelFunc,
	conn *websocket.Conn,
	inv openbindings.Invocation[any, any],
	callerClosed *atomic.Bool,
	protoErr chan<- *openbindings.InvocationError,
	logger *slog.Logger,
) {
	violation := func(reason string) {
		select {
		case protoErr <- &openbindings.InvocationError{Code: openbindings.ErrCodeProtocol, Message: reason}:
		default:
		}
		inv.Cancel()
	}

	for {
		raw, err := readInputFrame(ctx, conn)
		if err != nil {
			// Disconnect, client close frame, or our own teardown: cancel the
			// invocation lifetime so nothing leaks on abandoned streams.
			cancel()
			return
		}
		var frame frames.InputFrame
		if uerr := json.Unmarshal(raw, &frame); uerr != nil {
			violation(uerr.Error()) // rules 1/7: malformed or unknown-property frame
			return
		}
		switch frame.Kind {
		case frames.KindOpen:
			violation("second open frame") // rule 2
			return
		case frames.KindInput:
			if callerClosed.Load() || inputSideClosed(inv) {
				// Rule 3: input after input closure from either side is
				// ignored — never written, never terminal. Surface a
				// diagnostic and keep the invocation flowing.
				logger.Debug("ignoring input frame after input closure")
				continue
			}
			if werr := inv.Write(ctx, frame.Value); werr != nil {
				var ie *openbindings.InvocationError
				if errors.As(werr, &ie) && ie.Code == openbindings.ErrCodeInputClosed {
					// Rule 3's inherent race: closure landed while the write
					// was in flight. Same treatment as the pre-checked case.
					logger.Debug("ignoring input frame after input closure")
					continue
				}
				// Invocation already terminal; the output pump owns the
				// terminal frame. Keep draining until the socket closes.
				continue
			}
		case frames.KindClose:
			callerClosed.Store(true)
			_ = inv.Close()
			// Keep reading: late frames are ignored per rule 3, and a second
			// open after close is still a rule 2 violation.
		}
	}
}

// inputSideClosed reports (without blocking) whether the invocation's input
// side has closed from either side.
func inputSideClosed(inv openbindings.Invocation[any, any]) bool {
	select {
	case <-inv.InputClosed():
		return true
	default:
		return false
	}
}

// readInputFrame reads one raw text frame from the socket.
func readInputFrame(ctx context.Context, conn *websocket.Conn) ([]byte, error) {
	_, data, err := conn.Read(ctx)
	return data, err
}

// handleBindingPrepare serves prepareBinding: the side-effect-free preflight
// reporting the context a binding would require (ContextRequiredDetails), or
// null when requirements cannot be determined statically.
func handleBindingPrepare(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}
		input, derr := frames.DecodeInvocationInput(raw)
		if derr != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: derr.Error()})
			return
		}

		logger.Info("bindings/prepare", "format", input.Source.Format, "ref", input.Ref)

		details, perr := app.PrepareBinding(r.Context(), app.InvokeOperationInput{
			Source: app.InvokeSource{
				Format:   input.Source.Format,
				Location: input.Source.Location,
				Content:  input.Source.Content,
			},
			Ref:     input.Ref,
			Context: input.Context,
		})
		if perr != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: perr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, details)
	}
}

func handleInterfaceCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		OpenBindingsVersion string                      `json:"openbindingsVersion,omitempty"`
		Sources             []app.CreateInterfaceSource `json:"sources,omitempty"`
		Name                string                      `json:"name,omitempty"`
		Version             string                      `json:"version,omitempty"`
		Description         string                      `json:"description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	iface, err := app.CreateInterface(app.CreateInterfaceInput{
		OpenBindingsVersion: body.OpenBindingsVersion,
		Sources:             body.Sources,
		Name:                body.Name,
		Version:             body.Version,
		Description:         body.Description,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, iface)
}

func handleSourceInspect(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		Source openbindings.Source `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}
	if body.Source.Format == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "source.format is required"})
		return
	}

	result, err := app.InspectSource(r.Context(), &body.Source)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// registerAuthoringRoutes adds interface authoring endpoints (validate, diff, compat).
func registerAuthoringRoutes(srv *server.Server) {
	mux := srv.Mux()
	mux.HandleFunc("POST /validate", handleValidate)
	mux.HandleFunc("POST /diff", handleDiff)
	mux.HandleFunc("POST /compatibility", handleCompat)
	mux.HandleFunc("POST /conform", handleConform)
	mux.HandleFunc("POST /codegen", handleCodegen)
	mux.HandleFunc("POST /merge", handleMerge)
	mux.HandleFunc("POST /interface-status", handleInterfaceStatus)
}

func handleValidate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		Locator string `json:"locator"`
		Strict  bool   `json:"strict,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	report := app.ValidateInterface(app.ValidateInput{
		Locator: body.Locator,
		Strict:  body.Strict,
	})
	writeJSON(w, http.StatusOK, report)
}

func handleDiff(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		Baseline    string `json:"baseline"`
		Comparison  string `json:"comparison,omitempty"`
		FromSources bool   `json:"fromSources,omitempty"`
		OnlySource  string `json:"onlySource,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	report, err := app.Diff(app.DiffInput{
		BaselineLocator:   body.Baseline,
		ComparisonLocator: body.Comparison,
		FromSources:       body.FromSources,
		OnlySource:        body.OnlySource,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func handleCompat(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		Target    string `json:"target"`
		Candidate string `json:"candidate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	report := app.CompatibilityCheck(app.CompatInput{
		Target:    body.Target,
		Candidate: body.Candidate,
	})
	writeJSON(w, http.StatusOK, report)
}

func handleConform(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		Interface string `json:"interface"`
		Target    string `json:"target"`
		Yes       bool   `json:"yes,omitempty"`
		DryRun    bool   `json:"dryRun,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}
	// No interactive prompts on the wire: the confirm callback answers with the
	// request's `yes` flag (combine with `dryRun` to preview without writing).
	out := app.Conform(app.ConformInput{
		InterfaceLocator: body.Interface,
		TargetPath:       body.Target,
		Yes:              body.Yes,
		DryRun:           body.DryRun,
	}, func(string, string) bool { return body.Yes })
	writeJSON(w, http.StatusOK, out)
}

func handleCodegen(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		Source   string `json:"source"`
		Language string `json:"language"`
		Package  string `json:"package,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}
	lang := body.Language
	switch lang {
	case "ts":
		lang = "typescript"
	case "golang":
		lang = "go"
	}
	if lang != "typescript" && lang != "go" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "unsupported language (want typescript or go)"})
		return
	}
	iface, err := app.ResolveInterface(body.Source)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	result, err := codegen.Generate(iface)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	var code string
	switch lang {
	case "typescript":
		code = codegen.EmitTypeScript(result)
	case "go":
		code = codegen.EmitGo(result, body.Package)
	}
	writeJSON(w, http.StatusOK, map[string]any{"language": lang, "code": code})
}

func handleMerge(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		Target string `json:"target"`
		Source string `json:"source,omitempty"`
		All    bool   `json:"all,omitempty"`
		DryRun bool   `json:"dryRun,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}
	out, err := app.Merge(app.MergeInput{
		TargetPath:    body.Target,
		SourceLocator: body.Source,
		All:           body.All,
		DryRun:        body.DryRun,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleInterfaceStatus(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}
	out, err := app.OBIStatus(app.OBIStatusInput{OBIPath: body.Path})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
