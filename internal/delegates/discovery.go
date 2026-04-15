// Package delegates - discovery.go contains binding format delegate discovery logic.
package delegates

import (
	"sort"
	"strings"
)

// DiscoverParams configures delegate discovery.
type DiscoverParams struct {
	// Delegates is the list of delegate locations from the active environment.
	Delegates []string
}

// Discover finds all delegates from the environment.
func Discover(params DiscoverParams) ([]Info, error) {
	var all []Info
	seen := map[string]struct{}{}

	for _, loc := range params.Delegates {
		loc = strings.TrimSpace(loc)
		if loc == "" {
			continue
		}
		if _, ok := seen[loc]; ok {
			continue
		}
		seen[loc] = struct{}{}

		name := NameFromLocation(loc)
		all = append(all, Info{
			Name:     name,
			Location: loc,
			Source:   SourceEnvironment,
		})
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Name == all[j].Name {
			return all[i].Location < all[j].Location
		}
		return all[i].Name < all[j].Name
	})
	return all, nil
}
