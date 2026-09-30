package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// The preview keeps JSON member order so that sample documents, edits, and
// diffs read the way a real ob would write them.

type nxObj struct {
	keys []string
	vals map[string]any
}

func nxNewObj() *nxObj { return &nxObj{vals: map[string]any{}} }

func (o *nxObj) Has(k string) bool { _, ok := o.vals[k]; return ok }
func (o *nxObj) Get(k string) any  { return o.vals[k] }
func (o *nxObj) Keys() []string    { return append([]string(nil), o.keys...) }
func (o *nxObj) Len() int          { return len(o.keys) }

func (o *nxObj) Obj(k string) *nxObj {
	v, _ := o.vals[k].(*nxObj)
	return v
}

// Set replaces a member in place or appends a new one.
func (o *nxObj) Set(k string, v any) *nxObj {
	if !o.Has(k) {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

// SetCanon inserts a new member at its place in canon (the spec's member
// order); unknown members sort after known ones.
func (o *nxObj) SetCanon(k string, v any, canon []string) *nxObj {
	if o.Has(k) {
		o.vals[k] = v
		return o
	}
	rank := nxRank(canon, k)
	at := len(o.keys)
	for i, existing := range o.keys {
		if nxRank(canon, existing) > rank {
			at = i
			break
		}
	}
	o.keys = append(o.keys[:at], append([]string{k}, o.keys[at:]...)...)
	o.vals[k] = v
	return o
}

func nxRank(canon []string, k string) int {
	for i, c := range canon {
		if c == k {
			return i
		}
	}
	return len(canon) + 1
}

func (o *nxObj) Delete(k string) {
	if !o.Has(k) {
		return
	}
	delete(o.vals, k)
	for i, existing := range o.keys {
		if existing == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Rename changes a key without moving it.
func (o *nxObj) Rename(from, to string) {
	for i, existing := range o.keys {
		if existing == from {
			o.keys[i] = to
			o.vals[to] = o.vals[from]
			delete(o.vals, from)
			return
		}
	}
}

func nxClone(v any) any {
	switch t := v.(type) {
	case *nxObj:
		c := nxNewObj()
		for _, k := range t.keys {
			c.Set(k, nxClone(t.vals[k]))
		}
		return c
	case []any:
		c := make([]any, len(t))
		for i, item := range t {
			c[i] = nxClone(item)
		}
		return c
	default:
		return v
	}
}

func nxParse(s string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	v, err := nxDecode(dec)
	if err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("expected one JSON value")
	}
	return v, nil
}

func nxMustParse(s string) any {
	v, err := nxParse(s)
	if err != nil {
		panic(err)
	}
	return v
}

func nxDecode(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		o := nxNewObj()
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := nxDecode(dec)
			if err != nil {
				return nil, err
			}
			o.Set(kt.(string), v)
		}
		_, err := dec.Token()
		return o, err
	case '[':
		arr := []any{}
		for dec.More() {
			v, err := nxDecode(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		_, err := dec.Token()
		return arr, err
	}
	return nil, fmt.Errorf("unexpected %v", delim)
}

func nxPretty(v any) string {
	var b strings.Builder
	nxWrite(&b, v, 0, true)
	return b.String()
}

func nxCompact(v any) string {
	var b strings.Builder
	nxWrite(&b, v, 0, false)
	return b.String()
}

func nxString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

func nxWrite(b *strings.Builder, v any, indent int, pretty bool) {
	pad := func(n int) {
		if pretty {
			b.WriteString("\n")
			b.WriteString(strings.Repeat("  ", n))
		}
	}
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		b.WriteString(t.String())
	case int:
		fmt.Fprintf(b, "%d", t)
	case string:
		b.WriteString(nxString(t))
	case []string:
		items := make([]any, len(t))
		for i, s := range t {
			items[i] = s
		}
		nxWrite(b, items, indent, pretty)
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[")
		for i, item := range t {
			if i > 0 {
				b.WriteString(",")
			}
			pad(indent + 1)
			nxWrite(b, item, indent+1, pretty)
		}
		pad(indent)
		b.WriteString("]")
	case *nxObj:
		if t.Len() == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{")
		for i, k := range t.keys {
			if i > 0 {
				b.WriteString(",")
			}
			pad(indent + 1)
			b.WriteString(nxString(k))
			if pretty {
				b.WriteString(": ")
			} else {
				b.WriteString(":")
			}
			nxWrite(b, t.vals[k], indent+1, pretty)
		}
		pad(indent)
		b.WriteString("}")
	default:
		raw, _ := json.Marshal(t)
		b.Write(raw)
	}
}

func nxYAML(v any) string {
	var node yaml.Node
	nxYAMLNode(&node, v)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(&node)
	return strings.TrimRight(buf.String(), "\n")
}

func nxYAMLNode(n *yaml.Node, v any) {
	switch t := v.(type) {
	case *nxObj:
		n.Kind = yaml.MappingNode
		for _, k := range t.keys {
			key := &yaml.Node{Kind: yaml.ScalarNode, Value: k}
			val := &yaml.Node{}
			nxYAMLNode(val, t.vals[k])
			n.Content = append(n.Content, key, val)
		}
	case []any:
		n.Kind = yaml.SequenceNode
		for _, item := range t {
			child := &yaml.Node{}
			nxYAMLNode(child, item)
			n.Content = append(n.Content, child)
		}
	case []string:
		n.Kind = yaml.SequenceNode
		for _, item := range t {
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: item})
		}
	case nil:
		n.Kind, n.Tag, n.Value = yaml.ScalarNode, "!!null", "null"
	case bool:
		n.Kind, n.Tag, n.Value = yaml.ScalarNode, "!!bool", fmt.Sprint(t)
	case json.Number:
		n.Kind, n.Tag, n.Value = yaml.ScalarNode, "!!int", t.String()
		if strings.ContainsAny(t.String(), ".eE") {
			n.Tag = "!!float"
		}
	case int:
		n.Kind, n.Tag, n.Value = yaml.ScalarNode, "!!int", fmt.Sprint(t)
	case string:
		n.Kind, n.Tag, n.Value = yaml.ScalarNode, "!!str", t
	default:
		n.Kind, n.Value = yaml.ScalarNode, fmt.Sprint(t)
	}
}

// nxDiff returns a line diff of two pretty-printed documents, with two
// lines of context around each change, in the style of `git diff`.
func nxDiff(before, after string) []string {
	a := strings.Split(before, "\n")
	b := strings.Split(after, "\n")
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type line struct {
		op   byte
		text string
	}
	var ops []line
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, line{' ', a[i]})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, line{'-', a[i]})
			i++
		default:
			ops = append(ops, line{'+', b[j]})
			j++
		}
	}
	keep := make([]bool, len(ops))
	for k, op := range ops {
		if op.op != ' ' {
			for c := k - 2; c <= k+2; c++ {
				if c >= 0 && c < len(ops) {
					keep[c] = true
				}
			}
		}
	}
	var out []string
	gap := false
	for k, op := range ops {
		if !keep[k] {
			gap = true
			continue
		}
		if gap && len(out) > 0 {
			out = append(out, "  ...")
		}
		gap = false
		out = append(out, string(op.op)+" "+op.text)
	}
	return out
}
