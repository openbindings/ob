package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

const delegateMigrationFormat = "ob.delegate-migration@1"
const maxDelegateMigrationBytes = 8 << 20

type DelegateMigrationEntry struct {
	Index          int             `json:"index"`
	Original       json.RawMessage `json:"original"`
	Disposition    string          `json:"disposition"`
	Review         string          `json:"review,omitempty"`
	ProviderReview string          `json:"providerReview,omitempty"`
	Interface      json.RawMessage `json:"interface,omitempty"`
	Roles          []string        `json:"roles,omitempty"`
	Archive        []string        `json:"archive,omitempty"`
}

type DelegateMigrationPlan struct {
	Format       string                   `json:"format"`
	Environment  string                   `json:"environment"`
	SourceHash   string                   `json:"sourceHash"`
	TargetFormat string                   `json:"targetFormat"`
	Catalogue    string                   `json:"catalogue"`
	Entries      []DelegateMigrationEntry `json:"entries"`
}

type DelegateMigrationReceipt struct {
	PlanHash      string   `json:"planHash"`
	SourceHash    string   `json:"sourceHash"`
	CommitHash    string   `json:"commitHash"`
	Revision      uint64   `json:"revision"`
	Registrations []string `json:"registrations"` // original row order; empty = excluded
}

// readMigrationFile never follows symlinks or accepts unbounded input. Provider
// locators inside a file are inert data, not instructions to retrieve anything.
func readMigrationFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("migration input must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("migration input changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxDelegateMigrationBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxDelegateMigrationBytes {
		return nil, errors.New("migration input exceeds OB capacity")
	}
	return raw, nil
}

func legacyMigrationRows(raw []byte) (map[string]json.RawMessage, []json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, nil, errors.New("invalid legacy environment document")
	}
	if _, exists := fields["delegateRegistry"]; exists {
		return nil, nil, errors.New("environment already has a role registry or an unsupported registry marker")
	}
	rows := []json.RawMessage{}
	if value, exists := fields["delegates"]; exists {
		if err := jsonvalue.Unmarshal(value, &rows); err != nil || rows == nil {
			return nil, nil, errors.New("legacy delegates must be an array")
		}
	}
	return fields, rows, nil
}

func (r *roleRegistry) migrationPreview() (*DelegateMigrationPlan, error) {
	path, err := filepath.Abs(r.path)
	if err != nil {
		return nil, err
	}
	raw, err := readMigrationFile(filepath.Join(path, EnvConfigFile))
	if err != nil {
		return nil, err
	}
	_, rows, err := legacyMigrationRows(raw)
	if err != nil {
		return nil, err
	}
	plan := &DelegateMigrationPlan{Format: delegateMigrationFormat, Environment: path, SourceHash: HashContent(raw), TargetFormat: roleRegistryFormat, Catalogue: r.catalogue.identity, Entries: []DelegateMigrationEntry{}}
	for i, row := range rows {
		plan.Entries = append(plan.Entries, DelegateMigrationEntry{Index: i, Original: row, Disposition: "unresolved"})
	}
	return plan, nil
}

func archivePath(parts ...string) string {
	for i, part := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
	}
	return "/" + strings.Join(parts, "/")
}

// migratedPreferences uses exact original numeric values. No legacy float64
// decoder, capability inference or arbitrary per-operation guess participates.
func migratedPreferences(entry DelegateMigrationEntry) (map[string]json.Number, map[string]map[string]json.Number, error) {
	var old map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(entry.Original, &old); err != nil || old == nil {
		return nil, nil, errors.New("legacy row must be an object")
	}
	for _, retired := range []string{"formats", "formatPreferences"} {
		if _, has := old[retired]; has {
			return nil, nil, errors.New("retired registry row needs explicit exclusion and separate recovery")
		}
	}
	archived := map[string]bool{}
	for _, path := range entry.Archive {
		if archived[path] {
			return nil, nil, errors.New("duplicate archival disposition")
		}
		archived[path] = true
	}
	used := map[string]bool{}
	archive := func(path string) bool {
		if archived[path] {
			used[path] = true
			return true
		}
		return false
	}
	for key := range old {
		if !slices.Contains([]string{"location", "name", "operations", "contentHash", "capabilities", "bindingSpecs", "preference", "operationPreferences", "bindingSpecPreferences"}, key) && !archive(archivePath(key)) {
			return nil, nil, errors.New("unresolved legacy field")
		}
	}
	rolesByOperation := map[string]string{}
	for _, role := range entry.Roles {
		op, known := capabilityOperation[DelegateCapability(role)]
		if !known {
			return nil, nil, errors.New("legacy preference mapping is undefined for requested role")
		}
		rolesByOperation[op] = role
	}
	prefs := map[string]json.Number{}
	native := map[string]map[string]json.Number{}
	decodeNumber := func(raw json.RawMessage) (json.Number, error) {
		var value any
		if err := jsonvalue.Unmarshal(raw, &value); err != nil {
			return "", errors.New("invalid legacy number")
		}
		n, ok := value.(json.Number)
		if !ok || !validPreference(n) {
			return "", errors.New("legacy preference is not a supported exact number")
		}
		return n, nil
	}
	if raw, exists := old["preference"]; exists && !archive("/preference") {
		n, err := decodeNumber(raw)
		if err != nil {
			return nil, nil, err
		}
		for _, role := range entry.Roles {
			prefs[role] = n
		}
	}
	if raw, exists := old["operationPreferences"]; exists && !archive("/operationPreferences") {
		var operations map[string]json.RawMessage
		if err := jsonvalue.Unmarshal(raw, &operations); err != nil || operations == nil {
			return nil, nil, errors.New("invalid old operation preferences")
		}
		for op, value := range operations {
			if archive(archivePath("operationPreferences", op)) {
				continue
			}
			role, known := rolesByOperation[op]
			if !known {
				return nil, nil, errors.New("unmapped operation preference needs explicit archival disposition")
			}
			n, err := decodeNumber(value)
			if err != nil {
				return nil, nil, err
			}
			prefs[role] = n
		}
	}
	if raw, exists := old["bindingSpecPreferences"]; exists && !archive("/bindingSpecPreferences") {
		var entries []json.RawMessage
		if err := jsonvalue.Unmarshal(raw, &entries); err != nil || entries == nil {
			return nil, nil, errors.New("invalid old binding preferences")
		}
		for i, value := range entries {
			if archive(archivePath("bindingSpecPreferences", strconv.Itoa(i))) {
				continue
			}
			var fields map[string]json.RawMessage
			if err := jsonvalue.Unmarshal(value, &fields); err != nil || len(fields) != 3 {
				return nil, nil, errors.New("ambiguous old binding preference")
			}
			var op, spec string
			if jsonvalue.Unmarshal(fields["operation"], &op) != nil || jsonvalue.Unmarshal(fields["bindingSpec"], &spec) != nil || spec == "" {
				return nil, nil, errors.New("invalid old binding preference")
			}
			role, known := rolesByOperation[op]
			if !known {
				return nil, nil, errors.New("unmapped binding preference needs explicit archival disposition")
			}
			n, err := decodeNumber(fields["preference"])
			if err != nil {
				return nil, nil, err
			}
			if native[role] == nil {
				native[role] = map[string]json.Number{}
			}
			if _, exists := native[role][spec]; exists {
				return nil, nil, errors.New("duplicate binding preference needs explicit archival disposition")
			}
			native[role][spec] = n
		}
	}
	for path := range archived {
		if !used[path] {
			return nil, nil, errors.New("archival disposition does not name a discarded legacy field")
		}
	}
	return prefs, native, nil
}

func (r *roleRegistry) prepareMigration(plan *DelegateMigrationPlan, source []byte) (*EnvConfig, *DelegateMigrationReceipt, error) {
	path, err := filepath.Abs(r.path)
	if err != nil {
		return nil, nil, err
	}
	if plan.Format != delegateMigrationFormat || plan.TargetFormat != roleRegistryFormat || plan.Environment != path || plan.Catalogue != r.catalogue.identity || plan.SourceHash != HashContent(source) {
		return nil, nil, errors.New("migration plan is stale or targets another environment/catalogue")
	}
	fields, rows, err := legacyMigrationRows(source)
	if err != nil {
		return nil, nil, err
	}
	if len(plan.Entries) != len(rows) {
		return nil, nil, errors.New("every original row needs a disposition")
	}
	state, err := newRoleState(r.catalogue.identity)
	if err != nil {
		return nil, nil, err
	}
	encodedPlan, err := jsonvalue.Marshal(plan)
	if err != nil || len(encodedPlan) > maxDelegateMigrationBytes {
		return nil, nil, errors.New("migration plan exceeds OB capacity")
	}
	receipt := &DelegateMigrationReceipt{PlanHash: HashContent(encodedPlan), SourceHash: plan.SourceHash, Revision: 1, Registrations: make([]string, len(rows))}
	for i, entry := range plan.Entries {
		// Original-row bytes are carried through the SDK's raw JSON codec; a
		// semantic equality check permits harmless whitespace, never changed data.
		var original, supplied any
		if jsonvalue.Unmarshal(rows[i], &original) != nil || jsonvalue.Unmarshal(entry.Original, &supplied) != nil {
			return nil, nil, errors.New("invalid original row")
		}
		equal, equalityErr := jsonvalue.Equal(original, supplied)
		if entry.Index != i || equalityErr != nil || !equal {
			return nil, nil, errors.New("migration row identity/order changed")
		}
		if strings.TrimSpace(entry.Review) == "" {
			return nil, nil, errors.New("every disposition requires an explicit review explanation")
		}
		if entry.Disposition == "exclude" {
			continue
		}
		if entry.Disposition != "convert" {
			return nil, nil, errors.New("migration contains unresolved rows")
		}
		if _, err := r.catalogue.admit(entry.Interface, entry.Roles); err != nil {
			return nil, nil, err
		}
		var legacy struct {
			Location    string `json:"location"`
			ContentHash string `json:"contentHash"`
		}
		if jsonvalue.Unmarshal(entry.Original, &legacy) != nil || legacy.Location == "" {
			return nil, nil, errors.New("invalid old registration identity")
		}
		provider, err := openbindings.ValidateDocument(entry.Interface)
		if err != nil {
			return nil, nil, errors.New("invalid recovered interface")
		}
		hash, hashErr := interfaceContentHash(provider)
		if (legacy.ContentHash == "" || hashErr != nil || hash != legacy.ContentHash) && strings.TrimSpace(entry.ProviderReview) == "" {
			return nil, nil, errors.New("missing/changed provider pin requires explicit provider review")
		}
		prefs, native, err := migratedPreferences(entry)
		if err != nil {
			return nil, nil, err
		}
		seq := strconv.Itoa(len(state.Records) + 1)
		id := "dlg_" + state.Issuance + "_" + seq
		state.Records = append(state.Records, DelegateRegistration{ID: id, Interface: entry.Interface, Roles: entry.Roles, RolePreferences: prefs})
		if len(native) > 0 {
			state.BindingPreferences[id] = native
		}
		receipt.Registrations[i] = id
	}
	state.Next = strconv.Itoa(len(state.Records) + 1)
	state.MigrationReceipt, err = jsonvalue.Marshal(receipt)
	if err != nil {
		return nil, nil, err
	}
	delete(fields, "delegates")
	remaining, err := jsonvalue.Marshal(fields)
	if err != nil {
		return nil, nil, err
	}
	var config EnvConfig
	if err := jsonvalue.Unmarshal(remaining, &config); err != nil {
		return nil, nil, errors.New("unrelated configuration cannot be preserved")
	}
	if err := retainRoleState(&config, state); err != nil {
		return nil, nil, err
	}
	receipt.CommitHash, err = migrationStateHash(&config)
	if err != nil {
		return nil, nil, err
	}
	state.MigrationReceipt, err = jsonvalue.Marshal(receipt)
	if err != nil {
		return nil, nil, err
	}
	config.DelegateRegistry, err = jsonvalue.Marshal(state)
	if err != nil {
		return nil, nil, err
	}
	if _, err := readRoleState(&config); err != nil {
		return nil, nil, err
	}
	return &config, receipt, nil
}

// Exclude the receipt itself to avoid a recursive digest. Cover ALL other
// environment fields, not merely the registry revision, for guarded rollback.
func migrationStateHash(config *EnvConfig) (string, error) {
	raw, err := jsonvalue.Marshal(config)
	if err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	var registry map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(fields["delegateRegistry"], &registry); err != nil {
		return "", err
	}
	delete(registry, "migrationReceipt")
	fields["delegateRegistry"], err = jsonvalue.Marshal(registry)
	if err != nil {
		return "", err
	}
	raw, err = jsonvalue.Marshal(fields)
	if err != nil {
		return "", err
	}
	return HashContent(raw), nil
}

// Isolated crash-injection seam; not configurable by CLI, HTTP or environment.
var migrationBoundary = func(stage string) error { return nil }

func migrationBackupPath(envPath, planHash string) (string, error) {
	digest := strings.TrimPrefix(planHash, "sha256:")
	if digest == planHash || len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return "", errors.New("invalid migration identity")
	}
	return filepath.Join(envPath, ".delegate-migrations", digest), nil
}

func migrationDirectory(envPath, planHash string) (string, error) {
	target, err := migrationBackupPath(envPath, planHash)
	if err != nil {
		return "", err
	}
	path := envPath
	for _, component := range []string{".delegate-migrations", filepath.Base(target)} {
		path = filepath.Join(path, component)
		if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return "", err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("migration backup path must be a real directory")
		}
	}
	return path, nil
}

func publishMigrationFile(directory, name string, data []byte) error {
	path := filepath.Join(directory, name)
	if existing, err := readMigrationFile(path); err == nil {
		if !bytes.Equal(existing, data) {
			return errors.New("existing migration backup disagrees with plan")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if private, err := envFileIsPrivate(f); err != nil || !private {
			return errors.New("existing migration backup is not owner-only")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(directory, ".migration-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := restrictEnvFile(f); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := migrationBoundary(name + ":write"); err != nil {
		return err
	}
	if err := envCommitSync(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := envCommitReplace(f.Name(), path); err != nil {
		return err
	}
	return migrationBoundary(name + ":published")
}

func (r *roleRegistry) migrationApply(plan *DelegateMigrationPlan, quiesced bool) (*DelegateMigrationReceipt, error) {
	if !quiesced {
		return nil, errors.New("stop incompatible writers and explicitly confirm quiescence before migration")
	}
	if plan == nil {
		return nil, errors.New("migration plan is required")
	}
	planBytes, err := jsonvalue.Marshal(plan)
	if err != nil || len(planBytes) > maxDelegateMigrationBytes {
		return nil, errors.New("invalid or oversized migration plan")
	}
	// Detach a caller-owned plan before transaction validation and publication.
	var snapshot DelegateMigrationPlan
	if err := jsonvalue.Unmarshal(planBytes, &snapshot); err != nil {
		return nil, err
	}
	selected, err := filepath.Abs(r.path)
	if err != nil {
		return nil, err
	}
	if snapshot.Environment != selected || snapshot.Format != delegateMigrationFormat || snapshot.TargetFormat != roleRegistryFormat {
		return nil, errors.New("migration plan targets another environment or unsupported format")
	}
	return withEnvConfigLock(r.path, func() (*DelegateMigrationReceipt, error) {
		source, err := readMigrationFile(filepath.Join(r.path, EnvConfigFile))
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if err := jsonvalue.Unmarshal(source, &fields); err != nil {
			return nil, errors.New("invalid environment state")
		}
		if _, present := fields["delegateRegistry"]; present {
			var current EnvConfig
			if err := jsonvalue.Unmarshal(source, &current); err != nil {
				return nil, errors.New("invalid or mixed environment state")
			}
			state, err := readRoleState(&current)
			if err != nil {
				return nil, err
			}
			var receipt DelegateMigrationReceipt
			if state == nil || jsonvalue.Unmarshal(state.MigrationReceipt, &receipt) != nil || receipt.PlanHash != HashContent(planBytes) || receipt.SourceHash != snapshot.SourceHash {
				return nil, errors.New("environment does not match this migration receipt")
			}
			return &receipt, nil
		}
		config, receipt, err := r.prepareMigration(&snapshot, source)
		if err != nil {
			return nil, err
		}
		directory, err := migrationDirectory(r.path, receipt.PlanHash)
		if err != nil {
			return nil, err
		}
		if err := publishMigrationFile(directory, "original.json", source); err != nil {
			return nil, err
		}
		if err := publishMigrationFile(directory, "plan.json", planBytes); err != nil {
			return nil, err
		}
		if err := migrationBoundary("before-registry"); err != nil {
			return nil, err
		}
		if err := writeEnvConfigLocked(r.path, config); err != nil {
			return nil, err
		}
		if err := migrationBoundary("after-registry"); err != nil {
			return nil, fmt.Errorf("migration may have completed; inspect its receipt before retry: %w", err)
		}
		return receipt, nil
	})
}

func (r *roleRegistry) migrationRollback(planHash string, quiesced bool) error {
	if !quiesced {
		return errors.New("stop incompatible writers and explicitly confirm quiescence before rollback")
	}
	_, err := withEnvConfigLock(r.path, func() (struct{}, error) {
		current, err := readMigrationFile(filepath.Join(r.path, EnvConfigFile))
		if err != nil {
			return struct{}{}, err
		}
		directory, err := migrationBackupPath(r.path, planHash)
		if err != nil {
			return struct{}{}, err
		}
		original, err := readMigrationFile(filepath.Join(directory, "original.json"))
		if err != nil {
			return struct{}{}, err
		}
		plan, err := readMigrationFile(filepath.Join(directory, "plan.json"))
		if err != nil {
			return struct{}{}, err
		}
		var reviewed DelegateMigrationPlan
		selected, pathErr := filepath.Abs(r.path)
		if pathErr != nil || jsonvalue.Unmarshal(plan, &reviewed) != nil || reviewed.Environment != selected || reviewed.SourceHash != HashContent(original) || HashContent(plan) != planHash {
			return struct{}{}, errors.New("migration backup verification failed")
		}
		if bytes.Equal(current, original) {
			return struct{}{}, nil
		} // recovered completed rollback
		var config EnvConfig
		if jsonvalue.Unmarshal(current, &config) != nil {
			return struct{}{}, errors.New("invalid current environment")
		}
		state, err := readRoleState(&config)
		if err != nil || state == nil {
			return struct{}{}, errors.New("no role registry to roll back")
		}
		var receipt DelegateMigrationReceipt
		if jsonvalue.Unmarshal(state.MigrationReceipt, &receipt) != nil || receipt.PlanHash != planHash || receipt.Revision == 0 {
			return struct{}{}, errors.New("rollback does not match the applied migration")
		}
		// Resolve only our fixed hash-derived backup names, never an arbitrary
		// path from a plan or receipt supplied by a client.
		if HashContent(original) != receipt.SourceHash || HashContent(plan) != planHash {
			return struct{}{}, errors.New("migration backup verification failed")
		}
		if _, _, err := legacyMigrationRows(original); err != nil {
			return struct{}{}, err
		}
		if err := publishMigrationFile(directory, "current-"+strings.TrimPrefix(HashContent(current), "sha256:")+".json", current); err != nil {
			return struct{}{}, err
		}
		commitHash, err := migrationStateHash(&config)
		if err != nil {
			return struct{}{}, err
		}
		if state.Revision != receipt.Revision || commitHash != receipt.CommitHash {
			return struct{}{}, errors.New("newer registry edits were preserved in recovery backup; explicit direction is required before rollback")
		}
		if err := migrationBoundary("before-rollback"); err != nil {
			return struct{}{}, err
		}
		if err := writeEnvConfigBytesLocked(r.path, original); err != nil {
			return struct{}{}, err
		}
		if err := migrationBoundary("after-rollback"); err != nil {
			return struct{}{}, fmt.Errorf("rollback may have completed; inspect current state: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}
