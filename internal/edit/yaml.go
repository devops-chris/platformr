package edit

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// yamlDoc edits YAML by locating nodes with yaml.v3 (for their positions) and then
// splicing text at those positions, so comments and formatting are untouched. The
// file is re-parsed after every change so positions are always current.
type yamlDoc struct {
	src  []byte
	root *yaml.Node
}

func openYAML(content []byte) (*yamlDoc, error) {
	d := &yamlDoc{src: append([]byte(nil), content...)}
	return d, d.parse()
}

func (d *yamlDoc) parse() error {
	var n yaml.Node
	if err := yaml.Unmarshal(d.src, &n); err != nil {
		return err
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		d.root = n.Content[0]
	} else {
		d.root = &n
	}
	return nil
}

func (d *yamlDoc) Bytes() []byte { return d.src }

// offset converts a node's 1-based line/column (columns count characters) to a byte offset.
func (d *yamlDoc) offset(n *yaml.Node) int {
	starts := lineStarts(d.src)
	if n.Line-1 >= len(starts) {
		return len(d.src)
	}
	off := starts[n.Line-1]
	for col := 1; col < n.Column && off < len(d.src); col++ {
		_, size := utf8.DecodeRune(d.src[off:])
		off += size
	}
	return off
}

// resolve walks p and returns the node there, the mapping key node for it (if the
// last step was a name), and the parent collection.
func (d *yamlDoc) resolve(p Path) (node, key, parent *yaml.Node, err error) {
	node = d.root
	for i, s := range p {
		parent = node
		key = nil
		switch {
		case s.Match != nil:
			if node.Kind != yaml.SequenceNode {
				return nil, nil, nil, fmt.Errorf("%s isn't a list, so it can't be matched with %s", p[:i].String(), s)
			}
			var found []*yaml.Node
			for _, item := range node.Content {
				if item.Kind == yaml.MappingNode && matchesAll(yamlScalarFields(item), s.Match) {
					found = append(found, item)
				}
			}
			if len(found) != 1 {
				return nil, nil, nil, fmt.Errorf("%s matched %d items in %s — it needs to match exactly one", s, len(found), p[:i].String())
			}
			node = found[0]
		case s.IsIndex:
			if node.Kind != yaml.SequenceNode || s.Index < 0 || s.Index >= len(node.Content) {
				return nil, nil, nil, errNotFound(p, i)
			}
			node = node.Content[s.Index]
		default:
			if node.Kind != yaml.MappingNode {
				return nil, nil, nil, errNotFound(p, i)
			}
			var next *yaml.Node
			for j := 0; j+1 < len(node.Content); j += 2 {
				if node.Content[j].Value == s.Key {
					key, next = node.Content[j], node.Content[j+1]
					break
				}
			}
			if next == nil {
				return nil, nil, nil, errNotFound(p, i)
			}
			node = next
		}
	}
	return node, key, parent, nil
}

func yamlScalarFields(m *yaml.Node) map[string]string {
	out := map[string]string{}
	for j := 0; j+1 < len(m.Content); j += 2 {
		if v := m.Content[j+1]; v.Kind == yaml.ScalarNode {
			out[m.Content[j].Value] = v.Value
		}
	}
	return out
}

func yamlKind(n *yaml.Node) Kind {
	switch n.ShortTag() {
	case "!!int", "!!float":
		return KindNumber
	case "!!bool":
		return KindBool
	}
	return KindString
}

func (d *yamlDoc) Get(p Path) (string, error) {
	n, _, _, err := d.resolve(p)
	if err != nil {
		return "", err
	}
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("%s is a list or map, not a single value", p)
	}
	return n.Value, nil
}

func (d *yamlDoc) Items(p Path, show string) ([]string, error) {
	n, _, _, err := d.resolve(p)
	if err != nil {
		return nil, err
	}
	var out []string
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			switch item.Kind {
			case yaml.ScalarNode:
				out = append(out, item.Value)
			case yaml.MappingNode:
				if show == "" {
					return nil, fmt.Errorf("items in %s have several fields — set show = \"<field>\" to pick which one to list", p)
				}
				if v, ok := yamlScalarFields(item)[show]; ok {
					out = append(out, v)
				}
			}
		}
	case yaml.MappingNode:
		for j := 0; j < len(n.Content); j += 2 {
			out = append(out, n.Content[j].Value)
		}
	default:
		return nil, fmt.Errorf("%s isn't a list or map", p)
	}
	return out, nil
}

func (d *yamlDoc) Apply(op Op) (Change, error) {
	var (
		ch  Change
		err error
	)
	switch op.Action {
	case "set":
		ch, err = d.set(op)
	case "append":
		ch, err = d.appendItem(op)
	case "remove":
		ch, err = d.remove(op)
	case "put":
		ch, err = d.put(op)
	case "delete":
		ch, err = d.del(op)
	default:
		return Change{}, fmt.Errorf("unknown action %q", op.Action)
	}
	if err != nil {
		return Change{}, err
	}
	if perr := d.parse(); perr != nil {
		return Change{}, fmt.Errorf("the change to %s produced invalid YAML (please report this): %w", op.Path, perr)
	}
	return ch, nil
}

// ── set ──────────────────────────────────────────────────────────────────────────

func (d *yamlDoc) set(op Op) (Change, error) {
	n, _, _, err := d.resolve(op.Path)
	if err != nil {
		return Change{}, err
	}
	if n.Kind != yaml.ScalarNode {
		return Change{}, fmt.Errorf("%s is a list or map, not a single value", op.Path)
	}
	if n.Value == op.Value {
		return Change{}, nil
	}
	start, end, err := d.scalarSpan(n)
	if err != nil {
		return Change{}, fmt.Errorf("%s: %w", op.Path, err)
	}
	d.src = splice(d.src, start, end, yamlScalar(op.Value, yamlKind(n), n.Style))
	return Change{What: op.Path.String(), From: n.Value, To: op.Value, Verb: "changed"}, nil
}

// scalarSpan finds where a single-line scalar's text starts and ends in the source.
func (d *yamlDoc) scalarSpan(n *yaml.Node) (int, int, error) {
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return 0, 0, fmt.Errorf("multi-line text values (| or >) can't be changed by platformr")
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return 0, 0, fmt.Errorf("values with an explicit !!tag can't be changed by platformr")
	}
	start := d.offset(n)
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		for i := start + 1; i < len(d.src); i++ {
			if d.src[i] == '\\' {
				i++
				continue
			}
			if d.src[i] == '"' {
				return start, i + 1, nil
			}
		}
	case n.Style&yaml.SingleQuotedStyle != 0:
		for i := start + 1; i < len(d.src); i++ {
			if d.src[i] == '\'' {
				if i+1 < len(d.src) && d.src[i+1] == '\'' {
					i++
					continue
				}
				return start, i + 1, nil
			}
		}
	default:
		if bytes.HasPrefix(d.src[start:], []byte(n.Value)) {
			return start, start + len(n.Value), nil
		}
		return 0, 0, fmt.Errorf("values that wrap onto several lines can't be changed by platformr")
	}
	return 0, 0, fmt.Errorf("couldn't find where the value ends")
}

// yamlScalar writes v the way the value it replaces (or sits next to) was written.
func yamlScalar(v string, like Kind, style yaml.Style) string {
	if k := kindFor(v, like); k != KindString {
		return v
	}
	switch {
	case style&yaml.SingleQuotedStyle != 0:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case style&yaml.DoubleQuotedStyle != 0:
		return strconv.Quote(v)
	}
	if yamlPlainSafe(v) {
		return v
	}
	return strconv.Quote(v)
}

// yamlPlainSafe reports whether v can be written unquoted and still read back as
// exactly the same string (not a number, boolean, null, or something with syntax).
func yamlPlainSafe(v string) bool {
	if v == "" || strings.ContainsAny(v, "\n\"'") || strings.TrimSpace(v) != v {
		return false
	}
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("k: "+v), &n); err != nil || len(n.Content) == 0 {
		return false
	}
	m := n.Content[0]
	if len(m.Content) != 2 {
		return false
	}
	val := m.Content[1]
	return val.Kind == yaml.ScalarNode && val.ShortTag() == "!!str" && val.Value == v && val.Style == 0
}

// ── block collections: where each entry starts and ends ─────────────────────────

type yamlEntry struct {
	start, end int // byte range of whole lines, end just past the last line's newline
	indent     int // column (bytes) of the dash or key
}

// entryAt returns the whole-line range of a block entry whose first token (dash or
// key) is at offset tok. The entry continues over blank lines and lines indented
// more than tok's column, and stops at the first content line indented the same or less.
func (d *yamlDoc) entryAt(tok int) yamlEntry {
	start := lineStartOf(d.src, tok)
	indent := tok - start
	pos := lineEndOf(d.src, tok)
	end := pos
	for pos < len(d.src) {
		next := lineEndOf(d.src, pos)
		line := d.src[pos:next]
		trimmed := bytes.TrimLeft(line, " \t")
		if len(bytes.TrimSpace(trimmed)) != 0 {
			if len(line)-len(trimmed) <= indent {
				break
			}
			end = next
		}
		pos = next
	}
	return yamlEntry{start: start, end: end, indent: indent}
}

// seqEntry finds the entry of a block-sequence item, starting from its dash.
func (d *yamlDoc) seqEntry(item *yaml.Node) (yamlEntry, error) {
	i := d.offset(item) - 1
	for i >= 0 && (d.src[i] == ' ' || d.src[i] == '\t' || d.src[i] == '\n' || d.src[i] == '\r') {
		i--
	}
	if i < 0 || d.src[i] != '-' {
		return yamlEntry{}, fmt.Errorf("couldn't find the list item's dash")
	}
	return d.entryAt(i), nil
}

func isFlow(n *yaml.Node) bool { return n.Style&yaml.FlowStyle != 0 }

// ── flow collections: [a, b] and {a: 1} ─────────────────────────────────────────

// flowParts splits the inside of a flow collection opening at src[open] into its
// top-level comma-separated parts (trimmed spans), and returns the closing offset.
func flowParts(src []byte, open int) (parts [][2]int, close int, err error) {
	depth := 0
	partStart := open + 1
	addPart := func(end int) {
		s, e := partStart, end
		for s < e && isSpace(src[s]) {
			s++
		}
		for e > s && isSpace(src[e-1]) {
			e--
		}
		if e > s {
			parts = append(parts, [2]int{s, e})
		}
	}
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '"', '\'':
			j := i + 1
			for ; j < len(src); j++ {
				if c == '"' && src[j] == '\\' {
					j++
					continue
				}
				if src[j] == c {
					if c == '\'' && j+1 < len(src) && src[j+1] == '\'' {
						j++
						continue
					}
					break
				}
			}
			i = j
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				addPart(i)
				return parts, i, nil
			}
		case ',':
			if depth == 1 {
				addPart(i)
				partStart = i + 1
			}
		}
	}
	return nil, 0, fmt.Errorf("unclosed [ or {")
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// flowInsert adds text as a new last part of the flow collection opening at open.
func flowInsert(src []byte, open int, text string) ([]byte, error) {
	parts, close, err := flowParts(src, open)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return splice(src, open+1, close, text), nil
	}
	last := parts[len(parts)-1][1]
	return splice(src, last, last, ", "+text), nil
}

// flowRemove deletes part i (and one adjacent comma) from the flow collection at open.
func flowRemove(src []byte, open, i int) ([]byte, error) {
	parts, close, err := flowParts(src, open)
	if err != nil {
		return nil, err
	}
	switch {
	case len(parts) == 1:
		return splice(src, open+1, close, ""), nil
	case i < len(parts)-1:
		return splice(src, parts[i][0], parts[i+1][0], ""), nil
	default:
		return splice(src, parts[i-1][1], parts[i][1], ""), nil
	}
}

// ── rendering new items ─────────────────────────────────────────────────────────

func yamlFieldOrderAndKinds(sample *yaml.Node) ([]string, map[string]Kind) {
	var order []string
	kinds := map[string]Kind{}
	if sample != nil && sample.Kind == yaml.MappingNode {
		for j := 0; j+1 < len(sample.Content); j += 2 {
			order = append(order, sample.Content[j].Value)
			kinds[sample.Content[j].Value] = yamlKind(sample.Content[j+1])
		}
	}
	return order, kinds
}

// yamlFlowItem renders an item for a [..] or {..} collection.
func yamlFlowItem(it Item, sample *yaml.Node) string {
	if !it.IsObject() {
		like := KindString
		if sample != nil {
			like = yamlKind(sample)
		}
		return yamlScalar(it.Value, like, 0)
	}
	order, kinds := yamlFieldOrderAndKinds(sample)
	var parts []string
	for _, k := range it.orderedKeys(order) {
		parts = append(parts, yamlKey(k)+": "+yamlScalar(it.Fields[k], kinds[k], 0))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// yamlBlockFields renders an object's fields as lines at the given indent.
func yamlBlockFields(it Item, sample *yaml.Node, firstPrefix, indent string) string {
	order, kinds := yamlFieldOrderAndKinds(sample)
	var b strings.Builder
	for i, k := range it.orderedKeys(order) {
		if i == 0 {
			b.WriteString(firstPrefix)
		} else {
			b.WriteString(indent)
		}
		b.WriteString(yamlKey(k) + ": " + yamlScalar(it.Fields[k], kinds[k], 0) + "\n")
	}
	return b.String()
}

func yamlKey(k string) string {
	if yamlPlainSafe(k) {
		return k
	}
	return strconv.Quote(k)
}

// ensureTrailingNewline makes sure the text before pos ends a line.
func (d *yamlDoc) ensureNewlineAt(pos int) int {
	if pos > 0 && d.src[pos-1] != '\n' {
		d.src = splice(d.src, pos, pos, "\n")
		return pos + 1
	}
	return pos
}

// ── append / remove (lists) ──────────────────────────────────────────────────────

func (d *yamlDoc) appendItem(op Op) (Change, error) {
	n, key, _, err := d.resolve(op.Path)
	if err != nil {
		return Change{}, err
	}
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null" && key != nil {
		return Change{}, fmt.Errorf("%s is empty — write it as %s: [] in the file first", op.Path, key.Value)
	}
	if n.Kind != yaml.SequenceNode {
		return Change{}, fmt.Errorf("%s isn't a list", op.Path)
	}
	var sample *yaml.Node
	if len(n.Content) > 0 {
		sample = n.Content[len(n.Content)-1]
	}
	desc := describeItem(op.Item)
	if isFlow(n) {
		d.src, err = flowInsert(d.src, d.offset(n), yamlFlowItem(op.Item, sample))
		if err != nil {
			return Change{}, err
		}
		return Change{What: op.Path.String(), To: desc, Verb: "added"}, nil
	}
	last, err := d.seqEntry(sample)
	if err != nil {
		return Change{}, err
	}
	indent := strings.Repeat(" ", last.indent)
	var text string
	if op.Item.IsObject() {
		text = yamlBlockFields(op.Item, sample, indent+"- ", indent+"  ")
	} else {
		text = indent + "- " + yamlScalar(op.Item.Value, yamlKind(sample), sample.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle)) + "\n"
	}
	pos := d.ensureNewlineAt(last.end)
	d.src = splice(d.src, pos, pos, text)
	return Change{What: op.Path.String(), To: desc, Verb: "added"}, nil
}

func (d *yamlDoc) remove(op Op) (Change, error) {
	n, _, _, err := d.resolve(op.Path)
	if err != nil {
		return Change{}, err
	}
	if n.Kind != yaml.SequenceNode {
		return Change{}, fmt.Errorf("%s isn't a list", op.Path)
	}
	var idx []int
	for i, item := range n.Content {
		if itemMatches(item, op.Match) {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return Change{}, fmt.Errorf("nothing in %s matches %s", op.Path, describeMatch(op.Match))
	}
	desc := describeMatch(op.Match)
	// Remove from the end so earlier offsets stay valid.
	for k := len(idx) - 1; k >= 0; k-- {
		i := idx[k]
		if isFlow(n) {
			if d.src, err = flowRemove(d.src, d.offset(n), i); err != nil {
				return Change{}, err
			}
		} else {
			e, err := d.seqEntry(n.Content[i])
			if err != nil {
				return Change{}, err
			}
			d.src = splice(d.src, e.start, e.end, "")
		}
		if err := d.parse(); err != nil {
			return Change{}, err
		}
		if n, _, _, err = d.resolve(op.Path); err != nil {
			return Change{}, err
		}
	}
	if n.Kind == yaml.ScalarNode {
		// The block list lost its last item and now reads as null.
		if err := d.emptyAfterKey(op.Path, "[]"); err != nil {
			return Change{}, err
		}
	}
	return Change{What: op.Path.String(), From: desc, Verb: "removed"}, nil
}

// emptyAfterKey writes " []" or " {}" after "key:" once a block collection under it
// has lost its last entry (otherwise YAML would read it as null).
func (d *yamlDoc) emptyAfterKey(p Path, empty string) error {
	n, key, _, err := d.resolve(p)
	if err != nil || key == nil || n.Kind != yaml.ScalarNode || n.ShortTag() != "!!null" {
		return nil
	}
	pos := d.offset(key) + len(key.Value)
	for pos < len(d.src) && d.src[pos] != ':' {
		pos++
	}
	d.src = splice(d.src, pos+1, pos+1, " "+empty)
	return nil
}

func itemMatches(item *yaml.Node, match map[string]string) bool {
	if item.Kind == yaml.ScalarNode {
		v, ok := match["value"]
		return ok && len(match) == 1 && item.Value == v
	}
	if item.Kind == yaml.MappingNode {
		return matchesAll(yamlScalarFields(item), match)
	}
	return false
}

// ── put / delete (maps) ─────────────────────────────────────────────────────────

func (d *yamlDoc) put(op Op) (Change, error) {
	n, _, _, err := d.resolve(op.Path)
	if err != nil {
		return Change{}, err
	}
	if n.Kind != yaml.MappingNode {
		return Change{}, fmt.Errorf("%s isn't a map", op.Path)
	}
	var sample *yaml.Node
	for j := 0; j+1 < len(n.Content); j += 2 {
		if n.Content[j].Value == op.Name {
			return Change{}, fmt.Errorf("%s already has %q", op.Path, op.Name)
		}
		sample = n.Content[j+1]
	}
	what := op.Path.String()
	desc := op.Name + " = " + describeItem(op.Item)
	if isFlow(n) {
		d.src, err = flowInsert(d.src, d.offset(n), yamlKey(op.Name)+": "+yamlFlowItem(op.Item, sample))
		if err != nil {
			return Change{}, err
		}
		return Change{What: what, To: desc, Verb: "added"}, nil
	}
	lastKey := n.Content[len(n.Content)-2]
	last := d.entryAt(d.offset(lastKey))
	indent := strings.Repeat(" ", last.indent)
	var text string
	if op.Item.IsObject() {
		child := indent + "  "
		if sample != nil && sample.Kind == yaml.MappingNode && len(sample.Content) > 0 && !isFlow(sample) {
			child = strings.Repeat(" ", d.offset(sample.Content[0])-lineStartOf(d.src, d.offset(sample.Content[0])))
		}
		text = indent + yamlKey(op.Name) + ":\n" + yamlBlockFields(op.Item, sample, child, child)
	} else {
		like := KindString
		if sample != nil {
			like = yamlKind(sample)
		}
		text = indent + yamlKey(op.Name) + ": " + yamlScalar(op.Item.Value, like, 0) + "\n"
	}
	pos := d.ensureNewlineAt(last.end)
	d.src = splice(d.src, pos, pos, text)
	return Change{What: what, To: desc, Verb: "added"}, nil
}

func (d *yamlDoc) del(op Op) (Change, error) {
	n, _, _, err := d.resolve(op.Path)
	if err != nil {
		return Change{}, err
	}
	if n.Kind != yaml.MappingNode {
		return Change{}, fmt.Errorf("%s isn't a map", op.Path)
	}
	what := op.Path.String()
	for j := 0; j+1 < len(n.Content); j += 2 {
		if n.Content[j].Value != op.Name {
			continue
		}
		if isFlow(n) {
			d.src, err = flowRemove(d.src, d.offset(n), j/2)
			return Change{What: what, From: op.Name, Verb: "removed"}, err
		}
		e := d.entryAt(d.offset(n.Content[j]))
		d.src = splice(d.src, e.start, e.end, "")
		if len(n.Content) == 2 {
			if err := d.parse(); err != nil {
				return Change{}, err
			}
			return Change{What: what, From: op.Name, Verb: "removed"}, d.emptyAfterKey(op.Path, "{}")
		}
		return Change{What: what, From: op.Name, Verb: "removed"}, nil
	}
	return Change{}, fmt.Errorf("%s has no %q", op.Path, op.Name)
}

// ── summaries ───────────────────────────────────────────────────────────────────

func describeItem(it Item) string {
	if !it.IsObject() {
		return it.Value
	}
	keys := it.orderedKeys(nil)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + it.Fields[k]
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func describeMatch(m map[string]string) string {
	if v, ok := m["value"]; ok && len(m) == 1 {
		return v
	}
	return describeItem(Item{Fields: m})
}
