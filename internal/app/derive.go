package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// DeriveResult holds the operations and bindings derived from a single source,
// without merging them into an OBI. This is the building block for diff and merge.
type DeriveResult struct {
	// Operations are the operations derived from the source, keyed by operation ID.
	Operations map[string]openbindings.Operation

	// Bindings are the bindings derived from the source, with Source already
	// set to sourceKey. Keyed by binding name.
	Bindings map[string]openbindings.BindingEntry

	// Metadata from the derived interface (Name, Description, Version).
	// May be empty if the source doesn't provide metadata.
	Name        string
	Description string
	Version     string
}

// DeriveFromSource takes a single source entry and returns the operations and
// bindings it would produce, without merging into any OBI. The sourceKey is
// used to populate BindingEntry.Source.
//
// obiDir is the directory containing the OBI file. Relative artifact paths
// are resolved against this directory (D5). Pass "" if the source artifact
// path is already absolute or pre-resolved.
//
// This function uses the default executor to dispatch format-specific
// conversion and is the shared building block for diff --from-sources
// and merge --from-sources.
func DeriveFromSource(source openbindings.Source, sourceKey string, obiDir string) (DeriveResult, error) {
	locationPath := source.Location
	if locationPath != "" && obiDir != "" && !filepath.IsAbs(locationPath) && !strings.Contains(locationPath, "://") && !isHostPort(locationPath) {
		locationPath = filepath.Join(obiDir, locationPath)
	}

	createSrc := openbindings.CreateSource{
		Format:   source.Format,
		Location: locationPath,
	}
	if source.Content != nil {
		createSrc.Content = source.Content
	}

	generated, err := CreateInterfaceFromSource(context.Background(), &openbindings.CreateInput{
		Sources: []openbindings.CreateSource{createSrc},
	})
	if err != nil {
		return DeriveResult{}, fmt.Errorf("source %q: derive: %w", sourceKey, err)
	}

	return remapBindings(*generated, sourceKey), nil
}

// perSourceDerivation holds the derivation result for a single source.
type perSourceDerivation struct {
	key    string
	format string
	result DeriveResult
}

// deriveSourcesResult holds the output of deriveFromAllSources: per-source
// derivation results (for drift detection) and an assembled Interface
// (for diff/merge comparison). This is the shared building block for both
// diff --from-sources and merge --from-sources.
type deriveSourcesResult struct {
	// PerSource holds per-source derivation results (for drift detection).
	PerSource []perSourceDerivation
	// Assembled is a merged Interface containing all derived operations,
	// bindings, and source entries.
	Assembled *openbindings.Interface
	// Warnings collects non-fatal issues (missing executors, empty sources).
	Warnings []string
}

// deriveFromAllSources iterates through an OBI's sources, derives operations
// from each via the default executor, and assembles the results. Sources
// without an artifact/inline are skipped with a warning. Driver failures
// are treated as warnings (D8), not hard errors.
//
// onlySource optionally scopes derivation to a single source key. Pass ""
// to derive from all sources.
func deriveFromAllSources(iface *openbindings.Interface, obiDir string, onlySource string) (deriveSourcesResult, error) {
	if onlySource != "" {
		if _, exists := iface.Sources[onlySource]; !exists {
			return deriveSourcesResult{}, fmt.Errorf("source %q not found in OBI", onlySource)
		}
	}

	var perSource []perSourceDerivation
	var warnings []string

	sourceKeys := make([]string, 0, len(iface.Sources))
	for key := range iface.Sources {
		sourceKeys = append(sourceKeys, key)
	}
	sort.Strings(sourceKeys)

	for _, key := range sourceKeys {
		src := iface.Sources[key]
		if onlySource != "" && key != onlySource {
			continue
		}

		if src.Location == "" && src.Content == nil {
			warnings = append(warnings, fmt.Sprintf("source %q: no location or content, skipping", key))
			continue
		}

		result, err := DeriveFromSource(src, key, obiDir)
		if err != nil {
			// D8: warn and skip on executor unavailability.
			warnings = append(warnings, fmt.Sprintf("source %q: %v", key, err))
			continue
		}

		perSource = append(perSource, perSourceDerivation{
			key:    key,
			format: src.Format,
			result: result,
		})
	}

	// Assemble a combined Interface from all per-source results.
	assembled := &openbindings.Interface{
		Operations: map[string]openbindings.Operation{},
		Sources:    map[string]openbindings.Source{},
		Bindings:   map[string]openbindings.BindingEntry{},
	}
	for _, ps := range perSource {
		for opKey, op := range ps.result.Operations {
			assembled.Operations[opKey] = op
		}
		for k, b := range ps.result.Bindings {
			assembled.Bindings[k] = b
		}
		if src, ok := iface.Sources[ps.key]; ok {
			assembled.Sources[ps.key] = src
		}
	}

	return deriveSourcesResult{
		PerSource: perSource,
		Assembled: assembled,
		Warnings:  warnings,
	}, nil
}

// remapBindingKeys builds a new bindings map where each key is
// "<operation>.<sourceKey>" and each entry's Source field is set to sourceKey.
func remapBindingKeys(bindings map[string]openbindings.BindingEntry, sourceKey string) map[string]openbindings.BindingEntry {
	out := make(map[string]openbindings.BindingEntry, len(bindings))
	for _, b := range bindings {
		key := b.Operation + "." + sourceKey
		out[key] = openbindings.BindingEntry{
			Operation: b.Operation,
			Source:    sourceKey,
			Ref:       b.Ref,
		}
	}
	return out
}

// remapBindings converts an openbindings.Interface from a source discovery
// into a DeriveResult, remapping binding source keys to the actual source key.
func remapBindings(iface openbindings.Interface, sourceKey string) DeriveResult {
	return DeriveResult{
		Operations:  iface.Operations,
		Bindings:    remapBindingKeys(iface.Bindings, sourceKey),
		Name:        iface.Name,
		Description: iface.Description,
		Version:     iface.Version,
	}
}
