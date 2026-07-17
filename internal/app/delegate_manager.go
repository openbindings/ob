package app

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	openbindings "github.com/openbindings/openbindings-go"
	"github.com/openbindings/openbindings-go/canonicaljson"

	"github.com/openbindings/ob/internal/delegates"
)

// This file is ob's realization of the published delegate-manager interface:
// registerDelegate / unregisterDelegate / listDelegates / resolveDelegate /
// setDelegatePreference, satisfied by alias from ob's own operations. The
// contract's semantics are load-bearing here:
//
//   - registration FAILS on an unresolvable location (a delegate is its OBI);
//   - what is recorded is a SNAPSHOT, pinned by a content digest;
//   - re-registering refreshes the snapshot and never the preferences
//     (the snapshot is the delegate's data, the preferences the registrar's);
//   - resolveDelegate matches per operation and orders by effective preference;
//   - preference orders candidates, it never selects — ob's routing narrows
//     further (format support) as its own application policy.

// DelegateSummary is a registered delegate: its identity, the snapshot of what
// it carries, ob's derived routing data, and its selection preferences. It is
// the wire shape of the contract's DelegateSummary plus ob's extras.
type DelegateSummary struct {
	Name                 string                    `json:"name,omitempty"`
	Location             string                    `json:"location"`
	Operations           []string                  `json:"operations"`
	ContentHash          string                    `json:"contentHash,omitempty"`
	Capabilities         []DelegateCapability      `json:"capabilities,omitempty"`
	BindingSpecs         []DelegateBindingSpecInfo `json:"bindingSpecs,omitempty"`
	Preference           *float64                  `json:"preference,omitempty"`
	OperationPreferences map[string]float64        `json:"operationPreferences,omitempty"`
	// BindingSpecPreferences is ob's extra granularity beyond the delegate-manager
	// contract's per-operation index: a preference scoped to one (operation,
	// format) pair, overriding both the delegate-level and the per-operation
	// value when resolving that operation for that binding-source format.
	BindingSpecPreferences []BindingSpecPreference `json:"bindingSpecPreferences,omitempty"`
	Builtin                bool                    `json:"builtin,omitempty"`
}

func summaryFromRecord(rec DelegateRecord) DelegateSummary {
	return DelegateSummary{
		Name:                   rec.Name,
		Location:               rec.Location,
		Operations:             rec.Operations,
		ContentHash:            rec.ContentHash,
		Capabilities:           rec.Capabilities,
		BindingSpecs:           rec.BindingSpecs,
		Preference:             rec.Preference,
		OperationPreferences:   rec.OperationPreferences,
		BindingSpecPreferences: rec.BindingSpecPreferences,
	}
}

// Render returns a human-readable summary (used by registration and
// preference updates).
func (d DelegateSummary) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Delegate"))
	sb.WriteString(" ")
	sb.WriteString(s.Key.Render(d.Name))
	if d.Location != "" && d.Location != d.Name {
		sb.WriteString(s.Dim.Render(" " + d.Location))
	}
	if d.Builtin {
		sb.WriteString(s.Dim.Render(" (builtin)"))
	}

	fmt.Fprintf(&sb, "\n  %s%d", s.Dim.Render("operations: "), len(d.Operations))
	if d.ContentHash != "" {
		sb.WriteString("\n  ")
		sb.WriteString(s.Dim.Render("pinned: " + d.ContentHash))
	}

	if len(d.Capabilities) > 0 {
		caps := make([]string, len(d.Capabilities))
		for i, c := range d.Capabilities {
			caps[i] = string(c)
		}
		sb.WriteString("\n  ")
		sb.WriteString(s.Dim.Render("capabilities: "))
		sb.WriteString(strings.Join(caps, ", "))
	} else if !d.Builtin {
		sb.WriteString("\n  ")
		sb.WriteString(s.Warning.Render("! inert for ob — carries none of invoke/synthesize/inspect (other software may still use it)"))
	}

	if len(d.BindingSpecs) > 0 {
		toks := make([]string, len(d.BindingSpecs))
		for i, f := range d.BindingSpecs {
			toks[i] = f.BindingSpec
		}
		sb.WriteString("\n  ")
		sb.WriteString(s.Dim.Render("formats: "))
		sb.WriteString(strings.Join(toks, ", "))
	}
	if d.Preference != nil {
		fmt.Fprintf(&sb, "\n  %s%g", s.Dim.Render("preference: "), *d.Preference)
	}
	prefOps := make([]string, 0, len(d.OperationPreferences))
	for op := range d.OperationPreferences {
		prefOps = append(prefOps, op)
	}
	sort.Strings(prefOps)
	for _, op := range prefOps {
		fmt.Fprintf(&sb, "\n  %s%s = %g", s.Dim.Render("preference "), op, d.OperationPreferences[op])
	}
	specPrefs := append([]BindingSpecPreference(nil), d.BindingSpecPreferences...)
	sort.Slice(specPrefs, func(i, j int) bool {
		if specPrefs[i].Operation != specPrefs[j].Operation {
			return specPrefs[i].Operation < specPrefs[j].Operation
		}
		return specPrefs[i].BindingSpec < specPrefs[j].BindingSpec
	})
	for _, fp := range specPrefs {
		fmt.Fprintf(&sb, "\n  %s%s (%s) = %g", s.Dim.Render("preference "), fp.Operation, fp.BindingSpec, fp.Preference)
	}
	return sb.String()
}

// --- The self-delegate ---

var (
	selfRecordOnce sync.Once
	selfRecord     DelegateRecord
)

// selfDelegateRecord is ob's own native handling in registry shape: location
// "ob", every operation ob's embedded interface answers to, all three format
// capabilities, and the native formats. It is synthesized, never persisted.
func selfDelegateRecord() DelegateRecord {
	selfRecordOnce.Do(func() {
		selfRecord = DelegateRecord{
			Location:     SelfDelegateLocation,
			Name:         "ob",
			Operations:   []string{},
			Capabilities: []DelegateCapability{CapInvoke, CapSynthesize, CapInspect},
		}
		for _, tok := range getNativeTokens() {
			selfRecord.BindingSpecs = append(selfRecord.BindingSpecs, DelegateBindingSpecInfo{BindingSpec: tok})
		}
		if iface, err := OpenBindingsInterface(); err == nil {
			selfRecord.Operations = operationIdentifiers(&iface)
			if hash, herr := interfaceContentHash(&iface); herr == nil {
				selfRecord.ContentHash = hash
			}
		}
	})
	return selfRecord
}

func selfDelegateSummary() DelegateSummary {
	s := summaryFromRecord(selfDelegateRecord())
	s.Builtin = true
	return s
}

// --- Snapshotting ---

// operationIdentifiers returns every identifier the interface's operations
// answer to — keys and aliases, one flat sorted set (OBI-T-12's namespace).
func operationIdentifiers(iface *openbindings.Interface) []string {
	seen := make(map[string]struct{}, len(iface.Operations))
	// Non-nil so the summary's operations marshals as [] — the contract's
	// required array — even for an interface declaring no operations.
	ids := []string{}
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for key, op := range iface.Operations {
		add(key)
		for _, alias := range op.Aliases {
			add(alias)
		}
	}
	sort.Strings(ids)
	return ids
}

// interfaceContentHash pins an interface document: sha256 over its RFC 8785
// canonical form, so the pin tracks content rather than serialization
// accidents. Opaque to consumers; compared for equality.
func interfaceContentHash(iface *openbindings.Interface) (string, error) {
	canonical, err := canonicaljson.Marshal(iface)
	if err != nil {
		return "", fmt.Errorf("canonicalize interface: %w", err)
	}
	return HashContent(canonical), nil
}

// snapshotDelegate resolves a location and takes the registration snapshot.
// Resolution failure fails the snapshot: a delegate is its OBI, so an
// unresolvable reference is nothing to register.
func snapshotDelegate(location string) (DelegateRecord, error) {
	iface, err := resolveDelegateInterface(location)
	if err != nil {
		return DelegateRecord{}, exitText(1, fmt.Sprintf(
			"cannot resolve delegate %q: %v\na delegate is its OpenBindings interface; register it once it resolves", location, err), true)
	}

	rec := DelegateRecord{
		Location:   location,
		Name:       delegates.NameFromLocation(location),
		Operations: operationIdentifiers(iface),
	}
	if iface.Name != "" {
		rec.Name = iface.Name
	}
	if hash, err := interfaceContentHash(iface); err == nil {
		rec.ContentHash = hash
	}
	rec.Capabilities = delegateCapabilities(iface)

	// BindingSpecs are a best-effort probe of the delegate's listBindingSpecs; a delegate
	// that does not answer simply snapshots with none.
	if fmts, err := delegates.ProbeFormats(location, delegates.DefaultProbeTimeout); err == nil {
		for _, f := range fmts {
			rec.BindingSpecs = append(rec.BindingSpecs, DelegateBindingSpecInfo{BindingSpec: f})
		}
	}
	return rec, nil
}

// normalizeDelegateLocation validates and canonicalizes a delegate location.
func normalizeDelegateLocation(location string) (string, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return "", exitText(2, "a delegate location is required", true)
	}
	if delegates.IsLocalPath(location) {
		location = delegates.ExecScheme + location
	}
	if location != SelfDelegateLocation && !delegates.IsHTTPURL(location) && !delegates.IsExecURL(location) {
		return "", exitText(1, "delegate location must be an exec:, http://, https://, or local path", true)
	}
	return location, nil
}

// --- The manager surface ---

// RegisterDelegate registers (or refreshes) a delegate: resolve the location,
// snapshot what it carries, pin the resolved document, persist. Fails when the
// location cannot be resolved. Re-registration replaces the snapshot and
// preserves the registrar's preferences; a non-nil preference sets the
// delegate-level value.
func RegisterDelegate(location string, preference *float64) (*DelegateSummary, error) {
	location, err := normalizeDelegateLocation(location)
	if err != nil {
		return nil, err
	}
	if location == SelfDelegateLocation || isSelf(location) {
		return nil, exitText(1, "ob is already registered as the self-delegate \"ob\"", true)
	}

	envPath, err := FindEnvPath()
	if err != nil {
		return nil, exitText(1, "no environment found; run 'ob init' first", true)
	}

	snapshot, err := snapshotDelegate(location)
	if err != nil {
		return nil, err
	}

	// The load-mutate-save cycle runs under mutateEnvConfig's optimistic-
	// concurrency guard (internal/app/init.go): a concurrent `ob` process
	// registering, unregistering, or re-preferring the same registry between
	// this cycle's load and save is detected and retried against the fresh
	// state, rather than silently lost. snapshotDelegate above is the
	// expensive, non-idempotent part (it resolves and probes the delegate) and
	// deliberately runs once, outside the retry loop; only the registry
	// mutation itself — which IS safe to reapply — retries.
	rec, err := mutateEnvConfig(envPath, func(config *EnvConfig) (DelegateRecord, error) {
		rec := snapshot
		if idx := findDelegateRecord(config, location); idx >= 0 {
			// Refresh: the snapshot is the delegate's data; the preferences are
			// the registrar's and persist untouched.
			existing := config.Delegates[idx]
			rec.Preference = existing.Preference
			rec.OperationPreferences = existing.OperationPreferences
			rec.BindingSpecPreferences = existing.BindingSpecPreferences
			config.Delegates[idx] = rec
		} else {
			config.Delegates = append(config.Delegates, rec)
		}
		if preference != nil {
			idx := findDelegateRecord(config, location)
			config.Delegates[idx].Preference = preference
			rec = config.Delegates[idx]
		}
		return rec, nil
	})
	if err != nil {
		return nil, err
	}
	summary := summaryFromRecord(rec)
	return &summary, nil
}

// UnregisterDelegate removes a registered delegate. Idempotent: unregistering
// a location that is not registered succeeds (removed=false). The contract's
// output is null; removed only feeds the human rendering.
func UnregisterDelegate(location string) (removed bool, err error) {
	location, err = normalizeDelegateLocation(location)
	if err != nil {
		return false, err
	}
	if location == SelfDelegateLocation || isSelf(location) {
		return false, exitText(1, "the self-delegate cannot be unregistered", true)
	}

	envPath, err := FindEnvPath()
	if err != nil {
		return false, exitText(1, "no environment found; run 'ob init' first", true)
	}

	removed, err = mutateEnvConfig(envPath, func(config *EnvConfig) (bool, error) {
		idx := findDelegateRecord(config, location)
		if idx < 0 {
			return false, errEnvConfigNoop
		}
		config.Delegates = append(config.Delegates[:idx], config.Delegates[idx+1:]...)
		return true, nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// DelegateListOutput is listDelegates' output: every registered delegate, the
// self-delegate first.
type DelegateListOutput struct {
	Delegates []DelegateSummary `json:"delegates"`
}

// Render returns a human-friendly representation.
func (o DelegateListOutput) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Delegates:"))
	for _, d := range o.Delegates {
		sb.WriteString("\n\n")
		for _, line := range strings.Split(d.Render(), "\n") {
			sb.WriteString("  " + line + "\n")
		}
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// ListDelegates lists the registry from its records: the self-delegate first,
// then registered delegates in registration order. No live resolution — a
// summary reflects each delegate as of its last snapshot.
func ListDelegates() DelegateListOutput {
	out := DelegateListOutput{Delegates: []DelegateSummary{selfDelegateSummary()}}
	for _, rec := range GetDelegateContext().Delegates {
		out.Delegates = append(out.Delegates, summaryFromRecord(rec))
	}
	return out
}

// ResolveDelegateOutput is resolveDelegate's output: the operation echoed back
// and the carriers, best first.
type ResolveDelegateOutput struct {
	Operation  string            `json:"operation"`
	Candidates []DelegateSummary `json:"candidates"`
}

// Render returns a human-friendly representation.
func (o ResolveDelegateOutput) Render() string {
	s := Styles
	if len(o.Candidates) == 0 {
		return s.Dim.Render("No registered delegate carries ") + s.Key.Render(o.Operation)
	}
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Delegates carrying "))
	sb.WriteString(s.Key.Render(o.Operation))
	for i, d := range o.Candidates {
		fmt.Fprintf(&sb, "\n  %d. %s", i+1, d.Name)
		if d.Location != "" && d.Location != d.Name {
			sb.WriteString(s.Dim.Render(" " + d.Location))
		}
		if d.Builtin {
			sb.WriteString(s.Dim.Render(" (builtin)"))
		}
	}
	return sb.String()
}

// ResolveDelegate resolves an operation to the registered delegates that carry
// it — those whose snapshot answers to the operation identifier — ordered by
// effective preference, best first; ties favor the self-delegate, then
// registration order. It resolves candidates only: what to do with them
// (route, aggregate, narrow) is the caller's, and an empty candidate list is
// an answer, not an error.
func ResolveDelegate(operation string) (*ResolveDelegateOutput, error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return nil, usageExit("delegate resolve <operation>")
	}

	records := append([]DelegateRecord{selfDelegateRecord()}, GetDelegateContext().Delegates...)
	type ranked struct {
		summary DelegateSummary
		pref    float64
	}
	var carriers []ranked
	for i, rec := range records {
		if !carriesOperation(rec.Operations, operation) {
			continue
		}
		s := summaryFromRecord(rec)
		s.Builtin = i == 0
		carriers = append(carriers, ranked{summary: s, pref: rec.effectiveOperationPreference(operation)})
	}
	// Stable sort: self was appended first and records follow registration
	// order, so equal preferences keep self-first-then-registration ties.
	sort.SliceStable(carriers, func(i, j int) bool { return carriers[i].pref > carriers[j].pref })

	out := &ResolveDelegateOutput{Operation: operation, Candidates: []DelegateSummary{}}
	for _, c := range carriers {
		out.Candidates = append(out.Candidates, c.summary)
	}
	return out, nil
}

func carriesOperation(operations []string, operation string) bool {
	for _, op := range operations {
		if op == operation {
			return true
		}
	}
	return false
}

// SetDelegatePreferenceInput configures a preference update. A nil Preference
// clears: with an Operation it removes that entry from the index; without one
// it resets the delegate-level value to the unset baseline. BindingSpec
// scopes an operation entry to one binding specification (ob's extra
// granularity).
type SetDelegatePreferenceInput struct {
	Location    string   `json:"location"`
	Preference  *float64 `json:"preference"`
	Operation   string   `json:"operation,omitempty"`
	BindingSpec string   `json:"bindingSpec,omitempty"`
}

// SetDelegatePreference sets or clears a registered delegate's selection
// preference. Preference orders the candidates resolveDelegate returns; which
// candidate a caller uses stays the caller's decision. Returns the updated
// summary.
func SetDelegatePreference(in SetDelegatePreferenceInput) (*DelegateSummary, error) {
	location, err := normalizeDelegateLocation(in.Location)
	if err != nil {
		return nil, err
	}
	if location == SelfDelegateLocation || isSelf(location) {
		return nil, exitText(1, "the self-delegate sits at the baseline; prefer or bury external delegates relative to it", true)
	}
	if in.BindingSpec != "" && in.Operation == "" {
		return nil, exitText(2, "a format scope requires an operation", true)
	}

	envPath, err := FindEnvPath()
	if err != nil {
		return nil, exitText(1, "no environment found; run 'ob init' first", true)
	}

	rec, err := mutateEnvConfig(envPath, func(config *EnvConfig) (DelegateRecord, error) {
		idx := findDelegateRecord(config, location)
		if idx < 0 {
			return DelegateRecord{}, exitText(1, fmt.Sprintf("delegate %q is not registered; register it first", location), true)
		}
		rec := &config.Delegates[idx]

		switch {
		case in.Operation == "":
			rec.Preference = in.Preference // nil clears to the baseline
		case in.BindingSpec != "":
			setFormatPreference(rec, in.Operation, in.BindingSpec, in.Preference)
		default:
			if in.Preference == nil {
				delete(rec.OperationPreferences, in.Operation)
				if len(rec.OperationPreferences) == 0 {
					rec.OperationPreferences = nil
				}
			} else {
				if rec.OperationPreferences == nil {
					rec.OperationPreferences = map[string]float64{}
				}
				rec.OperationPreferences[in.Operation] = *in.Preference
			}
		}
		return *rec, nil
	})
	if err != nil {
		return nil, err
	}
	summary := summaryFromRecord(rec)
	return &summary, nil
}

func setFormatPreference(rec *DelegateRecord, operation, format string, preference *float64) {
	for i := range rec.BindingSpecPreferences {
		fp := &rec.BindingSpecPreferences[i]
		if fp.Operation == operation && fp.BindingSpec == format {
			if preference == nil {
				rec.BindingSpecPreferences = append(rec.BindingSpecPreferences[:i], rec.BindingSpecPreferences[i+1:]...)
				if len(rec.BindingSpecPreferences) == 0 {
					rec.BindingSpecPreferences = nil
				}
			} else {
				fp.Preference = *preference
			}
			return
		}
	}
	if preference != nil {
		rec.BindingSpecPreferences = append(rec.BindingSpecPreferences, BindingSpecPreference{
			Operation: operation, BindingSpec: format, Preference: *preference,
		})
	}
}
