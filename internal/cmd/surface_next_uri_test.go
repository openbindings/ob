package cmd

import (
	"strings"
	"testing"
)

func TestNextURIReferenceGrammar(t *testing.T) {
	for _, raw := range []string{
		"", "#", "#/schemas/Task", "https://user%41@example.com/a%2fb?x=%AF#part",
		"HTTPS://EXAMPLE.com/a", "urn:example:a/../b", "../relative", "//host/a",
		"https://[2001:db8::1]:443/a", "https://[vA.future:address]/a", "scheme:",
		"https://host:/a", "https:///a", "https://256.1.2.3/a", "https://host/a?",
	} {
		if u, ok := nxParseURI(raw); !ok || u.String() != raw {
			t.Errorf("valid reference %q: %v, round trip %q", raw, ok, u.String())
		}
	}
	for _, raw := range []string{
		"https://example.com/a b", "https://example.com/é", "https://example.com/%GG",
		"https://example.com/%", "#a#b", "#/bad[fragment]", "1invalid:scheme",
		"https://host:abc/a", "https://user@@host/a", "https://[bad]/a",
		"https://[::1]tail/a", "https://[::1%25zone]/a", "https://[v1.]/a",
		"https://host/a\\b", "https://host/{x}", "https://host/a\n",
	} {
		if _, ok := nxParseURI(raw); ok {
			t.Errorf("invalid reference %q accepted", raw)
		}
	}
}

func TestNextURIResolutionRFC3986Examples(t *testing.T) {
	// Section 5.4 examples exercise merge, query/fragment presence, strict
	// scheme handling, and dot removal without touching query or fragment.
	for _, tc := range []struct{ ref, want string }{
		{"g:h", "g:h"}, {"g", "http://a/b/c/g"}, {"./g", "http://a/b/c/g"},
		{"g/", "http://a/b/c/g/"}, {"/g", "http://a/g"}, {"//g", "http://g"},
		{"?y", "http://a/b/c/d;p?y"}, {"g?y", "http://a/b/c/g?y"},
		{"#s", "http://a/b/c/d;p?q#s"}, {"g#s", "http://a/b/c/g#s"},
		{"g?y#s", "http://a/b/c/g?y#s"}, {";x", "http://a/b/c/;x"},
		{"g;x", "http://a/b/c/g;x"}, {"g;x?y#s", "http://a/b/c/g;x?y#s"},
		{"", "http://a/b/c/d;p?q"}, {".", "http://a/b/c/"}, {"./", "http://a/b/c/"},
		{"..", "http://a/b/"}, {"../", "http://a/b/"}, {"../g", "http://a/b/g"},
		{"../..", "http://a/"}, {"../../", "http://a/"}, {"../../g", "http://a/g"},
		{"../../../g", "http://a/g"}, {"../../../../g", "http://a/g"},
		{"/./g", "http://a/g"}, {"/../g", "http://a/g"}, {"g.", "http://a/b/c/g."},
		{".g", "http://a/b/c/.g"}, {"g..", "http://a/b/c/g.."}, {"..g", "http://a/b/c/..g"},
		{"./../g", "http://a/b/g"}, {"./g/.", "http://a/b/c/g/"},
		{"g/./h", "http://a/b/c/g/h"}, {"g/../h", "http://a/b/c/h"},
		{"g;x=1/./y", "http://a/b/c/g;x=1/y"}, {"g;x=1/../y", "http://a/b/c/y"},
		{"g?y/./x", "http://a/b/c/g?y/./x"}, {"g?y/../x", "http://a/b/c/g?y/../x"},
		{"g#s/./x", "http://a/b/c/g#s/./x"}, {"g#s/../x", "http://a/b/c/g#s/../x"},
		{"http:g", "http:g"}, {"?", "http://a/b/c/d;p?"},
	} {
		if got, ok := nxResolveID("http://a/b/c/d;p?q", tc.ref); !ok || got != tc.want {
			t.Errorf("resolve %q: %q, %v; want %q", tc.ref, got, ok, tc.want)
		}
	}
	for _, tc := range []struct{ base, ref, want string }{
		{"HTTPS://user%41@EXAMPLE.com", "a%2fB", "HTTPS://user%41@EXAMPLE.com/a%2fB"},
		{"https://host/a//b/", "./c", "https://host/a//b/c"},
		{"https://host/a", "/.//g", "https://host//g"},
		{"", "urn:example:a/../b", "urn:/b"},
		{"", "https://host/%2e/%2E./b", "https://host/%2e/%2E./b"},
	} {
		if got, ok := nxResolveID(tc.base, tc.ref); !ok || got != tc.want {
			t.Errorf("resolve %q against %q: %q, %v; want %q", tc.ref, tc.base, got, ok, tc.want)
		}
	}
}

func TestNextIdentifierComparisonPreservesAuthoredSpelling(t *testing.T) {
	for _, tc := range []struct {
		name, schemas string
		duplicate     bool
	}{
		{"userinfo encoding", `{"A":{"$id":"https://user%41@example.com/a"},"B":{"$id":"https://userA@example.com/a"}}`, false},
		{"percent case", `{"A":{"$id":"https://host/%2f"},"B":{"$id":"https://host/%2F"}}`, false},
		{"encoded dot", `{"A":{"$id":"https://host/%2e/a"},"B":{"$id":"https://host/a"}}`, false},
		{"double slash", `{"A":{"$id":"https://host/a//b"},"B":{"$id":"https://host/a/b"}}`, false},
		{"empty query", `{"A":{"$id":"https://host/a?"},"B":{"$id":"https://host/a"}}`, false},
		{"invalid relative base excludes descendants", `{"A":{"$id":"https://host/a b","$defs":{"B":{"$id":"next"}}},"C":{"$id":"https://host/next"}}`, false},
		{"absolute descendant recovers from invalid base", `{"A":{"$id":"https://host/a b","$defs":{"B":{"$id":"https://host/next"}}},"C":{"$id":"https://host/next"}}`, true},
		{"rootless dot segments", `{"A":{"$id":"urn:example:a/../b"},"B":{"$id":"urn:/b"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := nxNewObj().Set("schemas", nxMustParse(tc.schemas))
			violations := strings.Join(nxAdditionalRules(doc), "\n")
			if got := strings.Contains(violations, "OBI-D-13"); got != tc.duplicate {
				t.Fatalf("duplicate = %v, want %v: %s", got, tc.duplicate, violations)
			}
		})
	}
	for _, schema := range []string{
		`{"$id":"https://example.com/a b"}`,
		`{"$ref":"https://example.com/a b"}`,
		`{"$ref":"#/bad[fragment]"}`,
	} {
		out, _, err := nxExec("", "schema", "add", "sample", "Bad", "--value", schema)
		if nxExitCode(err) != 3 || !strings.Contains(err.Error(), "OBI-D-05") || out != "" {
			t.Errorf("%s: output %q, error %v; want D-05 refusal without write", schema, out, err)
		}
	}
}
