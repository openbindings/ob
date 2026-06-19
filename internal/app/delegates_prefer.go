package app

import (
	"fmt"
	"strings"
)

// SetDelegatePreferenceInput configures a preference update. An empty
// Capability sets the delegate-level preference (the default for all its
// offerings); a Capability (optionally scoped to a Format) sets a per-offering
// override.
type SetDelegatePreferenceInput struct {
	Location   string
	Preference float64
	Capability DelegateCapability
	Format     string
}

// SetDelegatePreferenceResult reports the applied preference.
type SetDelegatePreferenceResult struct {
	Location   string             `json:"location"`
	Preference float64            `json:"preference"`
	Capability DelegateCapability `json:"capability,omitempty"`
	Format     string             `json:"format,omitempty"`
}

// Render returns a human-readable summary.
func (r SetDelegatePreferenceResult) Render() string {
	s := Styles
	scope := "delegate-level"
	if r.Capability != "" {
		scope = string(r.Capability)
		if r.Format != "" {
			scope += " " + r.Format
		}
	}
	return fmt.Sprintf("%s %s preference for %s = %g",
		s.Header.Render("Set"), scope, s.Key.Render(r.Location), r.Preference)
}

// SetDelegatePreference stores a delegate's selection preference (higher = more
// preferred). With no capability it sets the delegate-level default; with a
// capability (and optional format) it sets/replaces a per-offering override.
func SetDelegatePreference(in SetDelegatePreferenceInput) (*SetDelegatePreferenceResult, error) {
	loc := strings.TrimSpace(in.Location)
	if loc == "" {
		return nil, usageExit("delegate prefer <location> <preference>")
	}
	if in.Format != "" && in.Capability == "" {
		return nil, exitText(2, "--source-format requires --capability", true)
	}

	envPath, err := FindEnvPath()
	if err != nil {
		return nil, exitText(1, "no environment found; run 'ob init' first", true)
	}
	config, err := LoadEnvConfig(envPath)
	if err != nil {
		return nil, exitText(1, err.Error(), true)
	}

	idx := -1
	for i := range config.DelegatePreferences {
		if config.DelegatePreferences[i].Location == loc {
			idx = i
			break
		}
	}
	if idx == -1 {
		config.DelegatePreferences = append(config.DelegatePreferences, DelegatePreferenceConfig{Location: loc})
		idx = len(config.DelegatePreferences) - 1
	}
	dp := &config.DelegatePreferences[idx]

	if in.Capability == "" {
		dp.Preference = in.Preference
	} else {
		replaced := false
		for j := range dp.PerOffering {
			o := &dp.PerOffering[j]
			if o.Capability == in.Capability && o.Format == in.Format {
				o.Preference = in.Preference
				replaced = true
				break
			}
		}
		if !replaced {
			dp.PerOffering = append(dp.PerOffering, OfferingPreference{
				Capability: in.Capability,
				Format:     in.Format,
				Preference: in.Preference,
			})
		}
	}

	if err := SaveEnvConfig(envPath, config); err != nil {
		return nil, exitText(1, err.Error(), true)
	}

	return &SetDelegatePreferenceResult{
		Location:   loc,
		Preference: in.Preference,
		Capability: in.Capability,
		Format:     in.Format,
	}, nil
}
