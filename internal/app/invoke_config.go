package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
)

// InvokeConfig is the data face's per-invocation configuration, compiled
// from `op invoke`'s --decode/--ok-exit/--route flags into per-invocation
// hooks — the top decline-chain tier over ob's standing internal table
// (specification + configuration = complete invocation). Empty means no
// per-invocation intent: every axis falls through the chain to the
// table/builtin. Flags are PER-AXIS; an unmentioned axis or field declines.
type InvokeConfig struct {
	// Selection is the operation-invoker contract's ordered caller choice.
	// The first invocable binding key belonging to each resolved operation
	// wins. Keeping it as invocation context lets the same list govern nested
	// operation-graph calls without putting selection policy in the OBI.
	Selection []string
	// Configuration carries the governing binding specification's named
	// interpretation points for this call (document, server, address, and
	// so on). It is merged with Selection under context.configuration.
	Configuration map[string]any
	// Decode is the output-lane override: "json" (strict parse), "text"
	// (trailing-newline-stripped string), "none" (stdout unconsulted,
	// output null — T-08 STILL applies on a typed contract), or "" (unset).
	Decode string
	// OKExits are the exit codes classified as success (the diff(1) class,
	// {0,1}); nil = axis unset.
	OKExits []int
	// Routes are field→channel elections (argv|stdin|stdin-dash|file),
	// keyed by POST-TRANSFORM field name; nil/empty = axis unset.
	Routes map[string]string
	// Anonymous asserts that this invocation carries no credentials, which
	// is how a caller reaches an operation whose artifact declares security
	// the live service does not enforce — a public read under a blanket
	// document-level requirement, say. It answers credential challenges and
	// suppresses credential placement; it never answers a configuration
	// point such as which server to address.
	Anonymous bool
}

// empty reports whether the config carries no per-invocation hook intent.
// Binding-spec configuration and selection ride context independently.
func (c *InvokeConfig) empty() bool {
	return c == nil || (c.Decode == "" && len(c.OKExits) == 0 && len(c.Routes) == 0)
}

// context returns the caller-owned operation context carried into this
// invocation. Selection and named binding-spec interpretation points are
// caller-owned; credentials and standing transport context continue to come
// from the scoped context loop.
func (c *InvokeConfig) context() map[string]any {
	if c == nil || (len(c.Selection) == 0 && len(c.Configuration) == 0 && !c.Anonymous) {
		return nil
	}
	out := map[string]any{}
	if len(c.Selection) > 0 || len(c.Configuration) > 0 {
		configuration := make(map[string]any, len(c.Configuration)+1)
		for key, value := range c.Configuration {
			configuration[key] = value
		}
		if len(c.Selection) > 0 {
			configuration["selection"] = append([]string(nil), c.Selection...)
		}
		out["configuration"] = configuration
	}
	// Anonymity sits beside `configuration`, not inside it: it is not a
	// binding-specification interpretation point that some artifact declared,
	// it is a statement about the credentials this call carries. Filing it
	// under a named point would imply each family gets to redefine it.
	if c.Anonymous {
		out["anonymous"] = true
	}
	return out
}

// Context returns the caller-owned context represented by this invocation
// configuration. It is shared by the CLI invoke and prepare surfaces so the
// same binding-spec interpretation points reach both contracts.
func (c *InvokeConfig) Context() map[string]any {
	return c.context()
}

// perInvocationHooks compiles the config into a seam carrier composed over
// the invoker's standing hooks (SnapshotHooks: per-invocation over
// invoker-level). Nil when empty. The axes are format-generic — an
// explicit per-invocation flag is intent for THIS invocation and wins over
// any format built-in, so no format guard is applied (unlike the standing
// table, which is site-guarded to ob's own OBI).
func (c *InvokeConfig) perInvocationHooks(invoker *openbindings.OperationInvoker) *openbindings.InvokeHooks {
	if c.empty() {
		return nil
	}

	var decode openbindings.OutputDecoder
	switch c.Decode {
	case "json":
		decode = func(_ openbindings.InvokeSite, raw openbindings.RawResult) (any, error) {
			if len(raw.Body) == 0 {
				return nil, nil
			}
			var v any
			if err := json.Unmarshal(raw.Body, &v); err != nil {
				return nil, &openbindings.InvocationError{
					Code: openbindings.ErrCodeResponseError,
				}
			}
			return v, nil
		}
	case "text":
		decode = func(_ openbindings.InvokeSite, raw openbindings.RawResult) (any, error) {
			return strings.TrimRight(string(raw.Body), "\r\n"), nil
		}
	case "none":
		// stdout not consulted; the output value is null. T-08 still runs
		// against the contract (deriving output from exit status is a hook
		// capability, not a flag one — the triage row says so).
		decode = func(_ openbindings.InvokeSite, _ openbindings.RawResult) (any, error) {
			return nil, nil
		}
	}

	var classify openbindings.ResultClassifier
	if len(c.OKExits) > 0 {
		oks := append([]int(nil), c.OKExits...)
		classify = func(_ openbindings.InvokeSite, raw openbindings.RawResult) (bool, error) {
			if raw.Status == nil {
				return false, openbindings.ErrUseDefault
			}
			for _, o := range oks {
				if *raw.Status == o {
					return true, nil
				}
			}
			return false, nil
		}
	}

	var route openbindings.FieldRouter
	if len(c.Routes) > 0 {
		routes := c.Routes
		route = func(_ openbindings.InvokeSite, field string, _ any) string {
			return routes[field] // "" (absent) declines to the format default
		}
	}

	return invoker.SnapshotHooks(decode, classify, route)
}

// displacedElections enumerates ob's standing internal-table elections for
// a resolved canonical operation key — the elections that a WINNING
// EXTERNAL delegate does not receive (it dispatches the binding hop itself,
// past ob's invoker-level hooks). Non-empty only when the invoked op is one
// of ob's OWN bound ops (the table is site-guarded to ob's OBI); an
// arbitrary user OBI routed to a delegate has nothing to displace. Sorted
// for deterministic warnings.
func displacedElections(opKey string) []string {
	iface, err := OpenBindingsInterface()
	if err != nil {
		return nil
	}
	table := BoundCLIHookTable(&iface)
	var out []string
	for _, k := range table.DecodeJSON {
		if k == opKey {
			out = append(out, "decode=json")
		}
	}
	if codes, ok := table.OKExits[opKey]; ok {
		parts := make([]string, len(codes))
		for i, c := range codes {
			parts[i] = fmt.Sprintf("%d", c)
		}
		out = append(out, "ok-exit="+strings.Join(parts, ","))
	}
	if routes, ok := table.Routes[opKey]; ok {
		fields := make([]string, 0, len(routes))
		for f := range routes {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		for _, f := range fields {
			out = append(out, "route:"+f+"="+routes[f])
		}
	}
	sort.Strings(out)
	return out
}

// displacedElectionsWarning composes the loud attributed displacement
// warning: the count of ob's standing elections (the internal hook table
// published as docs/bound-cli-recipe.md) that do not reach the
// winning external delegate. Returns "" when nothing is displaced. The
// verbose detail (which elections) rides the returned slice for `-v`.
func displacedElectionsWarning(opKey, delegate string) (summary string, detail []string) {
	elections := displacedElections(opKey)
	if len(elections) == 0 {
		return "", nil
	}
	return fmt.Sprintf("%d internal-table election(s) for %q do not reach delegate %q; its own handling governs the binding hop",
		len(elections), opKey, delegate), elections
}
