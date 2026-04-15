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
// Go SDK executors) and external delegates.
func ListFormats() []FormatInfo {
	var formats []FormatInfo

	for _, tok := range getNativeTokens() {
		formats = append(formats, FormatInfo{Token: tok})
	}

	delCtx := GetDelegateContext()

	discovered, _ := delegates.Discover(delegates.DiscoverParams{
		Delegates: delCtx.Delegates,
	})
	for _, p := range discovered {
		delegateFormats, err := delegates.ProbeFormats(p.Location, delegates.DefaultProbeTimeout)
		if err == nil {
			for _, f := range delegateFormats {
				formats = append(formats, FormatInfo{Token: f})
			}
		}
	}

	return uniqueSortedFormats(formats)
}

func getNativeTokens() []string {
	nativeTokensOnce.Do(func() {
		nativeTokens = []string{"openbindings@" + openbindings.MaxTestedVersion}
		for _, fi := range DefaultExecutor().Formats() {
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
