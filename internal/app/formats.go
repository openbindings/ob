package app

import (
	"sort"
	"strings"
	"sync"

	"github.com/openbindings/ob/internal/delegates"
	openbindings "github.com/openbindings/openbindings-go"
)

// BindingSpecInfo describes a supported binding specification for display and
// for the listBindingSpecs contract result (BindingSpecInfo shape).
type BindingSpecInfo struct {
	BindingSpec string `json:"bindingSpec"`
	Description string `json:"description,omitempty"`
}

var (
	nativeTokens     []string
	nativeTokensOnce sync.Once
)

// RenderFormatList returns a human-friendly styled representation of a format list.
func RenderBindingSpecList(formats []BindingSpecInfo) string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render("Supported binding specifications:"))
	sb.WriteString("\n")
	for _, f := range formats {
		sb.WriteString("  ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(s.Key.Render(f.BindingSpec))
		if isDraftBindingSpec(f.BindingSpec) {
			sb.WriteString(s.Dim.Render(" (draft)"))
		}
		if f.Description != "" {
			sb.WriteString(s.Dim.Render(" - " + f.Description))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// isDraftBindingSpec reports whether a binding-spec token is one of ob's
// pre-promotion drafts, whose identifier is not yet minted. A minted OB
// binding spec (openbindings.<name>@<n>) and
// a namespaced third-party identifier both carry a dot in the name; the bare
// core version marker (openbindings@<version>) is a name of just "openbindings".
// A bare, dotless token that isn't the core marker is a draft.
func isDraftBindingSpec(tok string) bool {
	name := tok
	if at := strings.IndexByte(tok, '@'); at >= 0 {
		name = tok[:at]
	}
	if name == "openbindings" || strings.Contains(name, ".") {
		return false
	}
	return true
}

// ListFormats returns all formats that ob can handle, both built-in (native
// Go SDK drivers) and external delegates.
func ListBindingSpecs() []BindingSpecInfo {
	var formats []BindingSpecInfo

	for _, tok := range getNativeTokens() {
		formats = append(formats, BindingSpecInfo{BindingSpec: tok})
	}

	// Delegate formats come from the registry's registration-time snapshots —
	// the aggregate-across-all composition (native ∪ every delegate), never a
	// route-to-one, and no live probing.
	for _, rec := range GetDelegateContext().Delegates {
		for _, f := range rec.BindingSpecs {
			formats = append(formats, BindingSpecInfo{BindingSpec: f.BindingSpec, Description: f.Description})
		}
	}

	return uniqueSortedFormats(formats)
}

func getNativeTokens() []string {
	nativeTokensOnce.Do(func() {
		nativeTokens = []string{"openbindings@" + openbindings.MaxTestedVersion}
		for _, fi := range DefaultInvoker().BindingSpecs() {
			nativeTokens = append(nativeTokens, fi.BindingSpec)
		}
	})
	return nativeTokens
}

// resetNativeTokens clears the cached native token list. Intended for tests only.
func resetNativeTokens() {
	nativeTokensOnce = sync.Once{}
	nativeTokens = nil
}

// BuiltinSupportsFormat checks if ob natively supports a given format (in-process).
func BuiltinSupportsFormat(format string) bool {
	for _, tok := range getNativeTokens() {
		if delegates.SupportsFormat(tok, format) {
			return true
		}
	}
	return false
}

func uniqueSortedFormats(in []BindingSpecInfo) []BindingSpecInfo {
	if len(in) == 0 {
		return []BindingSpecInfo{} // wire shape: always an array, never null
	}
	seen := make(map[string]BindingSpecInfo, len(in))
	for _, f := range in {
		if f.BindingSpec == "" {
			continue
		}
		if _, exists := seen[f.BindingSpec]; !exists {
			seen[f.BindingSpec] = f
		}
	}
	out := make([]BindingSpecInfo, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].BindingSpec < out[j].BindingSpec
	})
	return out
}

// SpecFamily extracts the lowercase family name from a binding-specification
// identifier ("openbindings.openapi@1" → "openapi"; a pre-promotion draft
// token passes through). Identifiers themselves stay exact
// and opaque for matching (core §6); this is dispatch/display convenience.
func SpecFamily(identifier string) string {
	name := strings.TrimSpace(identifier)
	if at := strings.IndexByte(name, '@'); at > 0 {
		name = name[:at]
	}
	name = strings.TrimPrefix(name, "openbindings.")
	return strings.ToLower(name)
}
