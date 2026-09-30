package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// v02SampleOutput renders fixtures only. It never loads an OBI or content
// file, reads stdin, contacts a service, or writes an output destination.
func v02SampleOutput(cmd *cobra.Command, args []string) error {
	if cmd.Root().PersistentFlags().Changed("output") {
		return fmt.Errorf("--sample-output does not write files; omit -o")
	}
	format, err := v02OutputFormat(cmd)
	if err != nil {
		return err
	}
	if format != "" && format != "text" && format != "json" && format != "yaml" {
		return fmt.Errorf("-F must be text, json, or yaml")
	}
	if id := cmd.Annotations["surface-id"]; id == "binding.invoke" || id == "diff" {
		if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "SAMPLE OUTPUT — illustrative fixture only; no locator was read and no operation ran"); err != nil {
			return err
		}
		if id == "binding.invoke" {
			if cmd.Flags().Changed("input-stream") {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), `{"type":"value","binding":"`+args[1]+`","value":{"example":1}}`)
				if err != nil {
					return err
				}
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), `{"type":"complete","binding":"`+args[1]+`","illustrative":true}`)
			return err
		}
		if format == "" || format == "text" {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "changed /operations/get/output: example only")
			return err
		}
		result := map[string]any{"illustrative": true, "changes": []any{map[string]any{"path": "/operations/get/output", "before": nil, "after": map[string]any{"type": "object"}}}}
		bytes, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(bytes))
		return err
	}
	var result any
	switch cmd.Annotations["surface-id"] {
	case "new":
		doc := v02EmptyOBI()
		for _, flag := range []string{"name", "version", "description"} {
			if cmd.Name() == "init" && flag == "version" {
				flag = "interface-version"
			}
			if cmd.Flags().Changed(flag) {
				value, _ := cmd.Flags().GetString(flag)
				if (flag == "version" || flag == "interface-version") && value == "" {
					return fmt.Errorf("--version must be non-empty when present")
				}
				member := flag
				if flag == "interface-version" {
					member = "version"
				}
				doc[member] = value
			}
		}
		result = doc
	case "show":
		full := cmd.Flags().Lookup("full") == nil
		if cmd.Flags().Lookup("full") != nil {
			full, _ = cmd.Flags().GetBool("full")
		}
		if cmd.Flags().Lookup("summary") != nil {
			summary, _ := cmd.Flags().GetBool("summary")
			full = !summary
		}
		if full {
			result = v02IllustrativeOBI()
		} else {
			result = map[string]any{"illustrative": true, "openbindings": "0.2.0", "operations": 1, "sources": 1, "bindings": 1, "dependencies": 1}
		}
	case "source.add":
		kind, _ := cmd.Flags().GetString("kind")
		source := map[string]any{"kind": kind}
		if err := v02SampleOptionalValue(cmd, source, "content", "content"); err != nil {
			return err
		}
		if cmd.Flags().Changed("description") {
			value, _ := cmd.Flags().GetString("description")
			source["description"] = value
		}
		doc := v02EmptyOBI()
		doc["sources"] = map[string]any{args[1]: source}
		result = doc
	case "source.import":
		kind, _ := cmd.Flags().GetString("kind")
		doc := v02EmptyOBI()
		doc["sources"] = map[string]any{args[1]: map[string]any{
			"kind":    kind,
			"content": map[string]any{"example": "handler-defined content from an ob input locator"},
		}}
		result = doc
	case "source.pull":
		doc := v02EmptyOBI()
		doc["sources"] = map[string]any{args[1]: map[string]any{
			"kind": "example.private@1", "content": map[string]any{"example": "refreshed by a kind handler"},
		}}
		result = doc
	case "source.synthesize":
		apply, _ := cmd.Flags().GetBool("apply")
		if apply {
			doc := v02EmptyOBI()
			doc["operations"] = map[string]any{"example.get": map[string]any{}}
			doc["sources"] = map[string]any{args[1]: map[string]any{"kind": "example.private@1"}}
			doc["bindings"] = map[string]any{"example.get.binding": map[string]any{"operation": "example.get", "source": args[1]}}
			result = doc
		} else {
			result = []any{
				map[string]any{"op": "add", "path": "/operations/example.get", "value": map[string]any{}},
				map[string]any{"op": "add", "path": "/bindings/example.get.binding", "value": map[string]any{"operation": "example.get", "source": args[1]}},
			}
		}
	case "source.inspect":
		result = map[string]any{"illustrative": true, "source": args[1], "kind": "example.private@1", "report": map[string]any{"scope": "installed kind handler", "details": "handler-defined"}}
	case "binding.add":
		op, _ := cmd.Flags().GetString("operation")
		src, _ := cmd.Flags().GetString("source")
		binding := map[string]any{"operation": op, "source": src}
		if err := v02SampleOptionalValue(cmd, binding, "content", "content"); err != nil {
			return err
		}
		if cmd.Flags().Changed("preference") {
			value, _ := cmd.Flags().GetString("preference")
			var number json.Number = json.Number(value)
			binding["preference"] = number
		}
		if cmd.Flags().Changed("description") {
			value, _ := cmd.Flags().GetString("description")
			binding["description"] = value
		}
		if cmd.Flags().Changed("deprecated") {
			value, _ := cmd.Flags().GetBool("deprecated")
			binding["deprecated"] = value
		}
		doc := v02EmptyOBI()
		doc["operations"] = map[string]any{op: map[string]any{}}
		doc["sources"] = map[string]any{src: map[string]any{"kind": "example.private@1"}}
		doc["bindings"] = map[string]any{args[1]: binding}
		result = doc
	case "dependency.add":
		op, _ := cmd.Flags().GetString("operation")
		dependency := map[string]any{"operation": op}
		if cmd.Flags().Changed("kind") {
			kinds, _ := cmd.Flags().GetStringArray("kind")
			dependency["kinds"] = kinds
		}
		doc := v02EmptyOBI()
		doc["operations"] = map[string]any{op: map[string]any{}}
		doc["dependencies"] = map[string]any{args[1]: dependency}
		result = doc
	case "operation.add":
		operation := map[string]any{}
		for _, flag := range []string{"description", "idempotent", "input-schema", "output-schema"} {
			if !cmd.Flags().Changed(flag) {
				continue
			}
			switch flag {
			case "description":
				value, _ := cmd.Flags().GetString(flag)
				operation["description"] = value
			case "idempotent":
				if cmd.Flags().Lookup(flag).Value.Type() == "bool" {
					value, _ := cmd.Flags().GetBool(flag)
					operation["idempotent"] = value
				} else {
					value, _ := cmd.Flags().GetString(flag)
					operation["idempotent"] = value == "true"
				}
			default:
				value, _ := cmd.Flags().GetString(flag)
				if v02IsExternalValue(value) {
					return fmt.Errorf("--sample-output needs inline --%s JSON; it never reads a file or stdin", flag)
				}
				var decoded any
				_ = json.Unmarshal([]byte(value), &decoded)
				operation[strings.TrimSuffix(flag, "-schema")] = decoded
			}
		}
		if cmd.Flags().Changed("alias") {
			aliases, _ := cmd.Flags().GetStringArray("alias")
			operation["aliases"] = aliases
		}
		if cmd.Flags().Changed("tag") {
			tags, _ := cmd.Flags().GetStringArray("tag")
			operation["tags"] = tags
		}
		if cmd.Flags().Changed("deprecated") {
			deprecated, _ := cmd.Flags().GetBool("deprecated")
			operation["deprecated"] = deprecated
		}
		doc := v02EmptyOBI()
		doc["operations"] = map[string]any{args[1]: operation}
		result = doc
	case "schema.add":
		value, _ := cmd.Flags().GetString("value")
		if v02IsExternalValue(value) {
			return fmt.Errorf("--sample-output needs inline --value JSON; it never reads a file or stdin")
		}
		var decoded any
		_ = json.Unmarshal([]byte(value), &decoded)
		doc := v02EmptyOBI()
		doc["schemas"] = map[string]any{args[1]: decoded}
		result = doc
	case "source.show":
		result = map[string]any{"kind": "example.private@1", "content": map[string]any{"address": "https://api.example.test"}}
	case "source.list":
		full, _ := cmd.Flags().GetBool("full")
		if full {
			result = map[string]any{
				"api":     map[string]any{"kind": "example.private@1", "content": nil},
				"private": map[string]any{"kind": "uninstalled.private@1"},
			}
		} else {
			result = []any{map[string]any{"key": "api", "kind": "example.private@1"}}
		}
	case "binding.show":
		result = map[string]any{"operation": "get", "source": "api", "content": map[string]any{"target": "/items"}}
	case "binding.list":
		result = []any{map[string]any{"key": "get.http", "operation": "get", "source": "api"}}
	case "dependency.show":
		result = map[string]any{"operation": "charge", "kinds": []string{"example.private@1", "private.rpc@1"}}
	case "dependency.list":
		result = []any{map[string]any{"key": "billing", "operation": "charge"}}
	case "operation.show":
		result = map[string]any{"output": map[string]any{"type": "object"}, "aliases": []string{"example.get"}}
	case "operation.list":
		result = []any{map[string]any{"key": "get", "aliases": []string{"example.get"}}}
	case "schema.show":
		result = map[string]any{"type": "object"}
	case "schema.list":
		result = []any{map[string]any{"key": "Item"}}
	case "patch":
		result = v02IllustrativeOBI()
	case "source.remove", "binding.remove", "dependency.remove", "operation.remove", "schema.remove":
		result = v02EmptyOBI()
	case "validate":
		quiet, _ := cmd.Flags().GetBool("quiet")
		if quiet {
			return fmt.Errorf("--sample-output cannot illustrate a suppressed --quiet report")
		}
		result = map[string]any{"illustrative": true, "conclusion": "conformance undetermined", "ruleEvidence": []any{map[string]any{"rule": "OBI-D-01", "state": "inconclusive", "reason": "fixture does not include original document bytes"}}}
	case "kind.check":
		action, _ := cmd.Flags().GetString("action")
		available := []string{}
		if args[0] == "example.private@1" {
			available = []string{"inspect", "synthesize"}
		}
		if action == "" {
			result = map[string]any{"illustrative": true, "kind": args[0], "actions": available, "scope": "this installation only"}
		} else {
			supported := false
			for _, candidate := range available {
				if candidate == action {
					supported = true
				}
			}
			result = map[string]any{"illustrative": true, "kind": args[0], "action": action, "supported": supported, "scope": "this installation only"}
		}
	case "kind.list":
		result = map[string]any{"illustrative": true, "scope": "this installation only", "kinds": []any{map[string]any{"kind": "example.private@1", "actions": []string{"inspect", "synthesize"}}}}
	default:
		return fmt.Errorf("no illustrative result for %s; help and parsing remain explorable", cmd.CommandPath())
	}
	if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "SAMPLE OUTPUT — illustrative fixture only; no locator was read and no operation ran"); err != nil {
		return err
	}
	if format == "yaml" {
		bytes, err := canonicalYAML(result)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.TrimRight(string(bytes), "\n"))
		return err
	}
	bytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(bytes))
	return err
}

func v02SampleOptionalValue(cmd *cobra.Command, object map[string]any, flag, member string) error {
	if !cmd.Flags().Changed(flag) {
		return nil
	}
	value, _ := cmd.Flags().GetString(flag)
	if v02IsExternalValue(value) {
		return fmt.Errorf("--sample-output needs inline --%s JSON; it never reads a file or stdin", flag)
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return err
	}
	object[member] = decoded
	return nil
}

func v02EmptyOBI() map[string]any {
	return map[string]any{"openbindings": "0.2.0", "operations": map[string]any{}}
}

func v02IllustrativeOBI() map[string]any {
	return map[string]any{
		"openbindings": "0.2.0",
		"operations":   map[string]any{"get": map[string]any{"output": map[string]any{"type": "object"}}},
		"sources":      map[string]any{"api": map[string]any{"kind": "example.private@1", "content": map[string]any{"address": "https://api.example.test"}}},
		"bindings":     map[string]any{"get.http": map[string]any{"operation": "get", "source": "api", "content": map[string]any{"target": "/items"}}},
		"dependencies": map[string]any{"billing": map[string]any{"operation": "get", "kinds": []string{"example.private@1"}}},
	}
}
