package app

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/delegates"
)

// DelegateAddResult is returned by DelegateAdd: what ob discovered when
// registering the delegate (CreateDelegateResult in the contract).
type DelegateAddResult struct {
	Location     string               `json:"location"`
	Name         string               `json:"name,omitempty"`
	Reachable    bool                 `json:"reachable"`
	Capabilities []DelegateCapability `json:"capabilities,omitempty"`
	Formats      []DelegateFormatInfo `json:"formats,omitempty"`
}

// Render returns a human-readable summary.
func (r DelegateAddResult) Render() string {
	s := Styles
	var sb strings.Builder
	sb.WriteString(s.Header.Render("Registered delegate"))
	sb.WriteString(" ")
	sb.WriteString(s.Key.Render(r.Name))
	sb.WriteString(s.Dim.Render(" " + r.Location))

	if !r.Reachable {
		sb.WriteString("\n  ")
		sb.WriteString(s.Warning.Render("! unreachable — registered, but ob could not resolve its interface to learn its capabilities"))
		return sb.String()
	}
	if len(r.Capabilities) == 0 {
		sb.WriteString("\n  ")
		sb.WriteString(s.Warning.Render("! no delegatable capability — its interface satisfies none of invoke/synthesize/inspect"))
		return sb.String()
	}

	caps := make([]string, len(r.Capabilities))
	for i, c := range r.Capabilities {
		caps[i] = string(c)
	}
	sb.WriteString("\n  ")
	sb.WriteString(s.Dim.Render("capabilities: "))
	sb.WriteString(strings.Join(caps, ", "))

	if len(r.Formats) > 0 {
		toks := make([]string, len(r.Formats))
		for i, f := range r.Formats {
			toks[i] = f.Format
		}
		sb.WriteString("\n  ")
		sb.WriteString(s.Dim.Render("formats: "))
		sb.WriteString(strings.Join(toks, ", "))
	}
	return sb.String()
}

// DelegateAdd adds a binding format delegate to the environment.
func DelegateAdd(url string) (*DelegateAddResult, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, usageExit("delegate add <url>")
	}

	if !delegates.IsHTTPURL(url) && !delegates.IsExecURL(url) && !delegates.IsLocalPath(url) {
		return nil, exitText(1, "delegate must be an exec:, http://, https://, or local path", true)
	}

	if delegates.IsLocalPath(url) {
		url = delegates.ExecScheme + url
	}

	envPath, err := FindEnvPath()
	if err != nil {
		return nil, exitText(1, "no environment found; run 'ob init' first", true)
	}

	config, err := LoadEnvConfig(envPath)
	if err != nil {
		return nil, exitText(1, err.Error(), true)
	}

	for _, p := range config.Delegates {
		if p == url {
			return nil, exitText(1, fmt.Sprintf("delegate %q already registered", url), true)
		}
	}

	config.Delegates = append(config.Delegates, url)

	var newRemoved []string
	for _, d := range config.RemovedDefaultDelegates {
		if d != url {
			newRemoved = append(newRemoved, d)
		}
	}
	config.RemovedDefaultDelegates = newRemoved

	if err := SaveEnvConfig(envPath, config); err != nil {
		return nil, exitText(1, err.Error(), true)
	}

	// Resolve the delegate's OBI and report what it provides. Registration has
	// already succeeded; introspection failure only downgrades the report.
	intro := introspectDelegate(url)
	return &DelegateAddResult{
		Location:     url,
		Name:         intro.Name,
		Reachable:    intro.Reachable,
		Capabilities: intro.Capabilities,
		Formats:      intro.Formats,
	}, nil
}
