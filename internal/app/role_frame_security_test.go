package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/formats/asyncapi"
	"github.com/openbindings/openbindings-go/formats/usage"
	"github.com/openbindings/openbindings-go/invoke"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

// The candidate must retain the previous delegate artifact egress guard when
// switching to SDK invocation. A fake transport proves no private dial occurs;
// this test never sends traffic to a private or link-local destination.
func TestRoleFrameEntrypointArtifactEgress(t *testing.T) {
	for _, address := range []string{"http://169.254.169.254/asyncapi.yaml", "http://10.0.0.5/asyncapi.yaml"} {
		t.Run(address, func(t *testing.T) {
			r, _ := migrationTestRegistry(t)
			provider, err := openbindings.ValidateDocument(roleFrameProvider(t))
			if err != nil {
				t.Fatal(err)
			}
			provider.Sources["frames"] = openbindings.Source{BindingSpec: asyncapi.BindingSpec, Location: address}
			raw, err := jsonvalue.Marshal(provider)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.register(RoleRegistrationInput{Interface: raw, Roles: []string{"invoke"}}); err != nil {
				t.Fatal(err)
			}
			var attempts atomic.Int32
			transport := http.DefaultTransport
			http.DefaultTransport = &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				attempts.Add(1)
				return nil, errors.New("test forbids network")
			}}
			t.Cleanup(func() { http.DefaultTransport = transport })
			ResetDefaultInvoker()
			t.Cleanup(ResetDefaultInvoker)
			DefaultInvoker().AddBindingInvoker(&roleTestInvoker{result: func(string, any) any {
				return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
			}})
			resetNativeTokens()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			call := InvokeBindingHandle(ctx, roleFrameInput())
			defer call.Cancel()
			if _, err := call.Outputs().Read(ctx); err == nil {
				t.Fatal("private artifact accepted")
			}
			if attempts.Load() != 0 {
				t.Fatalf("private artifact reached transport %d times", attempts.Load())
			}
		})
	}
}

func TestRoleFrameEntrypointRegistrationDoesNotAuthorizeExec(t *testing.T) {
	r, _ := migrationTestRegistry(t)
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	provider, err := openbindings.ValidateDocument(roleFrameProvider(t))
	if err != nil {
		t.Fatal(err)
	}
	provider.Sources["frames"] = openbindings.Source{BindingSpec: usage.BindingSpec, Location: "exec:touch " + marker}
	raw, err := jsonvalue.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.register(RoleRegistrationInput{Interface: raw, Roles: []string{"invoke"}}); err != nil {
		t.Fatal(err)
	}
	queries := &roleTestInvoker{result: func(string, any) any {
		return []any{map[string]any{"bindingSpec": "example.work@1", "supported": true}}
	}}
	installRoleFrameRuntime(t, invoke.NewOperationInvoker(queries, newUsageInvoker()))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	call := InvokeBindingHandle(ctx, roleFrameInput())
	defer call.Cancel()
	if _, err := call.Outputs().Read(ctx); err == nil {
		t.Fatal("unauthorized executable accepted")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registration authorized exec: %v", err)
	}
	if authorizeExecAddress([]string{"touch", marker}) {
		t.Fatal("registered address gained exec authority")
	}
}
