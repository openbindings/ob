package app

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	openbindings "github.com/openbindings/openbindings-go"
)

// BindingSpecInfo describes a supported binding specification for display and
// for the listBindingSpecs contract result (BindingSpecInfo shape).
type BindingSpecInfo struct {
	BindingSpec string `json:"bindingSpec"`
	Description string `json:"description,omitempty"`
}

// BindingSpecCheckInput is the wire input to checkBindingSpecs.
type BindingSpecCheckInput struct {
	BindingSpecs []string `json:"bindingSpecs"`
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

// RenderBindingSpecVerdicts returns a human-friendly exact support report.
func RenderBindingSpecVerdicts(verdicts []openbindings.BindingSpecVerdict) string {
	if len(verdicts) == 0 {
		return "No binding specifications requested."
	}
	var sb strings.Builder
	for i, verdict := range verdicts {
		if i > 0 {
			sb.WriteByte('\n')
		}
		mark := "no"
		if verdict.Supported {
			mark = "yes"
		}
		fmt.Fprintf(&sb, "%s: %s", verdict.BindingSpec, mark)
	}
	return sb.String()
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

// ListBindingSpecs returns ob's advisory native-format listing. Registered
// delegates retain their own advisory listings in ListDelegates; they are not
// promoted into ob's list because delegation support is checked live.
func ListBindingSpecs() []BindingSpecInfo {
	var formats []BindingSpecInfo

	for _, tok := range getNativeTokens() {
		formats = append(formats, BindingSpecInfo{BindingSpec: tok})
	}

	return uniqueSortedFormats(formats)
}

// CheckBindingSpecs authoritatively checks ob's native support for exact,
// opaque identifiers. The SDK helper provides de-duplication and preserves
// first-occurrence order.
func CheckBindingSpecs(bindingSpecs []string) []openbindings.BindingSpecVerdict {
	supported := make([]openbindings.BindingSpecInfo, 0, len(getNativeTokens()))
	for _, bindingSpec := range getNativeTokens() {
		supported = append(supported, openbindings.BindingSpecInfo{BindingSpec: bindingSpec})
	}
	return openbindings.CheckBindingSpecs(bindingSpecs, supported)
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
	verdicts := CheckBindingSpecs([]string{format})
	return len(verdicts) == 1 && verdicts[0].Supported
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
