package cmd

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/openbindings-go/canonicaljson"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// embeddedUsageSpec is the usage.kdl content for exec: artifact resolution.
// This is the single source of truth for the OpenBindings CLI (ob) structure.
//
//go:embed usage.kdl
var embeddedUsageSpec string

// NewRoot builds the top-level `ob` command.
//
// We keep errors/usage silent and let our main() decide how to print ExitResult vs generic errors.
func NewRoot() *cobra.Command {
	var usageSpec bool
	var openbindingsFlag bool

	root := &cobra.Command{
		Use:           "ob",
		Short:         "openbindings: one interface · limitless bindings",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if usageSpec {
				fmt.Print(embeddedUsageSpec)
				return nil
			}
			if openbindingsFlag {
				iface, err := app.OpenBindingsInterface()
				if err != nil {
					return app.ExitResult{Code: 1, Message: fmt.Sprintf("failed to load OpenBindings interface: %v", err), ToStderr: true}
				}
				format, outputPath := getOutputFlags(cmd)
				var b []byte
				switch format {
				case "json", "":
					b, err = prettyCanonicalJSON(iface)
				case "yaml":
					b, err = canonicalYAML(iface)
				default:
					return app.ExitResult{Code: 2, Message: fmt.Sprintf("unknown output format %q (valid: json, yaml)", format), ToStderr: true}
				}
				if err != nil {
					return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
				}
				if outputPath != "" {
					if err := app.AtomicWriteFile(outputPath, b, app.FilePerm); err != nil {
						return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
					}
					return app.ExitResult{Code: 0, Message: "Wrote " + outputPath, ToStderr: false}
				}
				fmt.Print(string(b))
				return nil
			}
			return cmd.Help()
		},
	}

	root.Flags().BoolVar(&usageSpec, "usage-spec", false, "output the usage.kdl spec for this CLI")
	root.Flags().BoolVar(&openbindingsFlag, "openbindings", false, "output the OpenBindings interface for this CLI")
	root.PersistentFlags().StringP("output", "o", "", "write output to file (default: stdout)")
	root.PersistentFlags().StringP("format", "F", "", "output format: json|yaml|text")

	root.AddGroup(
		&cobra.Group{ID: "start", Title: "start a working area"},
		&cobra.Group{ID: "explore", Title: "browse and interact"},
		&cobra.Group{ID: "authoring", Title: "interface authoring"},
		&cobra.Group{ID: "delegates", Title: "delegates and formats"},
		&cobra.Group{ID: "serve", Title: "serve and integrate"},
		&cobra.Group{ID: "introspect", Title: "introspection and protocol"},
	)

	initCmd := newInitCmd()
	initCmd.GroupID = "start"

	statusCmd := newStatusCmd()
	statusCmd.GroupID = "start"

	contextCmd := newContextCmd()
	contextCmd.GroupID = "start"

	fetchCmd := newFetchCmd()
	fetchCmd.GroupID = "explore"

	createCmd := newCreateCmd()
	createCmd.GroupID = "authoring"

	sourceCmd := newSourceCmd()
	sourceCmd.GroupID = "authoring"

	operationCmd := newOperationCmd()
	operationCmd.GroupID = "authoring"

	diffCmd := newDiffCmd()
	diffCmd.GroupID = "authoring"

	mergeCmd := newMergeCmd()
	mergeCmd.GroupID = "authoring"

	syncCmd := newSyncCmd()
	syncCmd.GroupID = "authoring"

	conflictsCmd := newConflictsCmd()
	conflictsCmd.GroupID = "authoring"

	bindingCmd := newBindingCmd()
	bindingCmd.GroupID = "authoring"

	codegenCmd := newCodegenCmd()
	codegenCmd.GroupID = "authoring"

	conformCmd := newConformCmd()
	conformCmd.GroupID = "authoring"

	formatsCmd := newFormatsCmd()
	formatsCmd.GroupID = "delegates"

	delegateCmd := newDelegateCmd()
	delegateCmd.GroupID = "delegates"

	infoCmd := newInfoCmd()
	infoCmd.GroupID = "introspect"

	validateCmd := newValidateCmd()
	validateCmd.GroupID = "introspect"

	compatCmd := newCompatCmd()
	compatCmd.GroupID = "introspect"

	mcpCmd := newMCPCmd()
	mcpCmd.GroupID = "serve"

	serveCmd := newServeCmd()
	serveCmd.GroupID = "serve"

	demoCmd := newDemoCmd()
	demoCmd.GroupID = "explore"

	root.AddCommand(
		initCmd,
		statusCmd,
		contextCmd,
		fetchCmd,
		createCmd,
		sourceCmd,
		operationCmd,
		bindingCmd,
		codegenCmd,
		conformCmd,
		syncCmd,
		conflictsCmd,
		diffCmd,
		mergeCmd,
		formatsCmd,
		delegateCmd,
		infoCmd,
		validateCmd,
		compatCmd,
		mcpCmd,
		serveCmd,
		demoCmd,
	)

	return root
}

// prettyCanonicalJSON outputs pretty-printed JSON with canonical key ordering.
func prettyCanonicalJSON(v any) ([]byte, error) {
	b, err := canonicaljson.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// canonicalYAML outputs YAML with consistent key ordering.
func canonicalYAML(v any) ([]byte, error) {
	b, err := canonicaljson.Marshal(v)
	if err != nil {
		return nil, err
	}
	var anyVal any
	if err := json.Unmarshal(b, &anyVal); err != nil {
		return nil, err
	}
	return yaml.Marshal(anyVal)
}
