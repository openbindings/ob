package app

import (
	"context"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/openbindings-go/invoke"
)

type storedContextPreparer struct {
	details *invoke.ContextRequiredDetails
}

func (p storedContextPreparer) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: "example.binding@1"}}
}

func (p storedContextPreparer) InvokeBinding(context.Context, *invoke.BindingInvocationArgs) invoke.Invocation[any, any] {
	return invoke.NewErroredInvocation[any, any](&invoke.InvocationError{
		Code: invoke.ErrCodeRuntime,
	})
}

func (p storedContextPreparer) PrepareBinding(context.Context, *invoke.BindingInvocationArgs) (*invoke.ContextRequiredDetails, error) {
	return p.details, nil
}

func TestWithStoredContextScopesStoreButPreservesExplicitContext(t *testing.T) {
	setupContextTestDir(t)
	target := "https://api.example.com"
	if err := SaveUnifiedContext(target, map[string]any{
		"bearerToken": "stored-token",
		"headers": map[string]any{
			"X-Stored-Unrelated": "must-not-pass",
		},
	}); err != nil {
		t.Fatalf("SaveUnifiedContext: %v", err)
	}

	details := &invoke.ContextRequiredDetails{
		Target: target,
		Alternatives: []invoke.ContextAlternative{{
			Requirements: []invoke.ContextRequirement{{Type: "auth.bearer"}},
		}},
	}
	invoker := invoke.NewOperationInvoker(storedContextPreparer{details: details})
	args := &invoke.BindingInvocationArgs{
		Source: invoke.InvocationSource{BindingSpec: "example.binding@1"},
		Context: map[string]any{
			"headers": map[string]any{
				"X-Explicit": "caller-value",
			},
		},
	}

	got := withStoredContext(context.Background(), invoker, args)
	if invoke.ContextBearerToken(got) != "stored-token" {
		t.Fatalf("scoped stored credential missing: %#v", got)
	}
	headers := invoke.ContextHeaders(got)
	if headers["X-Explicit"] != "caller-value" {
		t.Fatalf("explicit per-call context was dropped: %#v", got)
	}
	if _, leaked := headers["X-Stored-Unrelated"]; leaked {
		t.Fatalf("unrelated stored context leaked: %#v", got)
	}
}
