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
	mux.HandleFunc("GET /operations/invoke", handleOperationInvoke(srv, logger))
	mux.HandleFunc("POST /operations/prepare", handleOperationPrepare(logger))
	mux.HandleFunc("POST /interfaces/synthesize", handleInterfaceSynthesize)
	mux.HandleFunc("POST /sources/inspect", handleSourceInspect)
}

// handleBindingInvoke serves invokeBinding as the binding-invoker frame
// protocol over WebSocket: the caller streams BindingInvokerInputFrame
// messages (open, input..., close) and receives BindingInvokerOutputFrame
// messages (output/input_closed..., then one terminal complete or error).
// One connection carries exactly one invocation.
func handleBindingInvoke(srv *server.Server, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn := acceptInvocationWebSocket(w, r, srv, logger, "binding-invoker")
		if conn == nil {
			return
		}
		defer conn.Close(websocket.StatusInternalError, "unexpected close")

		// The lifetime ctx must be cancelled when the client disconnects.
		// websocket.Accept hijacks the connection, so r.Context() is no
		// longer cancelled on client close; the frame reader cancels this
		// ctx when its read fails, tearing down the invocation and its
		// upstream transport.
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		serveBindingFrameStream(ctx, cancel, conn, logger)
	}
}

// handleOperationInvoke serves invokeOperation over the same frame transport
// as invokeBinding. The open payload carries the interface plus an operation
// or binding key; subsequent input/output frames are cardinality-agnostic.
func handleOperationInvoke(srv *server.Server, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn := acceptInvocationWebSocket(w, r, srv, logger, "operation-invoker")
		if conn == nil {
			return
		}
		defer conn.Close(websocket.StatusInternalError, "unexpected close")

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		serveOperationFrameStream(ctx, cancel, conn, logger)
	}
}

// acceptInvocationWebSocket applies the transport policy shared by both frame
// endpoints before the HTTP connection is upgraded. The frame context is for
// the downstream binding and never carries this server's transport token.
func acceptInvocationWebSocket(w http.ResponseWriter, r *http.Request, srv *server.Server, logger *slog.Logger, protocol string) *websocket.Conn {
	if !server.IsWebSocketUpgrade(r) {
		writeErrorJSON(w, http.StatusUpgradeRequired, "upgrade_required", "WebSocket upgrade required ("+protocol+" frame protocol)")
		return nil
	}
	if !srv.IsValidToken(wsAuthToken(r)) {
		logger.Warn("websocket auth failure", "remote_addr", r.RemoteAddr)
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
		return nil
	}
	if origin := r.Header.Get("Origin"); origin != "" && !srv.AllowsOrigin(origin) {
		logger.Warn("websocket origin rejected", "origin", origin, "remote_addr", r.RemoteAddr)
		writeErrorJSON(w, http.StatusForbidden, "origin_forbidden", "WebSocket origin is not allowed")
		return nil
	}

	// Origin validation happened above. InsecureSkipVerify disables the
	// library's narrower same-origin default so the shared CORS policy wins.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		logger.Error("websocket accept failed", "error", err)
		return nil
	}
	conn.SetReadLimit(maxRequestBodyBytes)
	return conn
}

// wsAuthToken extracts the session token from a WebSocket upgrade request:
// the Authorization header when present, else the `token` query parameter.
func wsAuthToken(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
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

// serveBindingFrameStream opens one binding-layer frame invocation, then hands
// the common input/output lifecycle to driveFrameStream.
// Frame-protocol rules enforced here: the first frame must be
// `open` (rule 1), a second `open` is a violation (rule 2), input after input
// closure from either side is ignored with a diagnostic (rule 3), exactly one
// terminal frame ends the stream (rule 4), and strict decoding rejects
// unknown frame properties (rule 7). CONTEXT_REQUIRED and every other
// terminal from the invocation handle pass through as the error frame.
func serveBindingFrameStream(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, logger *slog.Logger) {
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

	logger.Info("bindings/invoke (frames)", "format", open.Input.Source.BindingSpec, "ref", open.Input.Ref)

	inv := app.InvokeBindingHandle(ctx, app.InvocationInput{
		Source: app.InvokeSource{
			BindingSpec: open.Input.Source.BindingSpec,
			Location:    open.Input.Source.Location,
			Content:     open.Input.Source.Content,
		},
		Ref:     open.Input.Ref,
		Context: open.Input.Context,
	})
	driveFrameStream(ctx, cancel, conn, logger, writer, inv)
}

// serveOperationFrameStream opens one operation-layer frame invocation.
func serveOperationFrameStream(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, logger *slog.Logger) {
	writer := &frameWriter{conn: conn}
	first, err := readInputFrame(ctx, conn)
	if err != nil {
		return
	}
	var open frames.OperationInputFrame
	if uerr := json.Unmarshal(first, &open); uerr != nil {
		_ = writer.write(ctx, frames.Error(&openbindings.InvocationError{
			Code: openbindings.ErrCodeProtocol, Message: uerr.Error(),
		}))
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if open.Kind != frames.KindOpen {
		_ = writer.write(ctx, frames.Error(&openbindings.InvocationError{
			Code: openbindings.ErrCodeProtocol, Message: "first frame must be open, got " + open.Kind,
		}))
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}

	logger.Info("operations/invoke (frames)", "operation", open.Input.Operation, "binding", open.Input.Binding)
	inv := app.InvokeOperationHandle(ctx, app.OperationHandleInput{
		Interface: open.Input.Interface,
		Operation: open.Input.Operation,
		Binding:   open.Input.Binding,
		Context:   open.Input.Context,
	})
	driveFrameStream(ctx, cancel, conn, logger, writer, inv)
}

// driveFrameStream serializes an invocation handle over the shared frame
// lifecycle. Frame-protocol rules enforced here: a second open is a
// violation, input after closure is ignored, and exactly one terminal frame
// ends the stream.
func driveFrameStream(
	ctx context.Context,
	cancel context.CancelFunc,
	conn *websocket.Conn,
	logger *slog.Logger,
	writer *frameWriter,
	inv openbindings.Invocation[any, any],
) {

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
		var header struct {
			Kind string `json:"kind"`
		}
		if uerr := json.Unmarshal(raw, &header); uerr == nil && header.Kind == frames.KindOpen {
			violation("second open frame")
			return
		}
		var frame frames.InputFrame
		if uerr := json.Unmarshal(raw, &frame); uerr != nil {
			violation(uerr.Error()) // rules 1/7: malformed or unknown-property frame
			return
		}
		switch frame.Kind {
		case frames.KindOpen:
			violation("second open frame")
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
		if !validateJSONMediaType(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeRequestDecodeError(w, err)
			return
		}
		input, derr := frames.DecodeInvocationInput(raw)
		if derr != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", derr.Error())
			return
		}

		logger.Info("bindings/prepare", "format", input.Source.BindingSpec, "ref", input.Ref)

		details, perr := app.PrepareBinding(r.Context(), app.InvocationInput{
			Source: app.InvokeSource{
				BindingSpec: input.Source.BindingSpec,
				Location:    input.Source.Location,
				Content:     input.Source.Content,
			},
			Ref:     input.Ref,
			Context: input.Context,
		})
		if perr != nil {
			writeErrorJSON(w, http.StatusBadRequest, "preflight_failed", perr.Error())
			return
		}
		writeJSON(w, http.StatusOK, details)
	}
}

// handleOperationPrepare is the document-valued, by-reference counterpart to
// handleBindingPrepare.
func handleOperationPrepare(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Transport adaptation: the OpenAPI artifact wraps the conditional
		// OperationInvocationInput under `input`, and the bound OBI's input
		// transform performs the inverse for operation callers.
		var envelope struct {
			Input json.RawMessage `json:"input"`
		}
		if !decodeRequest(w, r, &envelope) {
			return
		}
		if len(envelope.Input) == 0 {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "input is required")
			return
		}
		input, derr := frames.DecodeOperationInvocationInput(envelope.Input)
		if derr != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", derr.Error())
			return
		}

		logger.Info("operations/prepare", "operation", input.Operation, "binding", input.Binding)
		details, perr := app.PrepareInterfaceOperation(
			r.Context(), input.Interface, input.Operation, input.Binding, input.Context,
		)
		if perr != nil {
			writeErrorJSON(w, http.StatusBadRequest, "preflight_failed", perr.Error())
			return
		}
		writeJSON(w, http.StatusOK, details)
	}
}

func handleInterfaceSynthesize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OpenBindingsVersion string                          `json:"openbindingsVersion,omitempty"`
		Sources             []app.SynthesizeInterfaceSource `json:"sources,omitempty"`
		Name                string                          `json:"name,omitempty"`
		Version             string                          `json:"version,omitempty"`
		Description         string                          `json:"description,omitempty"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}

	iface, err := app.SynthesizeInterface(app.SynthesizeInterfaceInput{
		OpenBindingsVersion: body.OpenBindingsVersion,
		Sources:             body.Sources,
		Name:                body.Name,
		Version:             body.Version,
		Description:         body.Description,
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "synthesis_failed", err.Error())
		return
	}
	writeOBI(w, http.StatusOK, iface)
}

func handleSourceInspect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source openbindings.Source `json:"source"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Source.BindingSpec == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "source.bindingSpec is required")
		return
	}

	result, err := app.InspectSource(r.Context(), &body.Source)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "inspection_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// registerAuthoringRoutes adds interface authoring endpoints (validate, diff, compat).
func registerAuthoringRoutes(srv *server.Server) {
	mux := srv.Mux()
	// Canonical document API.
	mux.HandleFunc("POST /interfaces/validate", handleValidate)
	mux.HandleFunc("POST /interfaces/compare", handleDiff)
	mux.HandleFunc("POST /interfaces/compatibility", handleCompat)
	mux.HandleFunc("POST /interfaces/conform", handleConform)
	mux.HandleFunc("POST /interfaces/codegen", handleCodegen)
	mux.HandleFunc("POST /interfaces/merge", handleMerge)

	// Compatibility aliases from the original preview surface. They remain
	// callable but are not published in the canonical OpenAPI document.
	mux.HandleFunc("POST /validate", handleValidate)
	mux.HandleFunc("POST /diff", handleDiff)
	mux.HandleFunc("POST /compatibility", handleCompat)
	mux.HandleFunc("POST /conform", handleConform)
	mux.HandleFunc("POST /codegen", handleCodegen)
	mux.HandleFunc("POST /merge", handleMerge)
	mux.HandleFunc("POST /interfaces/status", handleInterfaceStatus)
}

func handleValidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Strict    bool                    `json:"strict,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}

	report := app.ValidateInterface(app.ValidateInput{
		Interface: body.Interface,
		Strict:    body.Strict,
	})
	writeJSON(w, http.StatusOK, report)
}

func handleDiff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Baseline    *openbindings.Interface `json:"baseline"`
		Comparison  *openbindings.Interface `json:"comparison,omitempty"`
		FromSources bool                    `json:"fromSources,omitempty"`
		Only        string                  `json:"only,omitempty"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Baseline == nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "baseline is required")
		return
	}

	report, err := app.Diff(app.DiffInput{
		BaselineInterface:   body.Baseline,
		ComparisonInterface: body.Comparison,
		FromSources:         body.FromSources,
		OnlySource:          body.Only,
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "comparison_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func handleCompat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target    *openbindings.Interface `json:"target"`
		Candidate *openbindings.Interface `json:"candidate"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Target == nil || body.Candidate == nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "target and candidate are required")
		return
	}

	// Target maps to the report's left side, candidate to its right, matching
	// the CLI's `ob compat <target> <candidate>`.
	report := app.ComparisonCheck(app.ComparisonInput{
		LeftInterface:  body.Target,
		RightInterface: body.Candidate,
	})
	writeJSON(w, http.StatusOK, report)
}

func handleConform(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Target    *openbindings.Interface `json:"target"`
		DryRun    bool                    `json:"dryRun,omitempty"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Interface == nil || body.Target == nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "interface and target are required")
		return
	}
	// Non-interactive on the wire: accept all scaffolding/replacements. With no
	// target path, Conform returns the conformed document in Result rather than
	// writing a file; dryRun previews without modifying it.
	out := app.Conform(app.ConformInput{
		ContractInterface: body.Interface,
		TargetInterface:   body.Target,
		Yes:               true,
		DryRun:            body.DryRun,
	}, func(string, string) bool { return true })
	writeJSON(w, http.StatusOK, out)
}

func handleCodegen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Language  string                  `json:"language"`
		Package   string                  `json:"package,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
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
		writeErrorJSON(w, http.StatusBadRequest, "unsupported_language", "language must be typescript or go")
		return
	}
	result, err := codegen.Generate(body.Interface)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "codegen_failed", err.Error())
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
	var body struct {
		Target            *openbindings.Interface `json:"target"`
		Source            *openbindings.Interface `json:"source,omitempty"`
		FromSources       bool                    `json:"fromSources,omitempty"`
		Only              string                  `json:"only,omitempty"`
		Operations        []string                `json:"operations,omitempty"`
		ExcludeOperations []string                `json:"excludeOperations,omitempty"`
		OpsOnly           bool                    `json:"opsOnly,omitempty"`
		NoBindings        bool                    `json:"noBindings,omitempty"`
		NoSources         bool                    `json:"noSources,omitempty"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Target == nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "target is required")
		return
	}
	// Non-interactive: apply all actionable entries and return the merged
	// document in Result rather than writing a file.
	out, err := app.Merge(app.MergeInput{
		TargetInterface: body.Target,
		SourceInterface: body.Source,
		FromSources:     body.FromSources,
		OnlySource:      body.Only,
		Operations:      body.Operations,
		ExcludeOps:      body.ExcludeOperations,
		OpsOnly:         body.OpsOnly,
		NoBindings:      body.NoBindings,
		NoSources:       body.NoSources,
		All:             true,
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "merge_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleInterfaceStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	out, err := app.OBIStatus(app.OBIStatusInput{Interface: body.Interface})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
