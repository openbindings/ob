package mcpbridge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openbindings "github.com/openbindings/openbindings-go"
)

// Binding selection for multi-binding operations.
//
// OpenBindings deliberately leaves binding SELECTION to the consuming
// application (core invariant 2; selection is not a spec-defined algorithm).
// When an OBI is bridged to MCP, the bridge IS that consuming application, so
// it owes a selection policy — but a spec-loyal one. The prior policy silently
// dropped every multi-binding operation from tools/list and logged the reason
// only to stderr, which an MCP client structurally cannot see. That is a QUIET
// refusal, and the project's own doctrine requires a LOUD, pre-dispatch one.
//
// This is the deference order made concrete (honor -> expose -> refuse loudly,
// never silently drop and never silently invent a default):
//
//  1. HONOR an explicit context selection (as OperationInvoker does).
//  2. A single invocable binding is not a choice — use it.
//  3. HONOR an author-declared `preference` (spec §5.3): if EVERY candidate
//     declares one and there is a unique maximum, that is an author assertion
//     the bridge may act on without inventing policy. (All-declared is required
//     because "omission states no preference and is not equivalent to zero"
//     — §5.3 — so a partial declaration cannot be ranked soundly.)
//  4. Otherwise ADVERTISE the operation anyway and refuse the call loudly:
//     a call that does not resolve the choice returns a structured error
//     listing the exact valid binding keys, so the agent recovers without
//     leaving the protocol.
//
// EXPOSE ALWAYS. Whenever an operation has more than one candidate, the tool
// carries the optional `_binding` argument enumerating them — including when
// steps 1-3 produced a preselect, in which case the description names the
// binding the caller will otherwise get. Honoring an author signal must not
// cost the caller the ability to choose against it: §5.3 calls `preference` a
// preference, and the core deliberately defines no selection algorithm, so a
// bridge that acted on it *and* hid the alternatives would have promoted a
// signal into a mandate. An explicit `_binding` therefore always outranks the
// preselect.
//
// Genuinely unadvertisable operations (no invoker, no invocable binding, a
// binding whose source is missing/unavailable) are still excluded, exactly as
// before — invoking them would fail, so refusing before advertising is correct.

// bindingSelectionArg is the optional per-call selection member added to a
// multi-binding operation's tool input. Underscore-prefixed to minimize
// collision with author-defined input fields.
const bindingSelectionArg = "_binding"

type bindingResolution struct {
	exclude    bool     // genuinely unadvertisable
	reason     string   // exclusion reason (only when exclude)
	preselect  string   // a loyal, unambiguous choice the bridge may make
	ambiguous  bool     // no loyal choice available; refuse loudly in-band
	candidates []string // sorted invocable binding keys; populated whenever the
	// operation is advertised, INCLUDING when preselect is
	// set, so a caller can always see and override the
	// choice made on its behalf (`preference` is a signal
	// the spec defines no algorithm for — honoring it must
	// not silently become binding).
}

func resolveOperationBinding(
	iface *openbindings.Interface,
	opKey string,
	invoker *openbindings.OperationInvoker,
	baseContext map[string]any,
) bindingResolution {
	if invoker == nil {
		return bindingResolution{exclude: true, reason: "no OpenBindings invoker is installed"}
	}
	available := map[string]bool{}
	for _, info := range invoker.BindingSpecs() {
		available[info.BindingSpec] = true
	}

	// The candidate set is computed BEFORE any choice is made, so that every
	// resolution — including one the bridge resolves on the caller's behalf —
	// can still advertise the full set. Honoring a declaration must not
	// destroy the caller's ability to see and override it.
	var candidates []string
	missing := map[string]string{}
	unavailable := map[string]string{}
	for key, binding := range iface.Bindings {
		if binding.Operation != opKey {
			continue
		}
		source, ok := iface.Sources[binding.Source]
		if !ok {
			candidates = append(candidates, key)
			missing[key] = binding.Source
			continue
		}
		if !available[source.BindingSpec] {
			unavailable[key] = source.BindingSpec
			continue
		}
		candidates = append(candidates, key)
	}

	if len(candidates) == 0 {
		if len(unavailable) > 0 {
			keys := make([]string, 0, len(unavailable))
			for key := range unavailable {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			needs := make([]string, 0, len(keys))
			for _, key := range keys {
				needs = append(needs, fmt.Sprintf("%s requires unavailable %s", key, unavailable[key]))
			}
			return bindingResolution{exclude: true, reason: strings.Join(needs, "; ")}
		}
		return bindingResolution{exclude: true, reason: "operation has no binding"}
	}

	sort.Strings(candidates)

	// (1) Explicit context selection wins, matching OperationInvoker's override:
	// the first listed binding for this operation on an installed spec.
	for _, key := range contextSelection(baseContext) {
		binding, ok := iface.Bindings[key]
		if !ok || binding.Operation != opKey {
			continue
		}
		source, sourceOK := iface.Sources[binding.Source]
		if !sourceOK {
			// Do NOT exclude. The caller named this binding explicitly; a
			// dangling source is a defect in the DOCUMENT, and hiding the
			// operation would report it only on stderr — the precise quiet
			// refusal this file exists to eliminate. Proceed, and let the
			// invoker surface ERR_UNKNOWN_SOURCE loudly at call time, which
			// is what the SDK's own selection override deliberately does.
			return bindingResolution{preselect: key, candidates: candidates}
		}
		if available[source.BindingSpec] {
			return bindingResolution{preselect: key, candidates: candidates}
		}
	}

	// (2) A single invocable binding is not a choice.
	if len(candidates) == 1 {
		if source, bad := missing[candidates[0]]; bad {
			return bindingResolution{exclude: true, reason: fmt.Sprintf("%s references missing source %s", candidates[0], source)}
		}
		return bindingResolution{preselect: candidates[0], candidates: candidates}
	}

	// (3) Honor a fully-declared, unique author preference.
	if winner, ok := uniquePreferenceWinner(iface, candidates, missing); ok {
		return bindingResolution{preselect: winner, candidates: candidates}
	}

	// (4) Advertise anyway and refuse loudly in-band.
	return bindingResolution{ambiguous: true, candidates: candidates}
}

// uniquePreferenceWinner returns the single candidate with the strictly-highest
// declared `preference`, but ONLY when every candidate declares a preference
// (so undeclared bindings are never treated as lowest, per §5.3) and the winner
// has an invocable source. Ties or any omission => no winner.
func uniquePreferenceWinner(iface *openbindings.Interface, candidates []string, missing map[string]string) (string, bool) {
	best := ""
	var bestPref float64
	tie := false
	for i, key := range candidates {
		be, ok := iface.Bindings[key]
		if !ok || be.Preference == nil {
			return "", false // an undeclared preference makes ranking unsound
		}
		p := *be.Preference
		if i == 0 || p > bestPref {
			bestPref, best, tie = p, key, false
		} else if p == bestPref {
			tie = true
		}
	}
	if best == "" || tie {
		return "", false
	}
	if _, bad := missing[best]; bad {
		return "", false // top preference is broken; refuse loudly rather than pick a lesser one
	}
	return best, true
}

// contextWithSelection clones base and sets configuration.selection so the
// OperationInvoker dispatches through the chosen binding.
func contextWithSelection(base map[string]any, selection []string) map[string]any {
	if len(selection) == 0 {
		return base
	}
	out := make(map[string]any, len(base)+1)
	for k, v := range base {
		out[k] = v
	}
	cfg := map[string]any{}
	if existing, ok := base["configuration"].(map[string]any); ok {
		for k, v := range existing {
			cfg[k] = v
		}
	}
	cfg["selection"] = selection
	out["configuration"] = cfg
	return out
}

// extractBindingArg pulls the optional `_binding` selection member out of a
// tool's raw arguments and returns the remaining arguments (with the member
// removed) so the operation input decodes cleanly.
func extractBindingArg(raw json.RawMessage) (choice string, cleaned json.RawMessage, err error) {
	if len(raw) == 0 {
		return "", raw, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		// Not an object (scalar/array via envelope decode path handles it
		// elsewhere); nothing to extract.
		return "", raw, nil
	}
	sel, ok := obj[bindingSelectionArg]
	if !ok {
		return "", raw, nil
	}
	delete(obj, bindingSelectionArg)
	if err := json.Unmarshal(sel, &choice); err != nil {
		return "", raw, fmt.Errorf("%q must be a string", bindingSelectionArg)
	}
	cleaned, err = json.Marshal(obj)
	if err != nil {
		return "", raw, err
	}
	return choice, cleaned, nil
}

// withBindingArg advertises the `_binding` selection member on a multi-binding
// tool's (object) input schema, so an MCP client can discover and supply the
// choice in-band. Non-object schemas are returned unchanged.
//
// preselect, when non-empty, is the binding this bridge will use if the caller
// omits the argument. It is stated in the description rather than left implicit:
// a caller that cannot see which binding it is about to dispatch over cannot
// meaningfully decide whether to override it.
func withBindingArg(schema any, candidates []string, preselect string) any {
	root, ok := schema.(map[string]any)
	if !ok {
		return schema
	}
	clone := make(map[string]any, len(root)+1)
	for k, v := range root {
		clone[k] = v
	}
	props := map[string]any{}
	if existing, ok := clone["properties"].(map[string]any); ok {
		for k, v := range existing {
			props[k] = v
		}
	}
	enum := make([]any, len(candidates))
	for i, c := range candidates {
		enum[i] = c
	}
	description := "This operation is realized over multiple bindings; select one by its key. " +
		"Omitting it returns an error listing the valid keys."
	if preselect != "" {
		description = fmt.Sprintf(
			"This operation is realized over multiple bindings; select one by its key. "+
				"Omitting it dispatches over %q, chosen from the interface's own declarations.",
			preselect)
	}
	props[bindingSelectionArg] = map[string]any{
		"type":        "string",
		"enum":        enum,
		"description": description,
	}
	clone["properties"] = props
	return clone
}

// ambiguousBindingResult is the loud, in-band, pre-dispatch refusal an MCP
// client receives when it invokes a multi-binding operation without resolving
// the choice. It names the exact valid keys so the agent can recover by
// re-calling with `_binding`.
func ambiguousBindingResult(candidates []string) *mcp.CallToolResult {
	msg := fmt.Sprintf(
		"binding selection required: this operation is realized over multiple bindings (%s); re-call with %q set to one of them.",
		strings.Join(candidates, ", "), bindingSelectionArg,
	)
	env := map[string]any{
		"error": map[string]any{
			"code":    "ERR_BINDING_SELECTION_REQUIRED",
			"message": msg,
			"details": map[string]any{"bindings": candidates},
		},
	}
	data, _ := json.Marshal(env)
	return &mcp.CallToolResult{
		IsError:           true,
		Content:           []mcp.Content{&mcp.TextContent{Text: string(data)}},
		StructuredContent: env,
	}
}

func invalidBindingResult(choice string, candidates []string) *mcp.CallToolResult {
	msg := fmt.Sprintf("%q is not a valid binding for this operation; choose one of: %s",
		choice, strings.Join(candidates, ", "))
	env := map[string]any{
		"error": map[string]any{
			"code":    "ERR_UNKNOWN_BINDING",
			"message": msg,
			"details": map[string]any{"bindings": candidates},
		},
	}
	data, _ := json.Marshal(env)
	return &mcp.CallToolResult{
		IsError:           true,
		Content:           []mcp.Content{&mcp.TextContent{Text: string(data)}},
		StructuredContent: env,
	}
}
