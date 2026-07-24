package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openbindings/ob/internal/demo"
)

// TestDemoSmoke_PrintedCommands drives every command `ob demo` prints
// against an in-process demo server through the real binary. The demo is
// the README's first suggested action, so its printed transcript must
// never rot: a handler drift (wrong Content-Type, stale binding key,
// changed flag grammar) fails here instead of in an evaluator's terminal.
func TestDemoSmoke_PrintedCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and execs the real binary")
	}

	httpPort := freePort(t)
	grpcPort := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = demo.Start(ctx, demo.Config{Port: httpPort, GRPCPort: grpcPort}) }()

	base := fmt.Sprintf("http://localhost:%d", httpPort)
	waitReady(t, base+"/api/menu")

	binDir := t.TempDir()
	ob := filepath.Join(binDir, "ob")
	build := exec.Command("go", "build", "-o", ob, "./cmd/ob")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ob: %v\n%s", err, out)
	}

	// Commands run from a sandbox dir: `ob resolve` writes the resolved OBI
	// to its working directory, which must never be the repo.
	workDir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(ob, args...)
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ob %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	// Discover.
	run("resolve", fmt.Sprintf("localhost:%d", httpPort))
	run("validate", base)
	run("op", "list", base)

	// Invoke every explicitly selected protocol the demo advertises.
	for _, binding := range []string{
		"getMenu.restApi",
		"getMenu.connectServer",
		"getMenu.grpcServer",
		"getMenu.mcpServer",
	} {
		run("op", "invoke", base, "--binding", binding)
	}
	run("op", "invoke", base, "--binding", "getMenu.graphqlServer",
		"--configuration", `{"document":"query { getMenu { items { name description category sizes { id label price } } } }"}`)

	// Place an order (the demo's printed input, verbatim).
	out := run("op", "invoke", base, "--binding", "placeOrder.restApi",
		"--input", `{"drink":"Schema Latte","size":"v2","customer":"Alice"}`)
	if !strings.Contains(out, "orderId") {
		t.Errorf("placeOrder output missing orderId: %s", out)
	}

	// Watch it progress: orderUpdates streams; expect at least one decoded
	// JSON event, then tear the stream down.
	streamCtx, stopStream := context.WithTimeout(ctx, 20*time.Second)
	defer stopStream()
	stream := exec.CommandContext(streamCtx, ob, "op", "invoke", base, "--binding", "orderUpdates.grpcServer")
	stream.Dir = workDir
	var streamStderr bytes.Buffer
	stream.Stderr = &streamStderr
	stdout, err := stream.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Start(); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				events <- line
				return
			}
		}
	}()
	// The subscription needs a beat to attach before the order fires events.
	time.Sleep(500 * time.Millisecond)
	run("op", "invoke", base, "--binding", "placeOrder.restApi",
		"--input", `{"drink":"Schema Latte","size":"v2","customer":"Bob"}`)

	select {
	case line := <-events:
		var ev map[string]any
		if uerr := json.Unmarshal([]byte(line), &ev); uerr != nil {
			t.Fatalf("orderUpdates event is not a JSON object (decode lane drift?): %q", line)
		}
		if ev["orderId"] == nil {
			t.Errorf("orderUpdates event missing orderId: %q", line)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("no orderUpdates event within 15s of placing an order; stream stderr: %s", streamStderr.String())
	}
	stopStream()
	_ = stream.Wait() // killed by ctx; the assertion above is the verdict
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("demo server not ready at %s", url)
}
