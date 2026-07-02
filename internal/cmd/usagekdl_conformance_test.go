package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/openbindings/openbindings-go/formats/usage"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestUsageKDLMatchesCommandTree cross-references the three sources of truth
// for the CLI surface: the cobra command tree (code), usage.kdl (the CLI's
// usage doc and binding source), and the root contract's operations
// (../../ob.obi.json). The bound OBI (internal/app/ob.obi.json) is generated
// from the latter two, so keeping these three aligned keeps every surface
// aligned. Sibling to TestBoundCLIConformsToContract in internal/app.
//
// Checked, per direction:
//   - tree drift: a command present in kdl but not cobra, or vice versa
//   - alias drift: kdl alias nodes vs cobra Aliases
//   - flag drift: a flag present in only one surface, or a shorthand /
//     takes-value mismatch; kdl entries for root-persistent flags (-o, -F)
//     are optional per command but must match the root definition
//   - arg-arity drift: required/optional/variadic counts in kdl args vs the
//     cobra Use string placeholders (<required> [optional] name...)
//   - opKey drift: kdl opKey missing from the contract (orphan), duplicated,
//     a kdl leaf with no opKey, or a contract op no kdl command binds
//     (phantom — the syncInterface bug class)
//   - wireInput coherence: a wireInput prop (machine lane: the operation's
//     whole wire input rides the named flag as JSON; boundgen emits the
//     matching inputTransform) must name a value-taking flag declared on
//     the same command
//
// Excluded: cobra builtins (help, completion) and the root meta-flags
// (--openbindings, --usage-spec), which are CLI plumbing, not operations.
func TestUsageKDLMatchesCommandTree(t *testing.T) {
	spec, err := usage.ParseKDL([]byte(embeddedUsageSpec))
	if err != nil {
		t.Fatalf("parse usage.kdl: %v", err)
	}
	kdl := collectKDLCommands(spec)

	root := NewRoot()
	cob := map[string]cliCommand{}
	collectCobraCommands(root, nil, cob)

	shortNames, err := contractShortNames("../../ob.obi.json")
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}

	rootGlobals := flagSetInfo(root.PersistentFlags())

	var problems []string
	addf := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	// Tree shape: same command paths on both sides.
	for path := range kdl {
		if _, ok := cob[path]; !ok {
			addf("command %q: in usage.kdl but not in the cobra tree", path)
		}
	}
	for path := range cob {
		if _, ok := kdl[path]; !ok {
			addf("command %q: in the cobra tree but not in usage.kdl", path)
		}
	}

	seenOpKeys := map[string]string{} // opKey -> command path
	for path, kc := range kdl {
		cc, ok := cob[path]
		if !ok {
			continue // tree drift already reported
		}

		// Aliases.
		if d := diffSets(kc.aliases, cc.aliases); d != "" {
			addf("command %q: alias drift: %s", path, d)
		}

		// Group vs leaf, opKey wiring.
		if len(kc.subcommands) > 0 {
			if !kc.subcommandRequired {
				addf("command %q: has subcommands in usage.kdl but no subcommand_required=#true", path)
			}
			if kc.opKey != "" {
				addf("command %q: group commands must not carry an opKey (got %q)", path, kc.opKey)
			}
		} else {
			if kc.opKey == "" {
				addf("command %q: leaf command has no opKey (every CLI command must bind a contract operation)", path)
			} else {
				if prev, dup := seenOpKeys[kc.opKey]; dup {
					addf("command %q: opKey %q already used by %q", path, kc.opKey, prev)
				}
				seenOpKeys[kc.opKey] = path
				if !shortNames[kc.opKey] {
					addf("command %q: opKey %q has no matching operation in the contract (orphan)", path, kc.opKey)
				}
			}
		}

		// Flags. Pair kdl flags against the command's own flags first; kdl
		// entries that instead document a root-persistent flag must match its
		// definition. Everything else is drift, in either direction.
		matchedLocal := map[string]bool{}
		for name, kf := range kc.flags {
			if cf, ok := cc.flags[name]; ok {
				matchedLocal[name] = true
				if kf.short != cf.short {
					addf("command %q: flag --%s: shorthand mismatch (kdl %q, cobra %q)", path, name, kf.short, cf.short)
				}
				if kf.takesValue != cf.takesValue {
					addf("command %q: flag --%s: takes-value mismatch (kdl %v, cobra %v)", path, name, kf.takesValue, cf.takesValue)
				}
				continue
			}
			if gf, ok := rootGlobals[name]; ok {
				if kf.short != gf.short || kf.takesValue != gf.takesValue {
					addf("command %q: flag --%s: does not match the root persistent definition (kdl short %q value %v; root short %q value %v)",
						path, name, kf.short, kf.takesValue, gf.short, gf.takesValue)
				}
				continue
			}
			addf("command %q: flag --%s: in usage.kdl but not in the cobra tree", path, name)
		}
		for name := range cc.flags {
			if !matchedLocal[name] {
				addf("command %q: flag --%s: in the cobra tree but not in usage.kdl", path, name)
			}
		}

		// Arg arity.
		if kc.args != cc.args {
			addf("command %q: arg arity mismatch (kdl %s, cobra Use %s)", path, kc.args, cc.args)
		}

		// wireInput coherence.
		if kc.wireInput != "" {
			if kc.opKey == "" {
				addf("command %q: wireInput without an opKey (nothing to transform)", path)
			}
			if f, ok := kc.flags[kc.wireInput]; !ok {
				addf("command %q: wireInput %q names no flag on the command", path, kc.wireInput)
			} else if !f.takesValue {
				addf("command %q: wireInput flag --%s must take a value", path, kc.wireInput)
			}
		}
	}

	// Phantom ops: contract operations no CLI command binds.
	for short := range shortNames {
		if _, ok := seenOpKeys[short]; !ok {
			addf("contract operation %q: no usage.kdl command binds it (phantom — add a command or remove the op)", short)
		}
	}

	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

type flagInfo struct {
	short      string
	takesValue bool
}

// arity is a comparable summary of a command's positional arguments.
type arity struct {
	required int
	optional int
	variadic bool
}

func (a arity) String() string {
	return fmt.Sprintf("(required=%d optional=%d variadic=%v)", a.required, a.optional, a.variadic)
}

type cliCommand struct {
	aliases            []string
	flags              map[string]flagInfo
	args               arity
	opKey              string
	wireInput          string
	subcommands        []string
	subcommandRequired bool
}

func collectKDLCommands(spec *usage.Spec) map[string]cliCommand {
	out := map[string]cliCommand{}
	spec.Walk(func(path []string, cmd usage.Command) {
		c := cliCommand{
			flags:              map[string]flagInfo{},
			opKey:              cmd.Node.Props["opKey"].String(),
			wireInput:          cmd.Node.Props["wireInput"].String(),
			subcommandRequired: cmd.SubcommandRequired,
		}
		for _, sub := range cmd.Commands {
			c.subcommands = append(c.subcommands, sub.Name)
		}
		for _, a := range cmd.Aliases {
			c.aliases = append(c.aliases, a.Names...)
		}
		for _, f := range cmd.Flags {
			p := f.ParseUsage()
			name := ""
			if len(p.Long) > 0 {
				name = p.Long[0]
			} else if len(p.Short) > 0 {
				name = p.Short[0]
			}
			short := ""
			if len(p.Short) > 0 {
				short = p.Short[0]
			}
			c.flags[name] = flagInfo{short: short, takesValue: p.ArgName != ""}
		}
		for _, a := range cmd.Args {
			if a.IsVariadic() {
				c.args.variadic = true
			}
			if a.IsRequired() {
				c.args.required++
			} else {
				c.args.optional++
			}
		}
		out[strings.Join(path, " ")] = c
	})
	return out
}

func collectCobraCommands(c *cobra.Command, path []string, out map[string]cliCommand) {
	for _, sub := range c.Commands() {
		name := sub.Name()
		if name == "help" || name == "completion" {
			continue
		}
		subPath := append(append([]string{}, path...), name)

		cc := cliCommand{
			aliases: sub.Aliases,
			flags:   flagSetInfo(sub.Flags(), sub.PersistentFlags()),
			args:    parseUseArity(sub.Use),
		}
		for _, s := range sub.Commands() {
			cc.subcommands = append(cc.subcommands, s.Name())
		}
		out[strings.Join(subPath, " ")] = cc

		collectCobraCommands(sub, subPath, out)
	}
}

func flagSetInfo(sets ...*pflag.FlagSet) map[string]flagInfo {
	out := map[string]flagInfo{}
	for _, fs := range sets {
		fs.VisitAll(func(f *pflag.Flag) {
			out[f.Name] = flagInfo{
				short:      f.Shorthand,
				takesValue: f.Value.Type() != "bool" && f.Value.Type() != "count",
			}
		})
	}
	return out
}

// parseUseArity reads positional-argument placeholders from a cobra Use
// string: <name> is required, [name] is optional, a trailing ... marks the
// command variadic. The first token is the command name itself.
func parseUseArity(use string) arity {
	var a arity
	fields := strings.Fields(use)
	if len(fields) < 2 {
		return a
	}
	for _, tok := range fields[1:] {
		if strings.Contains(tok, "...") {
			a.variadic = true
		}
		switch {
		case strings.HasPrefix(tok, "<"):
			a.required++
		case strings.HasPrefix(tok, "["):
			a.optional++
		}
	}
	return a
}

// contractShortNames loads the root contract and returns the set of operation
// short-names (the segment after the openbindings.ob. namespace), which is
// what usage.kdl opKeys refer to.
func contractShortNames(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Operations map[string]json.RawMessage `json:"operations"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	const ns = "openbindings.ob."
	out := make(map[string]bool, len(doc.Operations))
	for key := range doc.Operations {
		short, ok := strings.CutPrefix(key, ns)
		if !ok {
			return nil, fmt.Errorf("operation %q does not carry the %s namespace", key, ns)
		}
		out[short] = true
	}
	return out, nil
}

// diffSets reports elements present in only one of two string sets; empty
// string means the sets are equal.
func diffSets(kdlSide, cobraSide []string) string {
	k := map[string]bool{}
	for _, s := range kdlSide {
		k[s] = true
	}
	c := map[string]bool{}
	for _, s := range cobraSide {
		c[s] = true
	}
	var onlyKDL, onlyCobra []string
	for s := range k {
		if !c[s] {
			onlyKDL = append(onlyKDL, s)
		}
	}
	for s := range c {
		if !k[s] {
			onlyCobra = append(onlyCobra, s)
		}
	}
	if len(onlyKDL) == 0 && len(onlyCobra) == 0 {
		return ""
	}
	sort.Strings(onlyKDL)
	sort.Strings(onlyCobra)
	var parts []string
	if len(onlyKDL) > 0 {
		parts = append(parts, fmt.Sprintf("only in usage.kdl: %v", onlyKDL))
	}
	if len(onlyCobra) > 0 {
		parts = append(parts, fmt.Sprintf("only in cobra: %v", onlyCobra))
	}
	return strings.Join(parts, "; ")
}
