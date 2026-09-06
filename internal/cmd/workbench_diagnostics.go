package cmd

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/openbindings/openbindings-go/invoke"
)

// This authenticated, bounded workbench side channel is not an invoker frame,
// portable error extension, or an OpenAPI facility. It only exposes the SDK's
// value-free operation-contract diagnostics, never native errors or responses.
type workbenchDiagnostics struct {
	mu      sync.Mutex
	entries map[string]workbenchDiagnosticEntry
}
type workbenchDiagnosticEntry struct {
	at        time.Time
	collector *invoke.DiagnosticCollector
}
type diagnosticContextKey struct{}

var diagnosticIDPattern = regexp.MustCompile(`^[a-f0-9-]{36}$`)

func (d *workbenchDiagnostics) begin(id string) *invoke.DiagnosticCollector {
	if !diagnosticIDPattern.MatchString(id) {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.entries == nil {
		d.entries = map[string]workbenchDiagnosticEntry{}
	}
	for key, entry := range d.entries {
		if time.Since(entry.at) > 2*time.Minute {
			delete(d.entries, key)
		}
	}
	if _, exists := d.entries[id]; exists || len(d.entries) >= 128 {
		return nil
	}
	collector := invoke.NewDiagnosticCollector(8)
	d.entries[id] = workbenchDiagnosticEntry{time.Now(), collector}
	return collector
}

func (d *workbenchDiagnostics) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	d.mu.Lock()
	entry, ok := d.entries[r.PathValue("id")]
	delete(d.entries, r.PathValue("id"))
	d.mu.Unlock()
	if !ok || time.Since(entry.at) > 2*time.Minute {
		http.NotFound(w, r)
		return
	}
	records, _ := entry.collector.Snapshot()
	// No arbitrary operation/binding keys or schema URLs leave this channel.
	type record struct {
		Phase   invoke.ValidationPhase `json:"phase"`
		Pointer string                 `json:"pointer"`
		Keyword string                 `json:"keyword"`
	}
	out := make([]record, 0, len(records))
	for _, item := range records {
		out = append(out, record{item.Phase, item.InstancePointer, item.Keyword})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (d *workbenchDiagnostics) finish(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if entry, ok := d.entries[id]; ok {
		if records, _ := entry.collector.Snapshot(); len(records) == 0 {
			delete(d.entries, id)
		}
	}
}
