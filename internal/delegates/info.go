// Package delegates - info.go contains delegate info types and helpers.
package delegates

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/openbindings/ob/internal/execref"
)

// Info represents a discovered binding format delegate.
type Info struct {
	Name     string `json:"name"`
	Location string `json:"location,omitempty"` // how to reach the delegate (path, URL, or cmd ref)
	Source   string `json:"source"`             // environment
}

// IsHTTPURL returns true if the string is an HTTP or HTTPS URL.
func IsHTTPURL(s string) bool {
	return strings.HasPrefix(s, HTTPScheme) || strings.HasPrefix(s, HTTPSScheme)
}

// IsExecURL returns true if the string is an exec: command reference.
func IsExecURL(s string) bool {
	return strings.HasPrefix(s, ExecScheme)
}

// IsLocalPath returns true if the address looks like a local filesystem path.
func IsLocalPath(addr string) bool {
	if strings.Contains(addr, "://") {
		return false
	}
	return strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "./") || strings.HasPrefix(addr, "../") || filepath.IsAbs(addr)
}

// NameFromLocation derives a delegate name from a location.
func NameFromLocation(loc string) string {
	// Command reference (exec:...)
	if execref.IsExec(loc) {
		if cmd, err := execref.RootCommand(loc); err == nil && cmd != "" {
			return cmd
		}
		return loc
	}

	// URL - use host
	if strings.Contains(loc, "://") {
		u, err := url.Parse(loc)
		if err != nil {
			return loc
		}
		if u.Host != "" {
			return u.Host
		}
		return loc
	}

	// Local path - use basename
	return filepath.Base(loc)
}
