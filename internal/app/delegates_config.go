package app

var defaultDelegates = []string{"exec:ob", "http://localhost:8787"}

// DelegateContext contains delegate-related data from the environment config.
type DelegateContext struct {
	Delegates []string
}

// getDelegateContextFunc is the indirection point for tests so they can
// substitute an empty delegate context without depending on whatever
// .openbindings/ config exists in the developer's home directory or in
// the global fallback. The default implementation walks up from cwd
// (per FindEnvironment) and falls back to ~/.config/openbindings/.
var getDelegateContextFunc = defaultGetDelegateContext

// GetDelegateContext loads the environment config and extracts delegate context.
// Returns zero values if no environment exists or if loading fails.
func GetDelegateContext() DelegateContext {
	return getDelegateContextFunc()
}

func defaultGetDelegateContext() DelegateContext {
	envPath, err := FindEnvPath()
	if err != nil {
		return DelegateContext{}
	}
	config, err := LoadEnvConfig(envPath)
	if err != nil {
		return DelegateContext{}
	}
	return DelegateContext{
		Delegates: config.Delegates,
	}
}

// IsDefaultDelegate reports whether a delegate location is in the defaults list.
func IsDefaultDelegate(location string) bool {
	for _, d := range defaultDelegates {
		if d == location {
			return true
		}
	}
	return false
}

// migrateDefaultDelegates ensures all default delegates are present unless
// the user has explicitly removed them. Also prunes removedDefaultDelegates
// entries that are no longer in the defaults list.
// Returns true if the config was modified.
func migrateDefaultDelegates(config *EnvConfig) bool {
	existing := make(map[string]struct{}, len(config.Delegates))
	for _, d := range config.Delegates {
		existing[d] = struct{}{}
	}
	removed := make(map[string]struct{}, len(config.RemovedDefaultDelegates))
	for _, d := range config.RemovedDefaultDelegates {
		removed[d] = struct{}{}
	}

	var toAdd []string
	for _, d := range defaultDelegates {
		if _, ok := existing[d]; ok {
			continue
		}
		if _, ok := removed[d]; ok {
			continue
		}
		toAdd = append(toAdd, d)
	}

	var pruned []string
	for _, d := range config.RemovedDefaultDelegates {
		if IsDefaultDelegate(d) {
			pruned = append(pruned, d)
		}
	}
	prunedChanged := len(pruned) != len(config.RemovedDefaultDelegates)
	if prunedChanged {
		config.RemovedDefaultDelegates = pruned
	}

	if len(toAdd) == 0 {
		return prunedChanged
	}
	config.Delegates = append(toAdd, config.Delegates...)
	return true
}
