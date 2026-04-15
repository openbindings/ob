// Package delegates - probe.go contains delegate probing logic.
package delegates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/openbindings/ob/internal/execref"
	"github.com/openbindings/openbindings-go"
)

// ErrCommandTimeout indicates a command timed out.
var ErrCommandTimeout = errors.New("command timeout")

// ProbeFormats fetches the supported formats from a delegate
// by running its listFormats operation.
func ProbeFormats(path string, timeout time.Duration) ([]string, error) {
	if IsHTTPURL(path) {
		return nil, fmt.Errorf("formats require executor")
	}
	if IsExecURL(path) {
		cmd, err := execref.RootCommand(path)
		if err != nil {
			return nil, err
		}
		iface, err := RunCLIOpenBindings(cmd, timeout)
		if err != nil {
			return nil, err
		}
		return probeFormatsFromInterface(cmd, timeout, iface)
	}

	iface, err := RunCLIOpenBindings(path, timeout)
	if err != nil {
		return nil, err
	}
	return probeFormatsFromInterface(path, timeout, iface)
}

// RunCLIOpenBindings runs "<path> --openbindings" and parses the result.
func RunCLIOpenBindings(path string, timeout time.Duration) (openbindings.Interface, error) {
	stdout, stderr, err := RunCLI(path, []string{"--openbindings"}, timeout)
	if err != nil {
		if errors.Is(err, ErrCommandTimeout) {
			return openbindings.Interface{}, fmt.Errorf("openbindings timeout: %w", ErrCommandTimeout)
		}
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return openbindings.Interface{}, fmt.Errorf("openbindings command failed: %s", msg)
	}
	var iface openbindings.Interface
	if err := json.Unmarshal([]byte(stdout), &iface); err != nil {
		return openbindings.Interface{}, fmt.Errorf("invalid openbindings JSON: %w", err)
	}
	return iface, nil
}

// probeFormatsFromInterface executes the listFormats binding from an interface.
func probeFormatsFromInterface(path string, timeout time.Duration, iface openbindings.Interface) ([]string, error) {
	var (
		formatsRef string
		sourceKey  string
	)
	for _, b := range iface.Bindings {
		if b.Operation == OpListFormats {
			formatsRef = b.Ref
			sourceKey = b.Source
			break
		}
	}
	if formatsRef == "" || sourceKey == "" {
		return nil, fmt.Errorf("missing binding for %s", OpListFormats)
	}
	src, ok := iface.Sources[sourceKey]
	if !ok {
		return nil, fmt.Errorf("binding source not found for %s", OpListFormats)
	}
	if !strings.HasPrefix(src.Format, "usage@") {
		return nil, fmt.Errorf("unsupported binding format for %s", OpListFormats)
	}

	args := strings.Fields(formatsRef)
	if len(args) == 0 {
		return nil, fmt.Errorf("empty formats ref")
	}

	// Try JSON output first for structured parsing.
	jsonArgs := append(args, "-F", "json")
	stdout, stderr, err := RunCLI(path, jsonArgs, timeout)
	if err != nil {
		if errors.Is(err, ErrCommandTimeout) {
			return nil, fmt.Errorf("formats command timeout: %w", ErrCommandTimeout)
		}
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("formats command failed: %s", msg)
	}

	// Parse JSON array of {token: string} objects.
	var entries []struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(stdout), &entries); err == nil && len(entries) > 0 {
		var out []string
		for _, e := range entries {
			tok := strings.TrimSpace(e.Token)
			if tok != "" {
				out = append(out, tok)
			}
		}
		sort.Strings(out)
		return out, nil
	}

	// Fallback: plain text, one token per line.
	lines := strings.Split(stdout, "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out, nil
}

// RunCLI executes a CLI command with timeout and returns stdout, stderr, and error.
func RunCLI(command string, args []string, timeout time.Duration) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if ctx.Err() == context.DeadlineExceeded {
		return stdout, stderr, ErrCommandTimeout
	}
	return stdout, stderr, err
}
