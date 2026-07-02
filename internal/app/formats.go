package app

import (
	"sort"
	"strings"
	"sync"

	"github.com/openbindings/ob/internal/delegates"
	openbindings "github.com/openbindings/openbindings-go"
)

// FormatInfo describes a supported binding format for display purposes.
type FormatInfo struct {
	Token       string `json:"token"`
	Description string `json:"description,omitempty"`
}

var (
	nativeTokens     []string
	nativeTokensOnce sync.Once
)

// RenderFormatList returns a human-friendly styled representation of a format list.
func RenderFormatList(formats []FormatInfo) string {
	s := Styles
	var sb strings.Builder

	sb.WriteString(s.Header.Render("Supported formats:"))
	sb.WriteString("\n")
	for _, f := range formats {
		sb.WriteString("  ")
		sb.WriteString(s.Bullet.Render("•"))
		sb.WriteString(" ")
		sb.WriteString(s.Key.Render(f.Token))
		if f.Description != "" {
			sb.WriteString(s.Dim.Render(" - " + f.Description))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// ListFormats returns all formats that ob can handle, both built-in (native
// Go SDK drivers) and external delegates.
func ListFormats() []FormatInfo {
	var formats []FormatInfo

	for _, tok := range getNativeTokens() {
		formats = append(formats, FormatInfo{Token: tok})
	}

	// Delegate formats come from the registry's registration-time snapshots —
	// the aggregate-across-all composition (native ∪ every delegate), never a
	// route-to-one, and no live probing.
	for _, rec := range GetDelegateContext().Delegates {
		for _, f := range rec.Formats {
			formats = append(formats, FormatInfo{Token: f.Format, Description: f.Description})
		}
	}

	return uniqueSortedFormats(formats)
}

func getNativeTokens() []string {
	nativeTokensOnce.Do(func() {
		nativeTokens = []string{"openbindings@" + openbindings.MaxTestedVersion}
		for _, fi := range DefaultInvoker().Formats() {
			nativeTokens = append(nativeTokens, fi.Token)
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

func uniqueSortedFormats(in []FormatInfo) []FormatInfo {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]FormatInfo, len(in))
	for _, f := range in {
		if f.Token == "" {
			continue
		}
		if _, exists := seen[f.Token]; !exists {
			seen[f.Token] = f
		}
	}
	out := make([]FormatInfo, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Token < out[j].Token
	})
	return out
}
