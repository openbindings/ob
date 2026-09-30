package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSurfaceRootIsAnInertCommandShell(t *testing.T) {
	root := NewSurfaceRoot()
	var check func(*cobra.Command)
	check = func(command *cobra.Command) {
		if len(command.Commands()) == 0 && command.Parent() != nil {
			if command.Annotations["surface-preview"] != "placeholder" || command.RunE == nil {
				t.Errorf("%s is not a placeholder", command.CommandPath())
			}
		}
		for _, child := range command.Commands() {
			check(child)
		}
	}
	check(root)

	if surfaceCommand(root, "show") == nil ||
		surfaceCommand(root, "source", "show") == nil ||
		surfaceCommand(root, "operation", "show") == nil ||
		surfaceCommand(root, "binding", "show") == nil {
		t.Fatal("proposed show commands are missing")
	}
	if surfaceCommand(root, "source", "add").Flags().Lookup("pull") == nil {
		t.Fatal("source add --pull is missing")
	}
	if surfaceCommand(root, "resolve").Flags().Lookup("with-origin") == nil {
		t.Fatal("resolve --with-origin is missing")
	}
	if surfaceCommand(root, "operation", "invoke").Flags().Lookup("envelope") == nil {
		t.Fatal("operation invoke --envelope is missing")
	}

	path := filepath.Join(t.TempDir(), "would-create.obi.json")
	root.SetArgs([]string{"new", "-o", path})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "command-surface placeholder") {
		t.Fatalf("new must return a placeholder error, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("preview created an OBI: stat error = %v", err)
	}
}
