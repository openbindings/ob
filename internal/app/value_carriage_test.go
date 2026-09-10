package app

import (
	"encoding/json"
	openbindings "github.com/openbindings/openbindings-go"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInterfaceFilePreservesExactOrdinaryValues(t *testing.T) {
	for _, token := range []string{"9007199254740993", "1.234567890123456789", "1e400", "1e-400"} {
		t.Run(token, func(t *testing.T) {
			var iface openbindings.Interface
			raw := `{"openbindings":"0.2.0","operations":{"read":{"output":{"const":` + token + `}}},"x-value":` + token + `}`
			if err := json.Unmarshal([]byte(raw), &iface); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "exact.obi.json")
			if err := WriteInterfaceFile(path, &iface); err != nil {
				t.Fatal(err)
			}
			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(written), token) != 2 {
				t.Fatalf("source values changed: %s", written)
			}
			info, _ := os.Stat(path)
			if err := WriteInterfaceFile(path, &iface); err != nil {
				t.Fatal(err)
			}
			after, _ := os.Stat(path)
			if after.ModTime() != info.ModTime() {
				t.Fatal("unchanged retained document was rewritten")
			}
		})
	}
}

func TestContextExactFileReload(t *testing.T) {
	dir := setupContextTestDir(t)
	key := "https://value-fidelity.invalid/reload"
	for _, token := range []string{"9007199254740993", "1.234567890123456789", "1e400", "1e-400"} {
		raw := `{"configuration":{"saved":` + token + `}}`
		if err := os.WriteFile(filepath.Join(dir, contextFilename(key)), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadContextConfig(key)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Configuration["saved"] != json.Number(token) {
			t.Fatalf("lost %s: %#v", token, cfg.Configuration)
		}
		if err := SaveContextConfig(key, cfg); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, contextFilename(key)))
		if err != nil || !strings.Contains(string(data), token) {
			t.Fatalf("bad persisted value: %s, %v", data, err)
		}
	}
}

func TestYAMLDisplayPreservesJSONNumbers(t *testing.T) {
	raw := json.RawMessage(`{"wide":9007199254740993,"huge":1e400,"tiny":1e-400,"numericString":"9007199254740993","null":null}`)
	data, err := MarshalJSONAsYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		t.Fatal(err)
	}
	members := node.Content[0].Content
	for i := 0; i < len(members); i += 2 {
		name, value := members[i].Value, members[i+1]
		if name == "numericString" {
			if value.Tag != "!!str" {
				t.Fatal("changed numeric string")
			}
			continue
		}
		if name == "null" {
			if value.Tag != "!!null" {
				t.Fatal("changed null")
			}
			continue
		}
		if value.Tag == "!!str" {
			t.Fatalf("number became string: %s", data)
		}
	}
}
