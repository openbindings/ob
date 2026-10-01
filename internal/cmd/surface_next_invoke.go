package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ob invoke is the command-line face of the operation-invoker contract. One
// run is one invocation handle: stdin carries input values, end of stdin
// closes the input, outputs print as they arrive, Ctrl-C cancels, and the
// exit status is the terminal state.

func nxInvokeCmd(variant string) *cobra.Command {
	if variant == "binding-invoke" {
		return nxBindingInvokeCmd()
	}
	cmd := nxLeaf("invoke", "invoke <obi> <operation>", "Call an operation", `Call an operation, by name or alias, through one of its bindings.

An invocation is an exchange, whatever the operation's shape: ob opens it,
writes your input values to it, prints each output value as it arrives, and
finishes when the binding does. One value in and one out, a stream in, a
stream out, or both at once all work the same way.

Input:
  --input VALUE   write one value (JSON), then close the input
  --input @FILE   write each JSON value in the file, then close the input
  --input -       write each JSON value read from stdin, and close the input
                  when stdin ends (Ctrl-D at a terminal)
  no --input      write nothing, and close the input

Output: each output value, as one line of JSON, as it arrives. --frames
prints the whole exchange instead, one frame per line, exactly as the
operation-invoker interface defines them: each output, the binding closing
its input early, and the final complete or error frame. A refusal ends with
its error frame too.

Choosing a binding: --binding names one; repeat it to give an ordered list,
and ob uses the first one it can invoke. A name that is not one of the
operation's bindings is refused, and so is a list with none ob can invoke;
ob never falls back to a binding you did not name. Without --binding, if
the operation has exactly one binding ob can invoke, ob uses it; if it has
several, ob stops and lists them. Preference and deprecation are shown,
never used to choose.

Checks: ob checks each input value against the operation's input schema
before sending it, and each output value against its output schema. A
value given with --input VALUE is checked before ob asks for any context.
When a schema cannot be fully resolved, ob cannot check against it and
stops with ERR_SCHEMA_UNRESOLVED.

Context: what a binding needs beyond the input, such as a credential, comes
from ob's context store, looked up by the exact scope the binding asks for.
At a terminal ob gets anything missing: it asks for a value, or runs the
sign-in the binding names (such as an OAuth 2.0 flow in your browser), and
offers to store the result. Stored tokens are renewed as they expire.
Otherwise ob stops before sending anything and prints what supplies it: an
ob context set command, or, for a sign-in, this same invoke with
--preflight to run once at a terminal. --context gives context for this call
only, as a JSON object. A delegate that invokes for ob resolves its own
context: ob never sends it stored context, and if it asks for something, ob
asks you or stops.

--preflight calls nothing. It prints what the binding already knows it will
ask for, and at a terminal it also gets what is missing, asking you or
running the sign-in, and stores it for later calls.

Without --frames, an error's code, and any data it carries, go to stderr.

Exit status: 0 completed; 1 the operation failed or an output did not fit
(either may have taken effect); 3 refused before anything was sent;
130 cancelled.`,
		`  ob invoke tasks.obi.json completeTask --input '{"id":"t_1"}'
  ob invoke tasks.obi.json createTask --binding createTask.http --input '{"title":"Ship it"}'
  printf '{"title":"a"}\n{"title":"b"}\n' | ob invoke tasks.obi.json importTasks --input -
  ob invoke tasks.obi.json watchTasks --frames
  ob invoke tasks.obi.json createTask --binding createTask.mcp --preflight`,
		nxArgs(2, 2), nxInvoke)
	cmd.Flags().String("input", "", "the input: one JSON value, @file, or - to stream values from stdin")
	cmd.Flags().StringArray("binding", nil, "use this binding; repeat for an ordered list")
	cmd.Flags().Bool("frames", false, "print the whole exchange as frames, not just output values")
	cmd.Flags().String("context", "", "context for this call only: a JSON object, @file, or -")
	cmd.Flags().Bool("preflight", false, "print what the binding will ask for, and at a terminal get what is missing; call nothing")
	return cmd
}

func nxBindingInvokeCmd() *cobra.Command {
	cmd := nxLeaf("invoke", "invoke <obi> <binding>", "Call one exact binding", `Call one binding exactly; ob does not choose among bindings. --input is
the input (one JSON value, @file, or - to stream from stdin). Output is the
whole exchange, one frame per line.`,
		`  ob invoke tasks.obi.json completeTask.http --input '{"id":"t_1"}'`,
		nxArgs(2, 2), func(c *nxCtx) error {
			doc := c.doc(c.args[0])
			b, err := c.entry(doc, "bindings", "binding", c.args[1])
			if err != nil {
				c.println(nxErrorFrame("ERR_BINDING_NOT_FOUND", nil))
				return nxFail(3, "%s", err.Error())
			}
			return nxInvokeThrough(c, doc, fmt.Sprint(b.Get("operation")), c.args[1], true)
		})
	cmd.Flags().String("input", "", "the input: one JSON value, @file, or - to stream values from stdin")
	cmd.Flags().String("context", "", "context for this call only: a JSON object, @file, or -")
	cmd.Flags().Bool("preflight", false, "print what the binding will ask for, and at a terminal get what is missing; call nothing")
	return cmd
}

func nxKindOf(doc *nxObj, binding string) string {
	src := fmt.Sprint(doc.Obj("bindings").Obj(binding).Get("source"))
	return fmt.Sprint(doc.Obj("sources").Obj(src).Get("kind"))
}

func nxSignals(doc *nxObj, binding string) string {
	var s []string
	b := doc.Obj("bindings").Obj(binding)
	if p := b.Get("preference"); p != nil {
		s = append(s, fmt.Sprintf("preference %v", p))
	}
	if d, _ := b.Get("deprecated").(bool); d {
		s = append(s, "deprecated")
	}
	return strings.Join(s, ", ")
}

func nxInvoke(c *nxCtx) error {
	c.banner = "ob preview: sample results; nothing was called"
	doc := c.doc(c.args[0])
	// A refusal is the invocation's terminal state, so --frames prints the
	// interface's error frame for it.
	refuse := func(code, format string, a ...any) error {
		if c.on("frames") {
			c.println(nxErrorFrame(code, nil))
		}
		return nxFail(3, format, a...)
	}
	key, ok := nxResolveOperation(doc, c.args[1])
	if !ok {
		hint := nxDidYouMean(doc, c.args[1])
		if hint == "" {
			hint = "; ob operation list shows them"
		}
		return refuse("ERR_OPERATION_NOT_FOUND", "no operation named %q in %s%s", c.args[1], c.args[0], hint)
	}
	bindings := nxReferrers(doc, "bindings", "operation", key)
	if len(bindings) == 0 {
		msg := fmt.Sprintf("operation %s has no bindings in %s, so there is no way to call it", key, c.args[0])
		if deps := nxReferrers(doc, "dependencies", "operation", key); len(deps) > 0 {
			msg += fmt.Sprintf("; the document only calls it, at %s", strings.Join(deps, ", "))
		}
		return refuse("ERR_BINDING_NOT_FOUND", "%s", msg)
	}
	listing := func(names []string) string {
		var rows [][]string
		for _, b := range names {
			rows = append(rows, []string{"  " + b, nxKindOf(doc, b), nxSignals(doc, b)})
		}
		t := &nxCtx{}
		t.table("", rows)
		var lines []string
		for _, l := range strings.Split(strings.TrimRight(t.out.String(), "\n"), "\n") {
			lines = append(lines, strings.TrimRight(l, " "))
		}
		return strings.Join(lines, "\n")
	}
	var chosen string
	if named := c.strs("binding"); len(named) > 0 {
		for _, b := range named {
			if doc.Obj("bindings") == nil || !doc.Obj("bindings").Has(b) {
				return refuse("ERR_BINDING_NOT_FOUND", "no binding named %q in %s", b, c.args[0])
			}
			if !nxContains(bindings, b) {
				return refuse("ERR_BINDING_NOT_FOUND", "binding %s carries out %v, not %s", b, doc.Obj("bindings").Obj(b).Get("operation"), key)
			}
		}
		for _, b := range named {
			if _, ok := nxSupports(nxKindOf(doc, b), "invoke"); ok {
				chosen = b
				break
			}
		}
		if chosen == "" {
			return refuse("ERR_BINDING_NOT_FOUND", "this ob cannot invoke any binding you named:\n%s", listing(named))
		}
	} else {
		var usable []string
		for _, b := range bindings {
			if _, ok := nxSupports(nxKindOf(doc, b), "invoke"); ok {
				usable = append(usable, b)
			}
		}
		switch len(usable) {
		case 0:
			return refuse("ERR_BINDING_NOT_FOUND", "this ob cannot invoke any of %s's bindings:\n%s", key, listing(bindings))
		case 1:
			chosen = usable[0]
		default:
			return refuse("ERR_BINDING_SELECTION_REQUIRED", "%s has %d bindings ob can invoke; choose one by adding --binding to your command, for example --binding %s:\n%s", key, len(usable), usable[0], listing(usable))
		}
	}
	return nxInvokeThrough(c, doc, key, chosen, c.on("frames"))
}

// nxInvokeThrough runs the exchange through one chosen binding.
func nxInvokeThrough(c *nxCtx, doc *nxObj, key, binding string, frames bool) error {
	c.banner = "ob preview: sample results; nothing was called"
	c.note(fmt.Sprintf("using %s (%s)", binding, nxKindOf(doc, binding)))
	need, needs := nxBindingNeeds[binding]
	var callContext bool
	if c.set("context") {
		if _, _, err := c.object("context"); err != nil {
			return err
		}
		callContext = true
	}
	if c.on("preflight") {
		for _, f := range []string{"input", "frames"} {
			if c.set(f) {
				return nxUsageErr("--preflight sends nothing, so it does not take --%s; it prints the binding's requirements as one JSON value", f)
			}
		}
		if !needs {
			c.note(binding + " knows of nothing it will ask for.")
			c.println("null")
			return nil
		}
		details := nxNewObj().Set("target", need.scope).Set("alternatives", []any{
			nxNewObj().Set("requirements", []any{nxNewObj().Set("type", need.requirement).Set("durable", need.durable)}),
		})
		_, stored := nxStoredContext(need.scope)
		asks := fmt.Sprintf("%s will ask for %s for %s", binding, need.describe, need.scope)
		switch {
		case stored && need.durable:
			c.note(asks + "; ob has one stored for that scope.")
		case nxInteractive(c) && nxSignIn(need):
			c.note(asks + ".")
			c.note(fmt.Sprintf("(preview: ob would run the OAuth 2.0 sign-in %s names now, in your browser, and store the tokens for %s; nothing is called)", binding, need.scope))
		case nxInteractive(c):
			c.note(asks + ".")
			c.note(fmt.Sprintf("(preview: ob would ask you for it now and store it for %s; nothing is called)", need.scope))
		case nxSignIn(need):
			c.note(fmt.Sprintf("%s; nothing is stored for that scope. Run this command at a terminal to sign in, or store a token you have:\n  %s", asks, nxRemedy(need)))
		default:
			c.note(fmt.Sprintf("%s; nothing is stored for that scope. Supply it with:\n  %s", asks, nxRemedy(need)))
		}
		c.println(nxCompact(details))
		return nil
	}
	inputs, closeInputs, err := nxInputs(c)
	if err != nil {
		return err
	}
	defer closeInputs()
	sent := 0
	checkInput := func(v any) error {
		problems, checked, err := nxCheck(doc, key, "input", v)
		if err != nil || !checked || len(problems) == 0 {
			return err
		}
		if frames {
			c.println(nxErrorFrame("ERR_OPERATION_VALIDATION_FAILED", nil))
		}
		if sent == 0 {
			return nxFail(3, "an input value does not fit %s's input schema, so nothing was sent:\n  %s", key, strings.Join(problems, "\n  "))
		}
		return nxFail(1, "input value %d does not fit %s's input schema (%s); ob stopped after sending %s, which may have taken effect", sent+1, key, strings.Join(problems, "; "), nxCount(sent, "value"))
	}
	// ob checks a value it already holds before asking for anything.
	prechecked := false
	if raw := c.str("input"); c.set("input") && raw != "-" && !strings.HasPrefix(raw, "@") {
		v, _ := nxParse(raw)
		if err := checkInput(v); err != nil {
			return err
		}
		prechecked = true
	}
	// A stream that stops being JSON ends the call: ob, as the caller, cancels.
	next := func() (any, bool, error) {
		v, ok, err := inputs()
		if err == nil {
			return v, ok, nil
		}
		if frames {
			c.println(nxErrorFrame("ERR_CANCELLED", nil))
		}
		if sent == 0 {
			return nil, false, nxFail(3, "input value 1 is not JSON (%v), so ob cancelled the call before sending anything", err)
		}
		return nil, false, nxFail(1, "input value %d is not JSON (%v), so ob cancelled the call after sending %s, which may have taken effect", sent+1, err, nxCount(sent, "value"))
	}
	if needs {
		_, stored := nxStoredContext(need.scope)
		switch {
		case callContext:
			c.note("using the context given with --context for this call")
		case stored && need.durable:
			c.note("using stored context for " + need.scope)
		case nxInteractive(c) && need.requirement == "auth.oauth2":
			c.note(fmt.Sprintf("(preview: ob would run the OAuth 2.0 sign-in %s names, in your browser, then offer to store the tokens for %s and renew them as they expire)", binding, need.scope))
		case nxInteractive(c):
			c.note(fmt.Sprintf("(preview: ob would ask you here for %s for %s, and offer to store it)", need.describe, need.scope))
		default:
			if frames {
				data := nxNewObj().Set("target", need.scope).Set("alternatives", []any{
					nxNewObj().Set("requirements", []any{nxNewObj().Set("type", need.requirement).Set("durable", need.durable)}),
				})
				c.println(nxErrorFrame("CONTEXT_REQUIRED", data))
			}
			why := fmt.Sprintf("%s needs %s for %s, and nothing is stored for that scope, so nothing was sent.", binding, need.describe, need.scope)
			if nxSignIn(need) {
				return nxFail(3, "%s\n  sign in once at a terminal:  %s\n  or store a token you have:   %s\n  or for this call:            --context @context.json", why, nxSignInRemedy(c, key, binding), nxRemedy(need))
			}
			return nxFail(3, "%s\n  store it:           %s\n  or for this call:   --context @context.json", why, nxRemedy(need))
		}
	}
	c.live()
	emit := func(v any) error {
		if problems, checked, err := nxCheck(doc, key, "output", v); err == nil && checked && len(problems) > 0 {
			if frames {
				c.println(nxErrorFrame("ERR_OPERATION_VALIDATION_FAILED", nil))
			}
			return nxFail(1, "an output value does not fit %s's output schema (%s); it was not printed, and the operation may have taken effect", key, strings.Join(problems, "; "))
		}
		if frames {
			c.println(nxCompact(nxNewObj().Set("kind", "output").Set("value", v)))
		} else {
			c.println(nxCompact(v))
		}
		return nil
	}
	switch nxBindingShape[binding] {
	case "client-stream":
		for {
			v, ok, err := next()
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			if err := checkInput(v); err != nil {
				return err
			}
			sent++
		}
		if err := emit(nxNewObj().Set("imported", sent)); err != nil {
			return err
		}
	case "server-stream":
		if frames {
			c.println(`{"kind":"input_closed"}`)
		}
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		defer signal.Stop(interrupt)
		for i, title := range []string{"Write the docs", "Review the spec", "Ship it"} {
			if i > 0 && nxTerminalOut(c) {
				select {
				case <-interrupt:
					if frames {
						c.println(nxErrorFrame("ERR_CANCELLED", nil))
					}
					return nxFail(130, "cancelled")
				case <-time.After(700 * time.Millisecond):
				}
			}
			if err := emit(nxNewObj().Set("id", fmt.Sprintf("t_%d", i+1)).Set("title", title).Set("done", i == 1)); err != nil {
				return err
			}
		}
	default:
		v, ok, err := next()
		if err != nil {
			return err
		}
		if ok {
			if !prechecked {
				if err := checkInput(v); err != nil {
					return err
				}
			}
			sent++
		} else {
			c.note("(no input value was sent)")
		}
		if frames {
			c.println(`{"kind":"input_closed"}`)
		}
		if _, more, _ := inputs(); more {
			c.note(binding + " takes one input value; ob stopped reading after the first")
		}
		if err := emit(nxUnaryResult(key, v)); err != nil {
			return err
		}
	}
	if frames {
		c.println(`{"kind":"complete"}`)
	}
	return nil
}

func nxUnaryResult(key string, input any) any {
	field := func(name, fallback string) string {
		if obj, ok := input.(*nxObj); ok {
			if s, ok := obj.Get(name).(string); ok {
				return s
			}
		}
		return fallback
	}
	switch key {
	case "createTask":
		return nxNewObj().Set("id", "t_4").Set("title", field("title", "Write the docs")).Set("done", false)
	case "listTasks":
		return nxMustParse(`[{"id":"t_1","title":"Write the docs","done":false},{"id":"t_2","title":"Review the spec","done":true}]`)
	case "completeTask":
		return nxNewObj().Set("id", field("id", "t_1")).Set("title", "Write the docs").Set("done", true)
	}
	return nxNewObj()
}

// nxInputs returns a reader of input values: one inline value, every value
// in a file, or every value on stdin as it arrives.
func nxInputs(c *nxCtx) (func() (any, bool, error), func(), error) {
	none := func() (any, bool, error) { return nil, false, nil }
	if !c.set("input") {
		return none, func() {}, nil
	}
	raw := c.str("input")
	var r io.Reader
	closer := func() {}
	switch {
	case raw == "-":
		r = c.cmd.InOrStdin()
	case strings.HasPrefix(raw, "@"):
		if raw == "@" {
			return nil, nil, nxUsageErr("--input: @ must be followed by a file path")
		}
		f, err := os.Open(raw[1:])
		if err != nil {
			return nil, nil, nxFail(3, "cannot read input file %s: %v; nothing was sent", raw[1:], err)
		}
		r, closer = f, func() { f.Close() }
	default:
		v, err := nxParse(raw)
		if err != nil {
			return nil, nil, nxUsageErr("--input: expected one JSON value, @file, or - for stdin")
		}
		done := false
		return func() (any, bool, error) {
			if done {
				return nil, false, nil
			}
			done = true
			return v, true, nil
		}, func() {}, nil
	}
	dec := json.NewDecoder(r)
	dec.UseNumber()
	return func() (any, bool, error) {
		v, err := nxDecode(dec)
		if err == io.EOF {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return v, true, nil
	}, closer, nil
}

// nxErrorFrame is an operation-invoker error frame. The codes the interface
// owns carry no data; CONTEXT_REQUIRED carries its challenge.
func nxErrorFrame(code string, data any) string {
	e := nxNewObj().Set("code", code)
	if data != nil {
		e.Set("data", data)
	}
	return nxCompact(nxNewObj().Set("kind", "error").Set("error", e))
}

func nxInteractive(c *nxCtx) bool {
	if c.str("input") == "-" {
		return false
	}
	f, ok := c.cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func nxTerminalOut(c *nxCtx) bool {
	f, ok := c.cmd.OutOrStdout().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// nxSignIn says whether meeting a requirement means running a sign-in flow,
// which only a command that knows the binding can start.
func nxSignIn(need nxNeed) bool { return need.requirement == "auth.oauth2" }

// nxSignInRemedy is the command that does a sign-in ahead of time: the same
// invocation with --preflight, run once at a terminal. It calls nothing.
func nxSignInRemedy(c *nxCtx, key, binding string) string {
	if c.variant() == "binding-invoke" {
		return fmt.Sprintf("ob invoke %s %s --preflight", c.args[0], binding)
	}
	return fmt.Sprintf("ob invoke %s %s --binding %s --preflight", c.args[0], key, binding)
}

func nxRemedy(need nxNeed) string {
	flag := map[string]string{"auth.bearer": "--bearer-token -", "auth.oauth2": "--access-token -", "auth.apiKey": "--api-key -", "auth.basic": "--basic"}[need.requirement]
	return fmt.Sprintf("ob context set '%s' %s", need.scope, flag)
}
