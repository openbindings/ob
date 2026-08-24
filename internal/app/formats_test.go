package app

import (
	"slices"
	"testing"
)

func TestCheckBindingSpecsExactOrderedAndDeduplicated(t *testing.T) {
	native := getNativeTokens()
	if len(native) == 0 {
		t.Fatal("expected at least one native binding specification")
	}
	supported := native[0]
	adjacent := supported + ".future"

	got := CheckBindingSpecs([]string{supported, "unknown", supported, adjacent, "unknown"})
	if len(got) != 3 {
		t.Fatalf("got %d verdicts, want one per unique token (3): %+v", len(got), got)
	}
	if got[0].BindingSpec != supported || !got[0].Supported {
		t.Errorf("native exact token verdict = %+v, want supported", got[0])
	}
	if got[1].BindingSpec != "unknown" || got[1].Supported {
		t.Errorf("unknown token verdict = %+v, want refused", got[1])
	}
	if got[2].BindingSpec != adjacent || got[2].Supported {
		t.Errorf("prefix-adjacent token verdict = %+v, want exact-match refusal", got[2])
	}
}

func TestCheckBindingSpecsEmptyAndListedSubset(t *testing.T) {
	if got := CheckBindingSpecs(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty check = %#v, want a non-nil empty result", got)
	}

	listed := ListBindingSpecs()
	tokens := make([]string, len(listed))
	for i, info := range listed {
		tokens[i] = info.BindingSpec
	}
	verdicts := CheckBindingSpecs(tokens)
	if len(verdicts) != len(tokens) {
		t.Fatalf("listed check returned %d verdicts for %d tokens", len(verdicts), len(tokens))
	}
	for i, verdict := range verdicts {
		if verdict.BindingSpec != tokens[i] || !verdict.Supported {
			t.Errorf("listed token %q verdict = %+v, want exact supported verdict", tokens[i], verdict)
		}
	}

	want := append([]string(nil), getNativeTokens()...)
	slices.Sort(want)
	if !slices.Equal(tokens, want) {
		t.Errorf("advisory list = %v, want only sorted native tokens %v", tokens, want)
	}
}
