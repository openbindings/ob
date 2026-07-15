package app

import "testing"

// A live-address location must derive an OBI-D-03-valid source key.
func TestDeriveSourceKey_SanitizesLiveAddresses(t *testing.T) {
	for _, c := range []struct{ loc, wantValid string }{
		{"localhost:9090", "localhost_9090"},
		{"internal.host:8443", "internal_host:8443"},
	} {
		got := DeriveSourceKey(SynthesizeInterfaceSource{BindingSpec: "grpc", Location: c.loc}, 0)
		for _, r := range got {
			if !(r == '_' || r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				t.Errorf("DeriveSourceKey(%q) = %q contains invalid char %q", c.loc, got, r)
			}
		}
	}
}
