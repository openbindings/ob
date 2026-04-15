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

	mux.HandleFunc("GET /openapi.json", serveEmbedded("api/openapi.json", "application/json"))
	mux.HandleFunc("GET /asyncapi.json", serveEmbedded("api/asyncapi.json", "application/json"))
	mux.HandleFunc("GET /.well-known/openbindings", serveOBI(cfg.Port))

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

func serveOBI(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := apiFS.ReadFile("api/openbindings.json")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		baseURL := fmt.Sprintf("http://localhost:%d", port)

		body := string(data)
		body = strings.ReplaceAll(body, `"./openapi.json"`, `"`+baseURL+`/openapi.json"`)
		body = strings.ReplaceAll(body, `"./asyncapi.json"`, `"`+baseURL+`/asyncapi.json"`)
		body = strings.ReplaceAll(body, `"./mcp"`, `"`+baseURL+`/mcp"`)
		body = strings.ReplaceAll(body, `"./graphql"`, `"`+baseURL+`/graphql"`)
		body = strings.ReplaceAll(body, `"location": "./"`, `"location": "`+baseURL+`"`)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write([]byte(body))
	}
}

func serveEmbedded(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := apiFS.ReadFile(path)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(data)
	}
}
