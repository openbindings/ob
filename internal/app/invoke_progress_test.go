package app

import (
	"context"
	"testing"

	"github.com/openbindings/openbindings-go/invoke"
)

func TestDriveBindingRequiresStructuralContextProgress(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			calls := 0
			challenge := &invoke.ContextRequiredDetails{Target: "https://api.example.test", Alternatives: []invoke.ContextAlternative{{Requirements: []invoke.ContextRequirement{{Type: "auth.bearer"}}}}}
			invoker := func(ctx context.Context, supplied map[string]any) invoke.Invocation[any, any] {
				calls++
				return invoke.NewErroredInvocation[any, any](invoke.NewContextRequiredError(challenge))
			}
			resolver := func(ctx context.Context, details *invoke.ContextRequiredDetails) (map[string]any, error) {
				value := "initial"
				if changed {
					value = "replacement"
				}
				return map[string]any{"bearerToken": value, "nested": map[string]any{"same": true}}, nil
			}
			outputs := 0
			for result := range driveBinding(context.Background(), invoker, map[string]any{"bearerToken": "initial", "nested": map[string]any{"same": true}}, nil, resolver, nil) {
				outputs++
				if result.Error == nil {
					t.Fatal("challenge disappeared")
				}
			}
			want := 1
			if changed {
				want = 2
			}
			if calls != want || outputs != 1 {
				t.Fatalf("calls=%d outputs=%d; want calls=%d outputs=1", calls, outputs, want)
			}
		})
	}
}
