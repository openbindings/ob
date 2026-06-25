package app

import (
	"fmt"
	"strings"

	"github.com/openbindings/ob/internal/delegates"
)

// DelegateRemoveResult is returned by DelegateRemove (DeleteDelegateResult in
// the contract).
type DelegateRemoveResult struct {
	Location string `json:"location"`
}

// Render returns a human-readable summary.
func (r DelegateRemoveResult) Render() string {
	return fmt.Sprintf("removed delegate %s", r.Location)
}

// DelegateRemove removes a delegate from the environment.
func DelegateRemove(url string) (*DelegateRemoveResult, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, usageExit("delegate remove <url>")
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

	var newDelegates []string
	found := false
	for _, p := range config.Delegates {
		if p == url {
			found = true
			continue
		}
		newDelegates = append(newDelegates, p)
	}

	if !found {
		// Tolerant: removing a delegate that isn't registered is a no-op success.
		// Active defaults are merged into config.Delegates at load (LoadEnvConfig ->
		// migrateDefaultDelegates), so they are found above and suppressed normally;
		// reaching here means the delegate genuinely isn't active.
		return &DelegateRemoveResult{Location: url}, nil
	}

	config.Delegates = newDelegates

	if IsDefaultDelegate(url) {
		alreadyRecorded := false
		for _, d := range config.RemovedDefaultDelegates {
			if d == url {
				alreadyRecorded = true
				break
			}
		}
		if !alreadyRecorded {
			config.RemovedDefaultDelegates = append(config.RemovedDefaultDelegates, url)
		}
	}

	if err := SaveEnvConfig(envPath, config); err != nil {
		return nil, exitText(1, err.Error(), true)
	}

	return &DelegateRemoveResult{
		Location: url,
	}, nil
}
