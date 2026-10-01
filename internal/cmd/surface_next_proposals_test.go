package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNextConfigTypesAreExplicit(t *testing.T) {
	for _, tc := range []struct{ raw, point, kind string }{
		{"code=001", "code", "string"},
		{"enabled=false", "enabled", "string"},
		{"empty=", "empty", "string"},
		{"enabled:=false", "enabled", "JSON boolean"},
		{"count:=12345678901234567890", "count", "JSON number"},
		{`server:={"url":"https://example.com"}`, "server", "JSON object"},
		{"server=@server.txt", "server", "string"},
		{"server:=@server.json", "server", "JSON from file or stdin"},
		{"server=-", "server", "string"},
		{"server:=-", "server", "JSON from file or stdin"},
	} {
		point, kind, err := nxConfigAssignment(tc.raw)
		if err != nil || point != tc.point || kind != tc.kind {
			t.Errorf("%s: %q, %q, %v", tc.raw, point, kind, err)
		}
	}
	for _, raw := range []string{"code:=001", "enabled:=False", "=value", ":=null", "server=@"} {
		_, _, err := nxConfigAssignment(raw)
		if nxExitCode(err) != 2 {
			t.Errorf("bad assignment %q: %v", raw, err)
		}
	}
}

func TestNextNamedContextCanBeChangedAndRemoved(t *testing.T) {
	for _, args := range [][]string{
		{"--credential", "primary=@primary.json", "--credential", "secondary=@secondary.json"},
		{"--cookie", "session=@session.txt", "--refresh-token", "@refresh.txt"},
		{"--unset", "credential.primary", "--unset", "cookie.session", "--unset", "refresh-token"},
	} {
		out, _, err := nxExec("", append([]string{"context", "set", "https://api.example.com"}, args...)...)
		if err != nil || out == "" {
			t.Errorf("%v: %q, %v", args, out, err)
		}
	}
	for _, args := range [][]string{
		{"--credential", "primary=secret-on-command-line"},
		{"--credential", "primary=@"},
		{"--credential", "primary=-", "--credential", "secondary=-"},
		{"--config", "a:=-", "--cookie", "session=-"},
		{"--credential", "primary=@p.json", "--unset", "credential.primary"},
	} {
		_, _, err := nxExec("", append([]string{"context", "set", "https://api.example.com"}, args...)...)
		if nxExitCode(err) != 2 {
			t.Errorf("%v: want usage 2, got %v", args, err)
		}
	}
}

func TestNextKindReportAndPureContractDirection(t *testing.T) {
	_, _, err := nxExec("", "kind", "check", "unknown@1")
	if err != nil {
		t.Fatalf("no-role report: %v", err)
	}
	_, _, err = nxExec("", "kind", "check", "unknown@1", "--role", "invoke")
	if nxExitCode(err) != 1 {
		t.Fatalf("role verdict: %v", err)
	}
	doc := nxNewObj().Set("operations", nxNewObj().Set("contract", nxNewObj()))
	if roles := nxOperationRoles(doc, "contract"); strings.Join(roles, ",") != "provider,consumer" {
		t.Fatalf("pure contract: %v", roles)
	}
	doc.Set("bindings", nxNewObj().Set("bound", nxNewObj().Set("operation", "contract")))
	if roles := nxOperationRoles(doc, "contract"); strings.Join(roles, ",") != "provider" {
		t.Fatalf("bound operation: %v", roles)
	}
}

func TestNextLocalServiceHandoff(t *testing.T) {
	out, _, err := nxExec("", "start", "-F", "json", "--tls")
	var record struct{ Address, TLSAddress, Token, StateFile, OBI string }
	if err != nil || strings.Count(strings.TrimSpace(out), "\n") != 0 || json.Unmarshal([]byte(out), &record) != nil || record.Token == "" || record.StateFile == "" || record.Address != "http://127.0.0.1:20290" || record.TLSAddress != "https://127.0.0.1:20291" {
		t.Fatalf("startup record: %q, %v", out, err)
	}
	text, _, err := nxExec("", "start")
	if err != nil || strings.Contains(text, record.Token) || !strings.Contains(text, record.StateFile) {
		t.Fatalf("text handoff: %q, %v", text, err)
	}
	_, note, err := nxExec("", "show", record.Address)
	if err != nil || !strings.Contains(note, "private run record") {
		t.Fatalf("automatic local token: %q, %v", note, err)
	}
	_, note, err = nxExec("", "show", "http://127.0.0.1:9090")
	if err != nil || strings.Contains(note, "private run record") {
		t.Fatalf("record must match the service address: %q, %v", note, err)
	}
}
