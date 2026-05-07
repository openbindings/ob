package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

type comparisonFixture struct {
	Left    json.RawMessage `json:"left"`
	Right   json.RawMessage `json:"right"`
	Mode    string          `json:"mode"`
	Options struct {
		Profile string `json:"profile"`
	} `json:"options"`
	Suppressions struct {
		Suppressions []SuppressionRule `json:"suppressions"`
	} `json:"suppressions"`
	Expected json.RawMessage `json:"expected"`
}

func TestComparisonConformanceCorpus(t *testing.T) {
	root := findComparisonCorpus(t)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" || d.Name() == "manifest.json" {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		t.Run(rel, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fixture comparisonFixture
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}

			var left, right openbindings.Interface
			if err := json.Unmarshal(fixture.Left, &left); err != nil {
				t.Fatalf("left: %v", err)
			}
			if err := json.Unmarshal(fixture.Right, &right); err != nil {
				t.Fatalf("right: %v", err)
			}

			report := CompareInterfaces(CompareInterfacesInput{
				Left: resolvedComparisonInput{
					locator: "<input.left.uri>",
					iface:   &left,
				},
				Right: resolvedComparisonInput{
					locator: "<input.right.uri>",
					iface:   &right,
				},
				Mode:         fixture.Mode,
				Profile:      fixture.Options.Profile,
				GeneratedAt:  "<generated_at>",
				ToolVersion:  "<tool.version>",
				ProfileHash:  "<corpus-sha>",
				Suppressions: fixture.Suppressions.Suppressions,
			})

			var actual, expected any
			actualBytes, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(actualBytes, &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fixture.Expected, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				actualPretty, _ := json.MarshalIndent(actual, "", "  ")
				expectedPretty, _ := json.MarshalIndent(expected, "", "  ")
				t.Fatalf("report mismatch\nexpected:\n%s\nactual:\n%s", expectedPretty, actualPretty)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func findComparisonCorpus(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		"../spec/conformance/comparison",
		"../../spec/conformance/comparison",
		"../../../spec/conformance/comparison",
	} {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	t.Fatal("comparison conformance corpus not found")
	return ""
}
