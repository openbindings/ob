package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openbindings/openbindings-go/jsonvalue"
)

// Native conversion facility for the retired location-based registry. It is a
// CLI operator facility over the explicitly selected environment (the same
// discovery every other command uses), not a shared manager operation and not
// a remote administration API. Preview inventories; apply and rollback require
// the operator's explicit quiescence acknowledgment.

// PreviewDelegateMigration inventories every legacy row and preference of the
// active environment with a source fingerprint. It performs no registry,
// configuration or credential writes, no network and no executables.
func PreviewDelegateMigration() (*DelegateMigrationPlan, error) {
	registry, err := requireActiveRoleRegistry()
	if err != nil {
		return nil, err
	}
	return registry.migrationPreview()
}

// ApplyDelegateMigration validates a reviewed plan losslessly against the
// current environment and commits the whole conversion once under the common
// lock, with owner-only backups and a recoverable receipt. A repeated apply of
// the same plan reports the recorded completion instead of converting twice.
func ApplyDelegateMigration(plan *DelegateMigrationPlan, confirmQuiesced bool) (*DelegateMigrationReceipt, error) {
	registry, err := requireActiveRoleRegistry()
	if err != nil {
		return nil, err
	}
	return registry.migrationApply(plan, confirmQuiesced)
}

// RollbackDelegateMigration restores the exact original configuration of a
// completed conversion when the current state still matches its receipt.
// Newer edits are preserved in a recovery copy and refused, never overwritten.
func RollbackDelegateMigration(planHash string, confirmQuiesced bool) error {
	registry, err := requireActiveRoleRegistry()
	if err != nil {
		return err
	}
	return registry.migrationRollback(planHash, confirmQuiesced)
}

// DecodeMigrationPlan reads a reviewed plan losslessly: unknown top-level or
// entry fields are refused rather than dropped, because dropping them would
// silently change the identity of the plan the operator reviewed.
func DecodeMigrationPlan(data []byte) (*DelegateMigrationPlan, error) {
	if len(data) > maxDelegateMigrationBytes {
		return nil, errors.New("migration plan exceeds OB capacity")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var plan DelegateMigrationPlan
	if err := decoder.Decode(&plan); err != nil {
		return nil, fmt.Errorf("invalid migration plan: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, errors.New("invalid migration plan: trailing content after the plan document")
	}
	if plan.Format != delegateMigrationFormat {
		return nil, fmt.Errorf("invalid migration plan: unsupported format %q", plan.Format)
	}
	return &plan, nil
}

// ReadMigrationPlanFile reads a reviewed plan from a regular file.
func ReadMigrationPlanFile(path string) (*DelegateMigrationPlan, error) {
	data, err := readMigrationFile(path)
	if err != nil {
		return nil, fmt.Errorf("read migration plan %s: %v", path, err)
	}
	return DecodeMigrationPlan(data)
}

// WriteMigrationPlanFile exports a plan as an owner-only new file. An existing
// path, symlink or directory is refused: plans can carry sensitive provider
// documents and must never overwrite operator files.
func WriteMigrationPlanFile(path string, plan *DelegateMigrationPlan) error {
	data, err := jsonvalue.Marshal(plan)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		kind := "file"
		if info.Mode()&os.ModeSymlink != 0 {
			kind = "symlink"
		} else if info.IsDir() {
			kind = "directory"
		}
		return fmt.Errorf("refusing to write plan: %s already exists (%s)", path, kind)
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("write plan: %v", err)
	}
	if err := restrictEnvFile(f); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// PlanHash is the identity apply records and rollback names.
func (p *DelegateMigrationPlan) PlanHash() (string, error) {
	data, err := jsonvalue.Marshal(p)
	if err != nil {
		return "", err
	}
	return HashContent(data), nil
}

// Render summarizes without printing retained provider documents.
func (p DelegateMigrationPlan) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Delegate migration preview"))
	fmt.Fprintf(&sb, "\n  %s%s", s.Dim.Render("environment: "), p.Environment)
	fmt.Fprintf(&sb, "\n  %s%s", s.Dim.Render("source fingerprint: "), p.SourceHash)
	fmt.Fprintf(&sb, "\n  %s%s (catalogue %s)", s.Dim.Render("target: "), p.TargetFormat, p.Catalogue)
	counts := map[string]int{}
	for _, entry := range p.Entries {
		counts[entry.Disposition]++
	}
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	parts := make([]string, len(kinds))
	for i, kind := range kinds {
		parts[i] = fmt.Sprintf("%d %s", counts[kind], kind)
	}
	if len(parts) == 0 {
		parts = []string{"no legacy rows"}
	}
	fmt.Fprintf(&sb, "\n  %s%d (%s)", s.Dim.Render("legacy rows: "), len(p.Entries), strings.Join(parts, ", "))
	for _, entry := range p.Entries {
		var legacy struct {
			Location string `json:"location"`
		}
		_ = json.Unmarshal(entry.Original, &legacy)
		fmt.Fprintf(&sb, "\n    %d. %s — %s", entry.Index, legacy.Location, entry.Disposition)
	}
	sb.WriteString("\n  " + s.Dim.Render("Review every row (disposition, roles, recovered interface), then apply with --confirm-quiesced."))
	return sb.String()
}

// Render summarizes the applied receipt.
func (r DelegateMigrationReceipt) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Delegate migration applied"))
	fmt.Fprintf(&sb, "\n  %s%s", s.Dim.Render("plan: "), r.PlanHash)
	fmt.Fprintf(&sb, "\n  %s%s", s.Dim.Render("source fingerprint: "), r.SourceHash)
	fmt.Fprintf(&sb, "\n  %s%d", s.Dim.Render("registry revision: "), r.Revision)
	for i, id := range r.Registrations {
		if id == "" {
			fmt.Fprintf(&sb, "\n    row %d: excluded (kept in the original backup)", i)
		} else {
			fmt.Fprintf(&sb, "\n    row %d: %s", i, id)
		}
	}
	sb.WriteString("\n  " + s.Dim.Render("Backups live under .delegate-migrations/<plan digest>/ in the environment; roll back with the plan identity above."))
	return sb.String()
}

// migrationBackupRelative names the private backup directory for guidance.
func migrationBackupRelative(planHash string) string {
	return filepath.Join(".delegate-migrations", strings.TrimPrefix(planHash, "sha256:"))
}
