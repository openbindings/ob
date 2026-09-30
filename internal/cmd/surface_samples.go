package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// surfaceSampleOutput presents fixture results for reviewing the CLI facade.
// It never loads the user's locator or writes an output file.
func surfaceSampleOutput(cmd *cobra.Command, args []string) error {
	format, outputPath := getOutputFlags(cmd)
	if outputPath != "" || (cmd.Flags().Lookup("output") != nil && cmd.Flags().Lookup("output").Changed) {
		return fmt.Errorf("--sample-output never writes files; omit -o to view the illustrative result")
	}
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" && format != "yaml" {
		return fmt.Errorf("sample output supports -F text, json, or yaml")
	}

	var result any
	var textResult string
	switch surfaceContractID(cmd) {
	case "ob show":
		full, _ := cmd.Flags().GetBool("full")
		if full {
			result = sampleOBI()
			textResult = ""
		} else {
			result = map[string]any{
				"locator": args[0], "openbindings": "0.2.0",
				"operations": 2, "sources": 1, "bindings": 1, "dependencies": 1,
			}
			textResult = fmt.Sprintf("%s  OpenBindings 0.2.0\n2 operations  1 source  1 binding  1 dependency\n", args[0])
		}
	case "ob source show":
		result = map[string]any{"key": args[1], "bindingSpec": "openbindings.openapi-3.1@1", "location": "https://example.test/openapi.json"}
		textResult = fmt.Sprintf("%s  openbindings.openapi-3.1@1\nlocation: https://example.test/openapi.json\n", args[1])
		if full, _ := cmd.Flags().GetBool("full"); full {
			result = map[string]any{"bindingSpec": "openbindings.openapi-3.1@1", "location": "https://example.test/openapi.json"}
			textResult = ""
		}
	case "ob operation show":
		result = map[string]any{"key": args[1], "bindings": []string{"listPets.http"}, "dependencies": []string{}}
		textResult = fmt.Sprintf("%s\nbindings: listPets.http\n", args[1])
		if full, _ := cmd.Flags().GetBool("full"); full {
			result = map[string]any{}
			textResult = ""
		}
	case "ob binding show":
		result = map[string]any{"key": args[1], "operation": "listPets", "source": "api", "selector": "#/paths/~1pets/get"}
		textResult = fmt.Sprintf("%s  listPets via api\nselector: #/paths/~1pets/get\n", args[1])
		if full, _ := cmd.Flags().GetBool("full"); full {
			result = map[string]any{"operation": "listPets", "source": "api", "selector": "#/paths/~1pets/get"}
			textResult = ""
		}
	case "ob dependency show":
		result = map[string]any{"key": args[1], "operation": "chargeCard", "bindingSpecs": []string{}}
		textResult = fmt.Sprintf("%s  consumes chargeCard\nbinding specifications: unconstrained\n", args[1])
		if full, _ := cmd.Flags().GetBool("full"); full {
			result = map[string]any{"operation": "chargeCard"}
			textResult = ""
		}
	case "ob dependency list":
		result = []any{map[string]any{"key": "billing", "operation": "chargeCard"}}
		textResult = "billing  chargeCard\n"
	case "ob binding list":
		result = []any{map[string]any{"key": "listPets.http", "operation": "listPets", "source": "api"}}
		textResult = "listPets.http  listPets via api\n"
	case "ob source add":
		source := "inline source"
		if len(args) == 2 {
			source = args[1]
		}
		pull, _ := cmd.Flags().GetBool("pull")
		result = map[string]any{"action": "source.add", "source": source, "derived": pull}
		textResult = fmt.Sprintf("Registered source %s\n", source)
		if pull {
			textResult += "Derived its operations and bindings\n"
		}
	case "ob binding add":
		operation, _ := cmd.Flags().GetString("operation")
		source, _ := cmd.Flags().GetString("source")
		result = map[string]any{"action": "binding.add", "key": args[1], "operation": operation, "source": source}
		textResult = fmt.Sprintf("Added binding %s: %s via %s\n", args[1], operation, source)
	case "ob dependency add":
		operation, _ := cmd.Flags().GetString("operation")
		result = map[string]any{"action": "dependency.add", "key": args[1], "operation": operation}
		if bindingSpecs, _ := cmd.Flags().GetStringArray("binding-spec"); len(bindingSpecs) != 0 {
			result.(map[string]any)["bindingSpecs"] = bindingSpecs
		}
		textResult = fmt.Sprintf("Added dependency %s: consumes %s\n", args[1], operation)
	case "ob patch":
		result = map[string]any{"action": "patch", "obi": args[0], "patch": args[1], "valid": true}
		textResult = fmt.Sprintf("Patched %s; resulting OBI is valid\n", args[0])
	case "ob validate":
		result = map[string]any{"locator": args[0], "conformance": "conformant", "diagnostics": []any{}}
		textResult = fmt.Sprintf("%s: conformant\n", args[0])
	case "ob status":
		result = map[string]any{"obi": args[0], "sourceDrift": false, "sourcesChecked": 1}
		textResult = fmt.Sprintf("%s: sources are in sync (1 checked)\n", args[0])
	case "ob compat":
		result = map[string]any{"target": args[0], "candidate": args[1], "policy": "ob-comparison-report/v1", "verdict": "compatible"}
		textResult = fmt.Sprintf("%s is compatible with %s under ob policy\n", args[1], args[0])
	case "ob codegen":
		language, _ := cmd.Flags().GetString("lang")
		code := "// illustrative generated " + language + " invoker\n"
		result = code
		textResult = code
		if envelope, _ := cmd.Flags().GetBool("envelope"); envelope {
			result = map[string]any{"language": language, "code": code}
			textResult = ""
		}
	case "ob context get":
		secret := "********"
		if reveal, _ := cmd.Flags().GetBool("reveal-secrets"); reveal {
			secret = "sample-secret"
		}
		result = map[string]any{"target": args[0], "bearerToken": secret}
		textResult = fmt.Sprintf("%s\nbearer token: %s\n", args[0], secret)
	case "ob new":
		result = map[string]any{"openbindings": "0.2.0", "operations": map[string]any{}}
		for _, name := range []string{"name", "version", "description"} {
			if value, _ := cmd.Flags().GetString(name); value != "" {
				result.(map[string]any)[name] = value
			}
		}
	case "ob synthesize":
		result = sampleOBI()
		delete(result.(map[string]any), "dependencies")
		delete(result.(map[string]any)["operations"].(map[string]any), "chargeCard")
	case "ob resolve":
		result = sampleOBI()
	case "ob invoke", "ob operation invoke":
		event := map[string]any{"pets": []any{map[string]any{"name": "Mika"}}}
		envelope, _ := cmd.Flags().GetBool("envelope")
		if envelope {
			result = map[string]any{"events": []any{event}, "complete": true, "truncated": false}
		} else {
			result = event
		}
		// The default stream is JSON lines, including when -F is omitted.
		if format == "text" {
			format = "json"
		}
		if !envelope && format == "json" {
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "SAMPLE OUTPUT — illustrative only; no command ran"); err != nil {
				return err
			}
			line, err := json.Marshal(result)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(line))
			return err
		}
	case "ob binding invoke":
		value := map[string]any{"pets": []any{map[string]any{"name": "Mika"}}}
		if len(args) == 0 {
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "SAMPLE OUTPUT — illustrative only; no command ran"); err != nil {
				return err
			}
			line, err := json.Marshal(map[string]any{"output": value})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(line))
			return err
		}
		result = value
	default:
		return fmt.Errorf("no illustrative output is defined for %s; its parser and help are still explorable", cmd.CommandPath())
	}

	if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "SAMPLE OUTPUT — illustrative only; no command ran"); err != nil {
		return err
	}
	if format == "text" && textResult != "" {
		_, err := fmt.Fprint(cmd.OutOrStdout(), textResult)
		return err
	}
	var bytes []byte
	var err error
	if format == "yaml" {
		bytes, err = canonicalYAML(result)
	} else {
		bytes, err = prettyCanonicalJSON(result)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.TrimRight(string(bytes), "\n"))
	return err
}

func sampleOBI() map[string]any {
	return map[string]any{
		"openbindings": "0.2.0",
		"operations": map[string]any{
			"listPets":   map[string]any{},
			"chargeCard": map[string]any{},
		},
		"sources": map[string]any{
			"api": map[string]any{
				"bindingSpec": "openbindings.openapi-3.1@1",
				"location":    "https://example.test/openapi.json",
			},
		},
		"bindings": map[string]any{
			"listPets.http": map[string]any{"operation": "listPets", "source": "api", "selector": "#/paths/~1pets/get"},
		},
		"dependencies": map[string]any{
			"billing": map[string]any{"operation": "chargeCard"},
		},
	}
}
