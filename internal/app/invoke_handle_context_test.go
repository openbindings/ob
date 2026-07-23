package app

import (
	"context"
	"testing"

	openbindings "github.com/openbindings/openbindings-go"
)

type storedContextPreparer struct {
	details *openbindings.ContextRequiredDetails
}

func (p storedContextPreparer) BindingSpecs() []openbindings.BindingSpecInfo {
	return []openbindings.BindingSpecInfo{{BindingSpec: "example.binding@1"}}
}

func (p storedContextPreparer) InvokeBinding(context.Context, *openbindings.BindingInvocationArgs) openbindings.Invocation[any, any] {
	return openbindings.NewErroredInvocation[any, any](&openbindings.InvocationError{
		Code:    openbindings.ErrCodeRuntime,
		Message: "not used by this test",
	})
}

func (p storedContextPreparer) PrepareBinding(context.Context, *openbindings.BindingInvocationArgs) (*openbindings.ContextRequiredDetails, error) {
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

	details := &openbindings.ContextRequiredDetails{
		Target: target,
		Alternatives: []openbindings.ContextAlternative{{
			Requirements: []openbindings.ContextRequirement{{Type: "auth.bearer"}},
		}},
	}
	invoker := openbindings.NewOperationInvoker(storedContextPreparer{details: details})
	args := &openbindings.BindingInvocationArgs{
		Source: openbindings.InvocationSource{BindingSpec: "example.binding@1"},
		Context: map[string]any{
			"headers": map[string]any{
				"X-Explicit": "caller-value",
			},
		},
	}

	got := withStoredContext(context.Background(), invoker, args)
	if openbindings.ContextBearerToken(got) != "stored-token" {
		t.Fatalf("scoped stored credential missing: %#v", got)
	}
	headers := openbindings.ContextHeaders(got)
	if headers["X-Explicit"] != "caller-value" {
		t.Fatalf("explicit per-call context was dropped: %#v", got)
	}
	if _, leaked := headers["X-Stored-Unrelated"]; leaked {
		t.Fatalf("unrelated stored context leaked: %#v", got)
	}
}
