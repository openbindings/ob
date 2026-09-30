package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// The surface contract classifies every operational leaf in the broad variant.
// A compact variant retains each leaf's original contract ID when it moves.
type surfaceOutputMode string

const (
	surfaceDocument      surfaceOutputMode = "document"
	surfaceEditOBI       surfaceOutputMode = "edit-obi"
	surfaceReport        surfaceOutputMode = "report"
	surfaceEditConfig    surfaceOutputMode = "edit-config"
	surfaceStream        surfaceOutputMode = "stream"
	surfaceBindingInvoke surfaceOutputMode = "binding-invoke"
	surfaceProcess       surfaceOutputMode = "process"
	surfaceCode          surfaceOutputMode = "code"
)

var surfaceContracts = map[string]surfaceOutputMode{
	"ob new":                       surfaceDocument,
	"ob resolve":                   surfaceDocument,
	"ob synthesize":                surfaceDocument,
	"ob delegate requirements":     surfaceDocument,
	"ob binding add":               surfaceEditOBI,
	"ob binding remove":            surfaceEditOBI,
	"ob conform":                   surfaceEditOBI,
	"ob dependency add":            surfaceEditOBI,
	"ob dependency remove":         surfaceEditOBI,
	"ob merge":                     surfaceEditOBI,
	"ob meta set":                  surfaceEditOBI,
	"ob operation add":             surfaceEditOBI,
	"ob operation alias add":       surfaceEditOBI,
	"ob operation alias remove":    surfaceEditOBI,
	"ob operation bind":            surfaceEditOBI,
	"ob operation codegen-name":    surfaceEditOBI,
	"ob operation detach":          surfaceEditOBI,
	"ob operation output-schema":   surfaceEditOBI,
	"ob operation remove":          surfaceEditOBI,
	"ob operation rename":          surfaceEditOBI,
	"ob operation set":             surfaceEditOBI,
	"ob operation unbind":          surfaceEditOBI,
	"ob patch":                     surfaceEditOBI,
	"ob strip-ob-metadata":         surfaceEditOBI,
	"ob source add":                surfaceEditOBI,
	"ob source pull":               surfaceEditOBI,
	"ob source remove":             surfaceEditOBI,
	"ob binding list":              surfaceReport,
	"ob binding preflight":         surfaceReport,
	"ob binding show":              surfaceReport,
	"ob binding-specs check":       surfaceReport,
	"ob binding-specs list":        surfaceReport,
	"ob compat":                    surfaceReport,
	"ob context get":               surfaceReport,
	"ob context list":              surfaceReport,
	"ob delegate list":             surfaceReport,
	"ob delegate migrate preview":  surfaceReport,
	"ob delegate resolve":          surfaceReport,
	"ob delegate roles":            surfaceReport,
	"ob about":                     surfaceReport,
	"ob diff":                      surfaceReport,
	"ob environment":               surfaceReport,
	"ob inspect":                   surfaceReport,
	"ob operation alias list":      surfaceReport,
	"ob operation list":            surfaceReport,
	"ob operation preflight":       surfaceReport,
	"ob operation show":            surfaceReport,
	"ob dependency list":           surfaceReport,
	"ob dependency show":           surfaceReport,
	"ob source list":               surfaceReport,
	"ob source show":               surfaceReport,
	"ob show":                      surfaceReport,
	"ob status":                    surfaceReport,
	"ob validate":                  surfaceReport,
	"ob context remove":            surfaceEditConfig,
	"ob context set":               surfaceEditConfig,
	"ob delegate migrate apply":    surfaceEditConfig,
	"ob delegate migrate rollback": surfaceEditConfig,
	"ob delegate prefer":           surfaceEditConfig,
	"ob delegate register":         surfaceEditConfig,
	"ob delegate unregister":       surfaceEditConfig,
	"ob init":                      surfaceEditConfig,
	"ob invoke":                    surfaceStream,
	"ob operation invoke":          surfaceStream,
	"ob binding invoke":            surfaceBindingInvoke,
	"ob demo":                      surfaceProcess,
	"ob mcp":                       surfaceProcess,
	"ob start":                     surfaceProcess,
	"ob codegen":                   surfaceCode,
}

func surfaceContractID(cmd *cobra.Command) string {
	if cmd.Annotations != nil && cmd.Annotations["surface-contract-id"] != "" {
		return cmd.Annotations["surface-contract-id"]
	}
	return cmd.CommandPath()
}

func enforceSurfaceOutputContract(cmd *cobra.Command, args []string) error {
	id := surfaceContractID(cmd)
	mode, ok := surfaceContracts[id]
	if !ok {
		return fmt.Errorf("%s has no surface output contract", cmd.CommandPath())
	}
	global := cmd.Root().PersistentFlags()
	format, _ := global.GetString("format")
	if global.Changed("format") && format != "text" && format != "json" && format != "yaml" {
		return fmt.Errorf("%s: -F must be text, json, or yaml", cmd.CommandPath())
	}
	switch mode {
	case surfaceProcess:
		for _, name := range []string{"output", "format"} {
			if global.Changed(name) {
				return fmt.Errorf("%s does not accept --%s", cmd.CommandPath(), name)
			}
		}
	case surfaceEditConfig:
		if global.Changed("output") {
			return fmt.Errorf("%s changes the active configuration; -o is not an alternate config destination", cmd.CommandPath())
		}
	case surfaceStream:
		if global.Changed("output") {
			return fmt.Errorf("%s streams values; redirect stdout to save them", cmd.CommandPath())
		}
		envelope, _ := cmd.Flags().GetBool("envelope")
		if !envelope && global.Changed("format") && format != "json" {
			return fmt.Errorf("%s streams JSON values; -F yaml or text requires --envelope", cmd.CommandPath())
		}
	case surfaceBindingInvoke:
		if len(args) == 0 {
			if global.Changed("output") || global.Changed("format") {
				return fmt.Errorf("%s machine lane writes one JSON envelope to stdout; -o and -F do not apply", cmd.CommandPath())
			}
		}
	case surfaceDocument:
		if format == "text" {
			return fmt.Errorf("%s produces an OBI document; use -F json or yaml", cmd.CommandPath())
		}
		if format == "yaml" && global.Changed("output") {
			return fmt.Errorf("%s -F yaml is a stdout view; -o writes canonical JSON, so omit -F yaml or -o", cmd.CommandPath())
		}
	}
	if id == "ob strip-ob-metadata" {
		check, _ := cmd.Flags().GetBool("check")
		if check && global.Changed("output") {
			return fmt.Errorf("%s --check writes no document and does not accept -o", cmd.CommandPath())
		}
	}
	if flag := cmd.Flags().Lookup("quiet"); flag != nil && flag.Changed {
		if global.Changed("output") || global.Changed("format") {
			return fmt.Errorf("%s --quiet emits no result; -o and -F do not apply", cmd.CommandPath())
		}
	}
	return nil
}
