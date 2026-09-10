package app

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// MarshalJSONAsYAML preserves the JSON value model when displaying JSON as
// YAML. Decoding through YAML nodes avoids float64 conversion and prevents
// json.Number's underlying Go string type from becoming a YAML string.
func MarshalJSONAsYAML(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return nil, err
	}
	var visit func(*yaml.Node)
	visit = func(n *yaml.Node) {
		// JSON string literals have quoted style. Unquoted JSON numeric
		// literals remain numbers even beyond YAML's native numeric range.
		if n.Kind == yaml.ScalarNode && n.Style&yaml.DoubleQuotedStyle == 0 && len(n.Value) > 0 && (n.Value[0] == '-' || n.Value[0] >= '0' && n.Value[0] <= '9') {
			n.Tag = "!!int"
			if strings.ContainsAny(n.Value, ".eE") {
				n.Tag = "!!float"
			}
		}
		n.Style = 0
		for _, child := range n.Content {
			visit(child)
		}
	}
	visit(&node)
	return yaml.Marshal(&node)
}
