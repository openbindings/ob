package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/server"
)

// registerBindingRoutes adds direct binding invocation and interface creation endpoints,
// making ob serve a binding invoker host.
func registerBindingRoutes(srv *server.Server, logger *slog.Logger) {
	mux := srv.Mux()
	mux.HandleFunc("/bindings/invoke", handleBindingInvoke(srv, logger))
	mux.HandleFunc("POST /interfaces/create", handleInterfaceCreate)
	mux.HandleFunc("POST /sources/inspect", handleSourceInspect)
	mux.HandleFunc("POST /http/request", handleHttpRequest(logger))
}

type wsErrorDetail struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	// Details carries structured info from the SDK's InvocationError.Details.
	// For OBI-T-07 / OBI-T-08 validation failures, this is a
	// ValidationFailureDetails value with a Failures slice.
	Details any `json:"details,omitempty"`
}

// wsInvocationOutput is the JSON envelope sent over WebSocket for each output
// of a streaming invocation.
type wsInvocationOutput struct {
	Type   string         `json:"type"`
	Output any            `json:"output,omitempty"`
	Error  *wsErrorDetail `json:"error,omitempty"`
}

func handleBindingInvoke(srv *server.Server, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) {
			handleBindingInvokeWS(srv, logger, w, r)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		var body struct {
			Source    app.InvokeSource        `json:"source"`
			Ref       string                  `json:"ref"`
			Input     any                     `json:"input,omitempty"`
			Context   map[string]any          `json:"context,omitempty"`
			Interface *openbindings.Interface `json:"interface,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		logger.Info("bindings/invoke", "format", body.Source.Format, "ref", body.Ref)

		output := app.InvokeOperationWithContext(r.Context(), app.InvokeOperationInput{
			Source:    body.Source,
			Ref:       body.Ref,
			Input:     body.Input,
			Context:   body.Context,
			Interface: body.Interface,
		})

		status := http.StatusOK
		if output.Error != nil {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, output)
	}
}

func handleBindingInvokeWS(srv *server.Server, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	// Origin checking is skipped to match the CORS policy (any HTTPS
	// origin + any localhost origin). Bearer-token auth in the first
	// WebSocket message is the security boundary, not the origin header.
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
	// websocket.Accept hijacks the connection, so r.Context() is no longer
	// cancelled on client close; without an explicit cancel the invocation
	// goroutine and its upstream transport would leak on every abandoned
	// stream. cancel() also fires on every early return below.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var body struct {
		Source      app.InvokeSource        `json:"source"`
		Ref         string                  `json:"ref"`
		Input       any                     `json:"input,omitempty"`
		Context     map[string]any          `json:"context,omitempty"`
		Interface   *openbindings.Interface `json:"interface,omitempty"`
		BearerToken string                  `json:"bearerToken,omitempty"`
	}
	if err := wsjson.Read(ctx, conn, &body); err != nil {
		logger.Error("websocket read initial message failed", "error", err)
		conn.Close(websocket.StatusProtocolError, "expected JSON invocation request")
		return
	}

	// Validate bearer token from the first message.
	// WebSocket connections bypass HTTP auth middleware (browsers can't set
	// headers on upgrade). Auth is in the message body per the AsyncAPI spec.
	if body.BearerToken == "" || !srv.IsValidToken(body.BearerToken) {
		// Also check query param fallback for backward compat.
		qToken := r.URL.Query().Get("token")
		if qToken == "" || !srv.IsValidToken(qToken) {
			logger.Warn("websocket auth failure", "remote_addr", r.RemoteAddr)
			conn.Close(websocket.StatusPolicyViolation, "unauthorized")
			return
		}
	}

	// After the single request frame, this is a server→client stream. Hand the
	// read side to CloseRead, whose returned context is cancelled when the
	// client sends anything (including a close frame) or the connection drops —
	// that cancellation tears down the invocation and unblocks driveBinding.
	ctx = conn.CloseRead(ctx)

	logger.Info("bindings/invoke (ws)", "format", body.Source.Format, "ref", body.Ref)

	execInput := app.InvokeOperationInput{
		Source:    body.Source,
		Ref:       body.Ref,
		Input:     body.Input,
		Context:   body.Context,
		Interface: body.Interface,
	}

	events, err := app.SubscribeOperationWithContext(ctx, execInput)
	if err != nil {
		// Streaming not available for this operation — fall back to unary.
		out := app.InvokeOperationWithContext(ctx, execInput)
		if out.Error != nil {
			_ = wsjson.Write(ctx, conn, wsInvocationOutput{
				Type: "error",
				Error: &wsErrorDetail{
					Message: out.Error.Message,
					Code:    out.Error.Code,
					Details: out.Error.Details,
				},
			})
		} else {
			_ = wsjson.Write(ctx, conn, wsInvocationOutput{Type: "event", Output: out.Output})
		}
		conn.Close(websocket.StatusNormalClosure, "")
		return
	}

	// Outputs are outputs; errors are errors. Each event carries either an
	// output value (an "event" frame) or a terminal error (an "error" frame),
	// never both — a terminal error ends the stream. (OBI-T-08 output
	// validation failures are terminal under the invocation handle model.)
	for ev := range events {
		var frame wsInvocationOutput
		if ev.Error != nil {
			frame = wsInvocationOutput{
				Type: "error",
				Error: &wsErrorDetail{
					Message: ev.Error.Message,
					Code:    ev.Error.Code,
					Details: ev.Error.Details,
				},
			}
		} else {
			frame = wsInvocationOutput{Type: "event", Output: ev.Output}
		}
		if err := wsjson.Write(ctx, conn, frame); err != nil {
			logger.Error("websocket write failed", "error", err)
			return
		}
	}

	conn.Close(websocket.StatusNormalClosure, "stream complete")
}

func isWebSocketUpgrade(r *http.Request) bool {
	for _, v := range r.Header.Values("Connection") {
		for _, token := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
					return true
				}
			}
		}
	}
	return false
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
