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
		Use:   "ob",
		Short: "openbindings: one interface · limitless bindings",
		Long: `ob is the OpenBindings CLI: author, validate, compare, and invoke
OBIs — OpenBindings interface documents, portable descriptions of a
service's operations independent of the protocols that carry them.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		// `ob --version` is the conventional identity probe; `ob describe`
		// carries the full identity block.
		Version: fmt.Sprintf("%s (OpenBindings spec %s)", app.OBVersion, app.SpecRange()),
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
		&cobra.Group{ID: "setup", Title: "set up a working area"},
		&cobra.Group{ID: "explore", Title: "browse and interact"},
		&cobra.Group{ID: "authoring", Title: "interface authoring"},
		&cobra.Group{ID: "delegates", Title: "delegates and formats"},
		&cobra.Group{ID: "serve", Title: "serve and integrate"},
		&cobra.Group{ID: "introspect", Title: "introspection and protocol"},
	)

	initCmd := newInitCmd()
	initCmd.GroupID = "setup"

	environmentCmd := newEnvironmentCmd()
	environmentCmd.GroupID = "setup"

	contextCmd := newContextCmd()
	contextCmd.GroupID = "setup"

	statusCmd := newStatusCmd()
	statusCmd.GroupID = "authoring"

	resolveCmd := newResolveCmd()
	resolveCmd.GroupID = "explore"

	newCmd := newNewCmd()
	newCmd.GroupID = "authoring"

	metaCmd := newMetaCmd()
	metaCmd.GroupID = "authoring"

	inspectCmd := newInspectCmd()
	inspectCmd.GroupID = "authoring"

	synthesizeCmd := newSynthesizeCmd()
	synthesizeCmd.GroupID = "authoring"

	sourceCmd := newSourceCmd()
	sourceCmd.GroupID = "authoring"

	operationCmd := newOperationCmd()
	operationCmd.GroupID = "authoring"

	diffCmd := newDiffCmd()
	diffCmd.GroupID = "authoring"

	mergeCmd := newMergeCmd()
	mergeCmd.GroupID = "authoring"

	bindingCmd := newBindingCmd()
	bindingCmd.GroupID = "authoring"

	codegenCmd := newCodegenCmd()
	codegenCmd.GroupID = "authoring"

	conformCmd := newConformCmd()
	conformCmd.GroupID = "authoring"

	bindingSpecsCmd := newBindingSpecsCmd()
	bindingSpecsCmd.GroupID = "delegates"

	delegateCmd := newDelegateCmd()
	delegateCmd.GroupID = "delegates"

	describeCmd := newDescribeCmd()
	describeCmd.GroupID = "introspect"

	validateCmd := newValidateCmd()
	validateCmd.GroupID = "introspect"

	compatCmd := newCompatCmd()
	compatCmd.GroupID = "introspect"

	mcpCmd := newMCPCmd()
	mcpCmd.GroupID = "serve"

	startCmd := newStartCmd()
	startCmd.GroupID = "serve"

	demoCmd := newDemoCmd()
	demoCmd.GroupID = "explore"

	purifyCmd := newPurifyCmd()
	purifyCmd.GroupID = "authoring"

	root.AddCommand(
		initCmd,
		environmentCmd,
		statusCmd,
		contextCmd,
		resolveCmd,
		newCmd,
		metaCmd,
		inspectCmd,
		synthesizeCmd,
		sourceCmd,
		operationCmd,
		bindingCmd,
		codegenCmd,
		conformCmd,
		diffCmd,
		mergeCmd,
		bindingSpecsCmd,
		delegateCmd,
		describeCmd,
		validateCmd,
		compatCmd,
		mcpCmd,
		startCmd,
		demoCmd,
		purifyCmd,
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
