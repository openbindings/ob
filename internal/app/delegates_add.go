package app

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/delegates"
)

// DelegateAddResult is returned by DelegateAdd.
type DelegateAddResult struct {
	Added    string `json:"added"`
	Delegate string `json:"delegate"`
}

// Render returns a human-readable summary.
func (r DelegateAddResult) Render() string {
	return fmt.Sprintf("added delegate %s", r.Delegate)
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

	return &DelegateAddResult{
		Added:    "delegate",
		Delegate: url,
	}, nil
}
