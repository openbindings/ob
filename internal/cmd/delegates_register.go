package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/openbindings-go/jsonvalue"
	"github.com/spf13/cobra"
)

// maxRegisteredInterfaceBytes mirrors OB's retained-OBI capacity; reading one
// extra byte lets the CLI refuse oversized input before any registry work.
const maxRegisteredInterfaceBytes = 1 << 20

func newDelegateRegisterCmd() *cobra.Command {
	var (
		roles            []string
		registrationID   string
		preferences      []string
		clearPreferences bool
	)

	c := &cobra.Command{
		Use:     "register <interface>",
		Aliases: []string{"add"},
		Short:   "Enroll an interface document for one or more roles",
		Long: `Enroll an OpenBindings interface document as a delegate for explicit roles.

The argument is an interface document: a file path, or - to read it from
stdin. It is the actual value that is retained, not a location to resolve;
fetch or synthesize a remote interface first (for example
'ob resolve https://example.com -F json | ob delegate register - --role invoke').

Every requested role must accept the whole document (see 'ob delegate roles').
Roles that would also match are never enrolled implicitly. A new enrollment
receives a fresh registration ID; enrolling the same document twice creates
two registrations. Pass --id to replace an existing registration's complete
interface and role set while keeping its ID.

Explicit preferences: repeat --preference <role>=<number> to set the complete
map (numbers are retained exactly, including zero); --clear-preferences sets
an empty map. With neither, a fresh registration stores no explicit
preferences and a replacement keeps the entries of retained roles.

Examples:
  ob delegate register ./invoker.obi.json --role invoke
  ob delegate register ./tool.obi.json --role synthesize --role inspect --preference synthesize=10
  ob resolve https://tools.example.com -F json | jq .interface | ob delegate register - --role invoke
  ob delegate register ./tool.obi.json --id dlg_... --role inspect --clear-preferences`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if looksLikeLocation(args[0]) {
				return app.ExitResult{Code: 2, Message: fmt.Sprintf("%q is a location, but register takes an interface document (a file path or - for stdin); resolve it first, e.g. 'ob resolve %s -F json | jq .interface | ob delegate register - --role <role>'", args[0], args[0]), ToStderr: true}
			}
			if len(roles) == 0 {
				return app.ExitResult{Code: 2, Message: "--role is required: name every role to enroll for (see 'ob delegate roles')", ToStderr: true}
			}
			if cmd.Flags().Changed("id") && registrationID == "" {
				return app.ExitResult{Code: 2, Message: "--id must name an existing registration; omit it for a fresh enrollment", ToStderr: true}
			}
			seen := map[string]bool{}
			for _, role := range roles {
				if strings.TrimSpace(role) == "" {
					return app.ExitResult{Code: 2, Message: "--role must not be empty", ToStderr: true}
				}
				if seen[role] {
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("--role %s is repeated; roles are a set", role), ToStderr: true}
				}
				seen[role] = true
			}
			rolePreferences, err := parseRolePreferenceFlags(preferences, clearPreferences)
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			document, err := readInterfaceDocument(args[0], cmd.InOrStdin())
			if err != nil {
				return app.ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
			}
			result, err := app.RegisterDelegate(app.RoleRegistrationInput{ID: registrationID, Interface: document, Roles: roles, RolePreferences: rolePreferences})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			format, outputPath := getOutputFlags(cmd)
			return app.OutputResultText(result, format, outputPath, result.Render)
		},
	}
	c.Flags().StringArrayVar(&roles, "role", nil, "role to enroll for (repeatable, required)")
	c.Flags().StringVar(&registrationID, "id", "", "existing registration ID to replace (complete interface and role set)")
	c.Flags().StringArrayVar(&preferences, "preference", nil, "explicit preference as role=number (repeatable; together they form the complete map)")
	c.Flags().BoolVar(&clearPreferences, "clear-preferences", false, "store an empty explicit preference map")
	c.SetFlagErrorFunc(retiredFlagGuidance(map[string]string{}))
	return c
}

// parseRolePreferenceFlags builds the complete explicit map from repeated
// role=number tokens. Numbers are kept as exact JSON text; nothing is parsed
// through float64. nil means the map was omitted.
func parseRolePreferenceFlags(entries []string, clear bool) (json.RawMessage, error) {
	if clear && len(entries) > 0 {
		return nil, errors.New("--clear-preferences and --preference are exclusive")
	}
	if clear {
		return json.RawMessage(`{}`), nil
	}
	if len(entries) == 0 {
		return nil, nil
	}
	values := map[string]json.Number{}
	for _, entry := range entries {
		role, text, found := strings.Cut(entry, "=")
		if !found || strings.TrimSpace(role) == "" {
			return nil, fmt.Errorf("--preference %q must be role=number", entry)
		}
		number := json.Number(strings.TrimSpace(text))
		if !app.ValidDelegatePreference(number) {
			return nil, fmt.Errorf("--preference %q: %q is not an exact JSON number ob can retain", entry, text)
		}
		if _, duplicate := values[role]; duplicate {
			return nil, fmt.Errorf("--preference names role %s twice", role)
		}
		values[role] = number
	}
	raw, err := jsonvalue.Marshal(values)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// readInterfaceDocument reads one bounded JSON object from a regular file or
// stdin. It never dereferences a location, follows a symlink, or accepts
// trailing content; the bytes it returns are the value the registry retains.
func readInterfaceDocument(arg string, stdin io.Reader) (json.RawMessage, error) {
	var reader io.Reader
	source := arg
	if arg == app.StdinLocator {
		reader = stdin
		source = "stdin"
	} else {
		info, err := os.Lstat(arg)
		if err != nil {
			return nil, fmt.Errorf("cannot read interface document %s: %v", arg, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("interface document %s must be a regular file", arg)
		}
		f, err := os.Open(arg)
		if err != nil {
			return nil, fmt.Errorf("cannot read interface document %s: %v", arg, err)
		}
		defer f.Close()
		reader = f
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxRegisteredInterfaceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read interface document from %s: %v", source, err)
	}
	if len(data) > maxRegisteredInterfaceBytes {
		return nil, fmt.Errorf("interface document from %s exceeds ob's %d-byte retained interface capacity", source, maxRegisteredInterfaceBytes)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("interface document from %s must be one JSON object (an OpenBindings interface), not a locator", source)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("interface document from %s is not valid JSON: %v", source, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("interface document from %s has trailing content after the JSON object; supply exactly one document", source)
	}
	return json.RawMessage(trimmed), nil
}
