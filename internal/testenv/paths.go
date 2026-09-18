// Package testenv isolates application state without redirecting HOME or Go's
// toolchain/module directories. It is imported only by test entrypoints.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Run gives a test process and its children dedicated config/cache/credential
// paths. Cleanup is limited to the directory this call successfully created.
func Run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "ob-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	for key, value := range map[string]string{
		"OB_CONFIG_DIR":       filepath.Join(dir, "config"),
		"OB_CACHE_DIR":        filepath.Join(dir, "cache"),
		"OB_CREDENTIALS_FILE": filepath.Join(dir, "credentials.json"),
		"OB_NO_UPDATE_CHECK":  "1",
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}
