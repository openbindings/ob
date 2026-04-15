package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// formatForPath returns the output format to use when writing to a file path.
// Uses path extension when format is empty or "text": .yaml/.yml → yaml, else json.
func formatForPath(path, format string) OutputFormat {
	if format != "" && format != "text" {
		if f, err := ParseOutputFormat(format); err == nil && f != OutputFormatText {
			return f
		}
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return OutputFormatYAML
	}
	return OutputFormatJSON
}

// OutputFormat represents a supported output format.
type OutputFormat string

const (
	OutputFormatJSON OutputFormat = "json"
	OutputFormatYAML OutputFormat = "yaml"
	OutputFormatText OutputFormat = "text"
)

// FormatOutput serializes v to the specified output format.
// JSON output is pretty-printed (indented).
func FormatOutput(v any, format OutputFormat) ([]byte, error) {
	switch format {
	case OutputFormatJSON:
		return json.MarshalIndent(v, "", "  ")
	case OutputFormatYAML:
		// Round-trip via JSON so extension/lossless fields (e.g. x-ob) become
		// proper YAML objects instead of byte sequences (openbindings-go uses json.RawMessage).
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var tmp any
		if err := json.Unmarshal(b, &tmp); err != nil {
			return nil, err
		}
		return yaml.Marshal(tmp)
	default:
		return nil, fmt.Errorf("unsupported output format: %s", format)
	}
}

// ParseOutputFormat parses a string into an OutputFormat.
// Supports "text" as default human-readable format.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch s {
	case "", "text":
		return OutputFormatText, nil
	case "json":
		return OutputFormatJSON, nil
	case "yaml", "yml":
		return OutputFormatYAML, nil
	default:
		return "", fmt.Errorf("unknown output format %q (valid: text, json, yaml)", s)
	}
}

// Renderable is implemented by types that can render human-friendly output.
type Renderable interface {
	Render() string
}

// outputResultCore is the shared implementation for all output helpers.
// format: json|yaml|text|quiet (from --format). outputPath: from -o/--output; when set, write to file.
func outputResultCore(v any, format string, outputPath string, code int, textFn func() string, defaultFormat ...OutputFormat) error {
	if format == "quiet" {
		return ExitResult{Code: code, Message: "", ToStderr: false}
	}

	// If writing to file, serialize and write.
	if outputPath != "" {
		outFormat := formatForPath(outputPath, format)
		var b []byte
		var err error
		if outFormat == OutputFormatText {
			b = []byte(renderText(v, textFn))
		} else {
			b, err = FormatOutput(v, outFormat)
			if err != nil {
				return err
			}
		}
		if err := AtomicWriteFile(outputPath, b, FilePerm); err != nil {
			return ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
		}
		return ExitResult{Code: code, Message: "Wrote " + outputPath, ToStderr: false}
	}

	// Stdout: explicit format
	if format != "" {
		outFormat, err := ParseOutputFormat(format)
		if err != nil {
			return ExitResult{Code: 2, Message: err.Error(), ToStderr: true}
		}
		if outFormat == OutputFormatText {
			return ExitResult{Code: code, Message: renderText(v, textFn), ToStderr: false}
		}
		b, err := FormatOutput(v, outFormat)
		if err != nil {
			return err
		}
		return ExitResult{Code: code, Message: string(b), ToStderr: false}
	}

	// Stdout: default format
	if len(defaultFormat) > 0 && defaultFormat[0] != OutputFormatText {
		b, err := FormatOutput(v, defaultFormat[0])
		if err != nil {
			return err
		}
		return ExitResult{Code: code, Message: string(b), ToStderr: false}
	}

	// Stdout: text
	return ExitResult{Code: code, Message: renderText(v, textFn), ToStderr: false}
}

// renderText produces human-readable text from a value. Priority:
// textFn (if provided) > Renderable interface > JSON fallback.
func renderText(v any, textFn func() string) string {
	if textFn != nil {
		return strings.TrimRight(textFn(), "\n")
	}
	if r, ok := v.(Renderable); ok {
		return r.Render()
	}
	b, err := FormatOutput(v, OutputFormatJSON)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// OutputResult handles common output formatting logic for CLI commands.
// format: from --format (json|yaml|text|quiet). outputPath: from -o/--output; when set, write to file.
// If defaultFormat is provided and format is empty, uses that format for stdout.
func OutputResult(v any, format string, outputPath string, defaultFormat ...OutputFormat) error {
	return outputResultCore(v, format, outputPath, 0, nil, defaultFormat...)
}

// OutputResultWithCode is like OutputResult but allows the caller to specify
// a non-zero exit code. Useful for commands like "compat" where the output
// is valid data (not an error) but the process should exit non-zero.
// If format is "quiet", suppresses all output and returns only the exit code.
func OutputResultWithCode(v any, format string, outputPath string, code int) error {
	return outputResultCore(v, format, outputPath, code, nil)
}

// OutputResultText handles output formatting with an explicit text rendering function.
// If format is empty, calls textFn for stdout. If outputPath is set, writes to file.
func OutputResultText(result any, format string, outputPath string, textFn func() string) error {
	return outputResultCore(result, format, outputPath, 0, textFn)
}