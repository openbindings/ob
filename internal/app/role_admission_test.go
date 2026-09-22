package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// interfacesCorpusPath locates a file of the selected interfaces conformance
// corpus. OB_INTERFACES_CORPUS names the conformance root of the exact
// interfaces checkout CI selected; a sibling checkout is only a developer
// convenience and is refused when OB_CORPUS_REQUIRED is set, so "no workspace"
// never silently becomes "no corpus" or "some other corpus".
func interfacesCorpusPath(t *testing.T, relative ...string) string {
	t.Helper()
	root := os.Getenv("OB_INTERFACES_CORPUS")
	if root == "" {
		if os.Getenv("OB_CORPUS_REQUIRED") != "" {
			t.Fatal("OB_CORPUS_REQUIRED is set but OB_INTERFACES_CORPUS names no interfaces conformance root")
		}
		root = filepath.Join("..", "..", "..", "interfaces", "conformance")
	}
	path := filepath.Join(append([]string{root}, relative...)...)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("required interfaces corpus unavailable at %s: %v", path, err)
	}
	return path
}

func TestRoleAdmissionCorpus(t *testing.T) {
	data, err := os.ReadFile(interfacesCorpusPath(t, "delegate-manager", "admission.json"))
	if err != nil {
		t.Fatal("required manager corpus unavailable:", err)
	}
	var fixture struct {
		Cases []struct {
			ID             string          `json:"id"`
			Roles          []DelegateRole  `json:"roles"`
			Interface      json.RawMessage `json:"interface"`
			RequestedRoles []string        `json:"requestedRoles"`
			Expected       struct {
				Admitted bool `json:"admitted"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 17 {
		t.Fatalf("expected 17 frozen cases, got %d", len(fixture.Cases))
	}
	for _, c := range fixture.Cases {
		t.Run(c.ID, func(t *testing.T) {
			catalogue, err := newRoleCatalogue(c.Roles)
			if err != nil {
				t.Fatal(err)
			}
			routes, err := catalogue.admit(c.Interface, c.RequestedRoles)
			if (err == nil) != c.Expected.Admitted {
				t.Fatalf("admitted=%v, want %v: %v", err == nil, c.Expected.Admitted, err)
			}
			if err == nil && len(routes) != len(c.RequestedRoles) {
				t.Fatal("enrollment differs from requested roles")
			}
			if c.ID == "qualified-correspondence-with-bare-decoy" && err == nil {
				for _, route := range routes {
					for expected, actual := range route.Operations {
						if actual == "read" {
							t.Fatalf("bare-name decoy selected for %s", expected)
						}
					}
				}
			}
		})
	}
}

func TestRoleAdmissionSchemaEvidence(t *testing.T) {
	for _, tc := range []struct {
		required, provided string
		admit              bool
	}{
		{`{}`, `{}`, true}, {`true`, `{}`, true}, {`false`, `false`, true},
		{`false`, `{}`, false}, {`{}`, `false`, false}, {`{}`, `null`, false},
		{`{"$ref":"#/schemas/missing"}`, `{}`, false},
		// Identical schemas establish compatibility by identity, even when
		// their constraints are outside the directional comparison profile.
		{`{"pattern":"x"}`, `{"pattern":"x"}`, true},
		{`{"type":"string"}`, `"malformed"`, false},
	} {
		t.Run(tc.required+"/"+tc.provided, func(t *testing.T) {
			expected := json.RawMessage(fmt.Sprintf(`{"openbindings":"0.2.0","operations":{"op":{"input":%s,"output":false}}}`, tc.required))
			provider := json.RawMessage(fmt.Sprintf(`{"openbindings":"0.2.0","operations":{"op":{"input":%s,"output":false}}}`, tc.provided))
			c, err := newRoleCatalogue([]DelegateRole{{ID: "A", Description: "test", AcceptedInterfaces: []json.RawMessage{expected}}})
			if err != nil {
				if !tc.admit {
					return
				} // malformed expectations refuse at discovery
				t.Fatal(err)
			}
			_, err = c.admit(provider, []string{"A"})
			if (err == nil) != tc.admit {
				t.Fatalf("admission=%v, want %v: %v", err == nil, tc.admit, err)
			}
		})
	}
}

func TestRoleAdmissionEvidence(t *testing.T) {
	const expected = `{"openbindings":"0.2.0","operations":{"example.read":{"input":{"type":"integer","enum":[9007199254740992]},"output":{"type":"string"}}}}`
	const supplied = `{"openbindings":"0.2.0","operations":{"read":{"aliases":["example.read"],"input":{"type":"integer","enum":[9007199254740993]},"output":{"type":"string"}}}}`
	c, err := newRoleCatalogue([]DelegateRole{{ID: "A", Description: "test", AcceptedInterfaces: []json.RawMessage{json.RawMessage(expected)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.admit(json.RawMessage(supplied), []string{"A"}); err == nil {
		t.Fatal("disjoint exact inputs admitted")
	}
}
