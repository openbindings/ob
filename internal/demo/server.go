package demo

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"strings"
	"time"
)

//go:embed api/*
var apiFS embed.FS

// Config holds the demo server configuration.
type Config struct {
	Port     int
	GRPCPort int
}

// Server is the demo server instance.
type Server struct {
	store    *Store
	httpAddr string
	grpcPort int
}

// Start launches the demo server and blocks until the context is cancelled.
func Start(ctx context.Context, cfg Config) error {
	store := NewStore()
	defer store.Stop()

	grpcSrv, err := StartGRPCServer(store, cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("gRPC: %w", err)
	}
	defer grpcSrv.GracefulStop()

	mux := http.NewServeMux()

	RegisterRESTRoutes(mux, store)
	RegisterConnectRoutes(mux, store)
	RegisterSSERoutes(mux, store)
	RegisterGraphQLRoutes(mux, store)

	mcpHandler := NewMCPHandler(store)
	mux.Handle("POST /mcp", mcpHandler)
	mux.Handle("GET /mcp", mcpHandler)
	mux.Handle("DELETE /mcp", mcpHandler)

	mux.HandleFunc("GET /openapi.json", serveSpec("api/openapi.json", cfg.Port, cfg.GRPCPort))
	mux.HandleFunc("GET /asyncapi.json", serveSpec("api/asyncapi.json", cfg.Port, cfg.GRPCPort))
	mux.HandleFunc("GET /.well-known/openbindings", serveOBI(cfg.Port, cfg.GRPCPort))

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func serveOBI(port, grpcPort int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := apiFS.ReadFile("api/openbindings.json")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		baseURL := fmt.Sprintf("http://localhost:%d", port)

		// The static OBI declares the default ports (http://localhost:8080,
		// grpc localhost:9090) so it is a valid standalone OBI; rewrite both
		// to the running ports so the served document's source locations
		// point at this process.
		body := strings.ReplaceAll(string(data), "http://localhost:8080", baseURL)
		body = strings.ReplaceAll(body, "localhost:9090", fmt.Sprintf("localhost:%d", grpcPort))

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write([]byte(body))
	}
}

// serveSpec serves an embedded source spec (openapi.json / asyncapi.json)
// with its declared default ports rewritten to the running ones. The specs,
// like the OBI, declare http://localhost:8080 / localhost:9090 so they are
// valid standalone documents; a consumer that resolves a server URL from
// them (the openapi invoker reads `servers`) must land on this process,
// whatever --port it runs on.
func serveSpec(path string, port, grpcPort int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := apiFS.ReadFile(path)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Bare host:port form first covers AsyncAPI's scheme-less `host`
		// field; it also subsumes the http://-prefixed occurrences.
		body := strings.ReplaceAll(string(data), "localhost:8080", fmt.Sprintf("localhost:%d", port))
		body = strings.ReplaceAll(body, "localhost:9090", fmt.Sprintf("localhost:%d", grpcPort))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write([]byte(body))
	}
}
