package app

import (
	"fmt"
	"sort"
	"strings"
)

// InvokeConfig carries the caller-owned invocation context: the ordered
// binding selection, and (on the prepare surface) the binding
// specification's named interpretation points already known to the caller.
// Invocation surfaces carry zero protocol semantics — protocol-mechanic
// answers come from the standing context loop (CONTEXT_REQUIRED + the
// context store) and ob's standing internal table, never from per-call
// configuration.
type InvokeConfig struct {
	// Selection is the operation-invoker contract's ordered caller choice.
	// The first invocable binding key belonging to each resolved operation
	// wins. Keeping it as invocation context lets the same list govern nested
	// operation-graph calls without putting selection policy in the OBI.
	Selection []string
	// Configuration carries the governing binding specification's named
	// interpretation points (document, server, address, and so on). It is
	// merged with Selection under context.configuration.
	Configuration map[string]any
}

// context returns the caller-owned operation context carried into this
// invocation. Selection and named binding-spec interpretation points are
// caller-owned; credentials and standing transport context continue to come
// from the scoped context loop.
func (c *InvokeConfig) context() map[string]any {
	if c == nil || (len(c.Selection) == 0 && len(c.Configuration) == 0) {
		return nil
	}
	configuration := make(map[string]any, len(c.Configuration)+1)
	for key, value := range c.Configuration {
		configuration[key] = value
	}
	if len(c.Selection) > 0 {
		configuration["selection"] = append([]string(nil), c.Selection...)
	}
	return map[string]any{
		"configuration": configuration,
	}
}

// Context returns the caller-owned context represented by this invocation
// configuration. It is shared by the CLI invoke and prepare surfaces so the
// same binding-spec interpretation points reach both contracts.
func (c *InvokeConfig) Context() map[string]any {
	return c.context()
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
