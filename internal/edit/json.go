package edit

import (
	"encoding/json"
	"fmt"
	"strings"
)

// jsonDoc edits JSON by parsing it into a tree that remembers where every value
// starts and ends, then splicing text there. New items follow the file's existing
// indentation.
type jsonDoc struct {
	src  []byte
	root *jnode
}

type jkind int

const (
	jObject jkind = iota
	jArray
	jString
	jNumber
	jBool
	jNull
)

type jnode struct {
	kind       jkind
	start, end int // byte span of the value
	keys       []string
	keyStarts  []int // where each key's opening quote is
	children   []*jnode
	str        string // decoded value for scalars
}

func openJSON(content []byte) (*jsonDoc, error) {
	d := &jsonDoc{src: append([]byte(nil), content...)}
	return d, d.parse()
}

func (d *jsonDoc) parse() error {
	p := &jparser{src: d.src}
	p.ws()
	n, err := p.value()
	if err != nil {
		return err
	}
	p.ws()
	if p.i != len(p.src) {
		return fmt.Errorf("unexpected text after the end at byte %d", p.i)
	}
	d.root = n
	return nil
}

func (d *jsonDoc) Bytes() []byte { return d.src }

// ── parser ──────────────────────────────────────────────────────────────────────

type jparser struct {
	src []byte
	i   int
}

func (p *jparser) ws() {
	for p.i < len(p.src) && isSpace(p.src[p.i]) {
		p.i++
	}
}

func (p *jparser) value() (*jnode, error) {
	if p.i >= len(p.src) {
		return nil, fmt.Errorf("unexpected end of file")
	}
	start := p.i
	switch p.src[p.i] {
	case '{':
		n := &jnode{kind: jObject, start: start}
		p.i++
		p.ws()
		if p.peek('}') {
			p.i++
			n.end = p.i
			return n, nil
		}
		for {
			p.ws()
			ks := p.i
			k, err := p.value()
			if err != nil {
				return nil, err
			}
			if k.kind != jString {
				return nil, fmt.Errorf("object key at byte %d isn't a string", ks)
			}
			p.ws()
			if !p.peek(':') {
				return nil, fmt.Errorf("expected : at byte %d", p.i)
			}
			p.i++
			p.ws()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.keys = append(n.keys, k.str)
			n.keyStarts = append(n.keyStarts, ks)
			n.children = append(n.children, v)
			p.ws()
			if p.peek(',') {
				p.i++
				continue
			}
			if p.peek('}') {
				p.i++
				n.end = p.i
				return n, nil
			}
			return nil, fmt.Errorf("expected , or } at byte %d", p.i)
		}
	case '[':
		n := &jnode{kind: jArray, start: start}
		p.i++
		p.ws()
		if p.peek(']') {
			p.i++
			n.end = p.i
			return n, nil
		}
		for {
			p.ws()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, v)
			p.ws()
			if p.peek(',') {
				p.i++
				continue
			}
			if p.peek(']') {
				p.i++
				n.end = p.i
				return n, nil
			}
			return nil, fmt.Errorf("expected , or ] at byte %d", p.i)
		}
	case '"':
		p.i++
		for p.i < len(p.src) && p.src[p.i] != '"' {
			if p.src[p.i] == '\\' {
				p.i++
			}
			p.i++
		}
		if p.i >= len(p.src) {
			return nil, fmt.Errorf("unclosed string at byte %d", start)
		}
		p.i++
		var s string
		if err := json.Unmarshal(p.src[start:p.i], &s); err != nil {
			return nil, err
		}
		return &jnode{kind: jString, start: start, end: p.i, str: s}, nil
	default:
		for p.i < len(p.src) && !isSpace(p.src[p.i]) && !strings.ContainsRune(",]}:", rune(p.src[p.i])) {
			p.i++
		}
		lit := string(p.src[start:p.i])
		var kind jkind
		switch lit {
		case "true", "false":
			kind = jBool
		case "null":
			kind = jNull
		default:
			var f float64
			if err := json.Unmarshal([]byte(lit), &f); err != nil {
				return nil, fmt.Errorf("unexpected %q at byte %d", lit, start)
			}
			kind = jNumber
		}
		return &jnode{kind: kind, start: start, end: p.i, str: lit}, nil
	}
}

func (p *jparser) peek(c byte) bool { return p.i < len(p.src) && p.src[p.i] == c }

// ── lookups ─────────────────────────────────────────────────────────────────────

func (n *jnode) field(k string) (*jnode, int) {
	for i, key := range n.keys {
		if key == k {
			return n.children[i], i
		}
	}
	return nil, -1
}

func (n *jnode) scalarFields() map[string]string {
	out := map[string]string{}
	for i, k := range n.keys {
		if c := n.children[i]; c.kind != jObject && c.kind != jArray {
			out[k] = c.str
		}
	}
	return out
}

func (n *jnode) kindForEdit() Kind {
	switch n.kind {
	case jNumber:
		return KindNumber
	case jBool:
		return KindBool
	}
	return KindString
}

func (d *jsonDoc) resolve(p Path) (*jnode, error) {
	n := d.root
	for i, s := range p {
		switch {
		case s.Match != nil:
			if n.kind != jArray {
				return nil, fmt.Errorf("%s isn't a list, so it can't be matched with %s", p[:i].String(), s)
			}
			var found []*jnode
			for _, c := range n.children {
				if c.kind == jObject && matchesAll(c.scalarFields(), s.Match) {
					found = append(found, c)
				}
			}
			if len(found) != 1 {
				return nil, fmt.Errorf("%s matched %d items in %s — it needs to match exactly one", s, len(found), p[:i].String())
			}
			n = found[0]
		case s.IsIndex:
			if n.kind != jArray || s.Index < 0 || s.Index >= len(n.children) {
				return nil, errNotFound(p, i)
			}
			n = n.children[s.Index]
		default:
			if n.kind != jObject {
				return nil, errNotFound(p, i)
			}
			c, _ := n.field(s.Key)
			if c == nil {
				return nil, errNotFound(p, i)
			}
			n = c
		}
	}
	return n, nil
}

func (d *jsonDoc) Get(p Path) (string, error) {
	n, err := d.resolve(p)
	if err != nil {
		return "", err
	}
	if n.kind == jObject || n.kind == jArray {
		return "", fmt.Errorf("%s is a list or map, not a single value", p)
	}
	return n.str, nil
}

func (d *jsonDoc) Items(p Path, show string) ([]string, error) {
	n, err := d.resolve(p)
	if err != nil {
		return nil, err
	}
	var out []string
	switch n.kind {
	case jArray:
		for _, c := range n.children {
			switch c.kind {
			case jObject:
				if show == "" {
					return nil, fmt.Errorf("items in %s have several fields — set show = \"<field>\" to pick which one to list", p)
				}
				if v, ok := c.scalarFields()[show]; ok {
					out = append(out, v)
				}
			case jArray:
			default:
				out = append(out, c.str)
			}
		}
	case jObject:
		out = append(out, n.keys...)
	default:
		return nil, fmt.Errorf("%s isn't a list or map", p)
	}
	return out, nil
}

// ── rendering ───────────────────────────────────────────────────────────────────

func jsonScalar(v string, like Kind) string {
	if k := kindFor(v, like); k != KindString {
		return v
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// indentUnit guesses the file's indentation step (default two spaces).
func (d *jsonDoc) indentUnit() string {
	for _, line := range strings.Split(string(d.src), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed != "" && len(trimmed) < len(line) {
			return line[:len(line)-len(trimmed)]
		}
	}
	return "  "
}

func (n *jnode) multiline(src []byte) bool {
	return strings.Contains(string(src[n.start:n.end]), "\n")
}

// jsonItem renders an item. sample is an existing sibling (for field order and
// types); indent is the indentation of the line the item starts on, used for
// objects written across several lines.
func (d *jsonDoc) jsonItem(it Item, sample *jnode, indent string, multi bool) string {
	if !it.IsObject() {
		like := KindString
		if sample != nil {
			like = sample.kindForEdit()
		}
		return jsonScalar(it.Value, like)
	}
	var order []string
	kinds := map[string]Kind{}
	if sample != nil && sample.kind == jObject {
		order = sample.keys
		for i, k := range sample.keys {
			kinds[k] = sample.children[i].kindForEdit()
		}
	}
	keys := it.orderedKeys(order)
	parts := make([]string, len(keys))
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		parts[i] = string(kb) + ": " + jsonScalar(it.Fields[k], kinds[k])
	}
	if !multi {
		return "{" + strings.Join(parts, ", ") + "}"
	}
	unit := d.indentUnit()
	return "{\n" + indent + unit + strings.Join(parts, ",\n"+indent+unit) + "\n" + indent + "}"
}

// insertInto adds text (already rendered) as the last element of an array or object.
func (d *jsonDoc) insertInto(n *jnode, text string, multiItem bool) {
	if len(n.children) == 0 {
		// Empty: [] or {} — open it up across lines if the parent is multi-line.
		indent := indentOf(d.src, n.start)
		if d.root.multiline(d.src) {
			unit := d.indentUnit()
			d.src = splice(d.src, n.start+1, n.end-1, "\n"+indent+unit+text+"\n"+indent)
		} else {
			d.src = splice(d.src, n.start+1, n.end-1, text)
		}
		return
	}
	last := n.children[len(n.children)-1]
	if n.multiline(d.src) {
		indent := indentOf(d.src, d.lineStartFor(n, len(n.children)-1))
		d.src = splice(d.src, last.end, last.end, ",\n"+indent+text)
		return
	}
	d.src = splice(d.src, last.end, last.end, ", "+text)
}

// lineStartFor returns an offset on the line where child i (or its key) starts.
func (d *jsonDoc) lineStartFor(n *jnode, i int) int {
	if n.kind == jObject {
		return n.keyStarts[i]
	}
	return n.children[i].start
}

// removeAt deletes child i of an array or object, with the comma next to it.
func (d *jsonDoc) removeAt(n *jnode, i int) {
	start := d.lineStartFor(n, i)
	end := n.children[i].end
	switch {
	case len(n.children) == 1:
		d.src = splice(d.src, n.start+1, n.end-1, "")
	case i < len(n.children)-1:
		d.src = splice(d.src, start, d.lineStartFor(n, i+1), "")
	default:
		d.src = splice(d.src, n.children[i-1].end, end, "")
	}
}

// ── Apply ───────────────────────────────────────────────────────────────────────

func (d *jsonDoc) Apply(op Op) (Change, error) {
	ch, err := d.apply(op)
	if err != nil {
		return Change{}, err
	}
	if perr := d.parse(); perr != nil {
		return Change{}, fmt.Errorf("the change to %s produced invalid JSON (please report this): %w", op.Path, perr)
	}
	return ch, nil
}

func (d *jsonDoc) apply(op Op) (Change, error) {
	n, err := d.resolve(op.Path)
	if err != nil {
		return Change{}, err
	}
	what := op.Path.String()
	switch op.Action {
	case "set":
		if n.kind == jObject || n.kind == jArray {
			return Change{}, fmt.Errorf("%s is a list or map, not a single value", op.Path)
		}
		if n.str == op.Value {
			return Change{}, nil
		}
		d.src = splice(d.src, n.start, n.end, jsonScalar(op.Value, n.kindForEdit()))
		return Change{What: what, From: n.str, To: op.Value, Verb: "changed"}, nil

	case "append":
		if n.kind != jArray {
			return Change{}, fmt.Errorf("%s isn't a list", op.Path)
		}
		var sample *jnode
		if len(n.children) > 0 {
			sample = n.children[len(n.children)-1]
		}
		multi := sample != nil && sample.multiline(d.src)
		indent := indentOf(d.src, n.start) + d.indentUnit()
		if sample != nil {
			indent = indentOf(d.src, sample.start)
		}
		d.insertInto(n, d.jsonItem(op.Item, sample, indent, multi), multi)
		return Change{What: what, To: describeItem(op.Item), Verb: "added"}, nil

	case "remove":
		if n.kind != jArray {
			return Change{}, fmt.Errorf("%s isn't a list", op.Path)
		}
		var idx []int
		for i, c := range n.children {
			if jsonItemMatches(c, op.Match) {
				idx = append(idx, i)
			}
		}
		if len(idx) == 0 {
			return Change{}, fmt.Errorf("nothing in %s matches %s", op.Path, describeMatch(op.Match))
		}
		for k := len(idx) - 1; k >= 0; k-- {
			d.removeAt(n, idx[k])
			if err := d.parse(); err != nil {
				return Change{}, err
			}
			if n, err = d.resolve(op.Path); err != nil {
				return Change{}, err
			}
		}
		return Change{What: what, From: describeMatch(op.Match), Verb: "removed"}, nil

	case "put":
		if n.kind != jObject {
			return Change{}, fmt.Errorf("%s isn't a map", op.Path)
		}
		if c, _ := n.field(op.Name); c != nil {
			return Change{}, fmt.Errorf("%s already has %q", op.Path, op.Name)
		}
		var sample *jnode
		if len(n.children) > 0 {
			sample = n.children[len(n.children)-1]
		}
		multi := sample != nil && sample.multiline(d.src)
		indent := indentOf(d.src, n.start) + d.indentUnit()
		if len(n.children) > 0 {
			indent = indentOf(d.src, n.keyStarts[len(n.keyStarts)-1])
		}
		kb, _ := json.Marshal(op.Name)
		d.insertInto(n, string(kb)+": "+d.jsonItem(op.Item, sample, indent, multi), multi)
		return Change{What: what, To: op.Name + " = " + describeItem(op.Item), Verb: "added"}, nil

	case "delete":
		if n.kind != jObject {
			return Change{}, fmt.Errorf("%s isn't a map", op.Path)
		}
		_, i := n.field(op.Name)
		if i < 0 {
			return Change{}, fmt.Errorf("%s has no %q", op.Path, op.Name)
		}
		d.removeAt(n, i)
		return Change{What: what, From: op.Name, Verb: "removed"}, nil
	}
	return Change{}, fmt.Errorf("unknown action %q", op.Action)
}

func jsonItemMatches(c *jnode, match map[string]string) bool {
	switch c.kind {
	case jObject:
		return matchesAll(c.scalarFields(), match)
	case jArray:
		return false
	default:
		v, ok := match["value"]
		return ok && len(match) == 1 && c.str == v
	}
}
