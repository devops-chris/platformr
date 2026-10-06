package edit

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// hclDoc edits HCL (Terraform, OpenTofu, Terragrunt, Packer, ...) by locating values
// with HashiCorp's own parser, which reports exact byte positions, and splicing text
// there. If the file was already `terraform fmt`-formatted, it's re-formatted after
// each change so alignment stays tidy; otherwise formatting is left alone.
//
// HCL values can be expressions. platformr only edits plain values: strings, numbers,
// booleans, and [...] / {...} written out literally, optionally wrapped in one of
// toset / tolist / tomap / jsonencode. Anything computed (var.x, merge(...), format(...))
// gets a clear "can't change that" error instead of a guess.
type hclDoc struct {
	src       []byte
	file      string
	body      *hclsyntax.Body
	formatted bool
}

var hclWrappers = map[string]bool{"toset": true, "tolist": true, "tomap": true, "jsonencode": true}

func openHCL(file string, content []byte) (*hclDoc, error) {
	d := &hclDoc{src: append([]byte(nil), content...), file: file}
	if err := d.parse(); err != nil {
		return nil, err
	}
	d.formatted = bytes.Equal(hclwrite.Format(d.src), d.src)
	return d, nil
}

func (d *hclDoc) parse() error {
	f, diags := hclsyntax.ParseConfig(d.src, d.file, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return diags
	}
	d.body = f.Body.(*hclsyntax.Body)
	return nil
}

func (d *hclDoc) Bytes() []byte { return d.src }

// ── values ──────────────────────────────────────────────────────────────────────

// unwrap removes toset(...)-style wrappers and parentheses around a value.
func unwrap(e hclsyntax.Expression) hclsyntax.Expression {
	for {
		switch x := e.(type) {
		case *hclsyntax.FunctionCallExpr:
			if hclWrappers[x.Name] && len(x.Args) == 1 {
				e = x.Args[0]
				continue
			}
		case *hclsyntax.ParenthesesExpr:
			e = x.Expression
			continue
		}
		return e
	}
}

// literal returns the value of a plain string, number or boolean expression.
func literal(e hclsyntax.Expression) (string, Kind, bool) {
	switch x := e.(type) {
	case *hclsyntax.LiteralValueExpr:
		switch x.Val.Type() {
		case cty.Number:
			return x.Val.AsBigFloat().Text('f', -1), KindNumber, true
		case cty.Bool:
			if x.Val.True() {
				return "true", KindBool, true
			}
			return "false", KindBool, true
		case cty.String:
			return x.Val.AsString(), KindString, true
		}
	case *hclsyntax.TemplateExpr:
		var b strings.Builder
		for _, part := range x.Parts {
			lit, ok := part.(*hclsyntax.LiteralValueExpr)
			if !ok || lit.Val.Type() != cty.String {
				return "", 0, false
			}
			b.WriteString(lit.Val.AsString())
		}
		return b.String(), KindString, true
	}
	return "", 0, false
}

// objectKey returns the name of an object item's key: `name = ...` or `"name" = ...`.
func objectKey(e hclsyntax.Expression) (string, bool) {
	if k, ok := e.(*hclsyntax.ObjectConsKeyExpr); ok {
		if t, ok := k.Wrapped.(*hclsyntax.ScopeTraversalExpr); ok && len(t.Traversal) == 1 {
			return t.Traversal.RootName(), true
		}
		s, _, ok := literal(k.Wrapped)
		return s, ok
	}
	s, _, ok := literal(e)
	return s, ok
}

func objectFields(o *hclsyntax.ObjectConsExpr) (map[string]string, []string, map[string]Kind) {
	fields := map[string]string{}
	kinds := map[string]Kind{}
	var order []string
	for _, it := range o.Items {
		k, ok := objectKey(it.KeyExpr)
		if !ok {
			continue
		}
		order = append(order, k)
		if v, kind, ok := literal(it.ValueExpr); ok {
			fields[k] = v
			kinds[k] = kind
		}
	}
	return fields, order, kinds
}

func blockFields(b *hclsyntax.Block) map[string]string {
	out := map[string]string{}
	for name, a := range b.Body.Attributes {
		if v, _, ok := literal(a.Expr); ok {
			out[name] = v
		}
	}
	return out
}

func notPlain(p Path) error {
	return fmt.Errorf("%s isn't written as a plain value in the file (it's computed, e.g. var.x, merge(...) or format(...)) — platformr can't change it", p)
}

// ── path resolution ─────────────────────────────────────────────────────────────

// resolve walks p through blocks (by type, then labels, then a match or position for
// repeated blocks), attributes, and object/list values. It returns the expression at
// the end of the path.
func (d *hclDoc) resolve(p Path) (hclsyntax.Expression, error) {
	body := d.body
	var expr hclsyntax.Expression
	for i := 0; i < len(p); i++ {
		s := p[i]
		if body != nil {
			if s.Match != nil || s.IsIndex {
				return nil, errNotFound(p, i)
			}
			if a, ok := body.Attributes[s.Key]; ok {
				expr, body = a.Expr, nil
				continue
			}
			var blocks []*hclsyntax.Block
			for _, b := range body.Blocks {
				if b.Type == s.Key {
					blocks = append(blocks, b)
				}
			}
			if len(blocks) == 0 {
				return nil, errNotFound(p, i)
			}
			// Consume labels: module "eks" { } is reached as module.eks.
			for len(blocks) > 0 && len(blocks[0].Labels) > 0 {
				depth := len(blocks[0].Labels)
				var matched []*hclsyntax.Block
				if i+depth >= len(p) {
					return nil, fmt.Errorf("%s blocks have labels — add the label(s) to the key, e.g. %s.<name>", s.Key, p[:i+1])
				}
				for _, b := range blocks {
					ok := len(b.Labels) == depth
					for l := 0; ok && l < depth; l++ {
						ok = p[i+1+l].Key == b.Labels[l]
					}
					if ok {
						matched = append(matched, b)
					}
				}
				if len(matched) == 0 {
					return nil, errNotFound(p, i+depth)
				}
				blocks, i = matched, i+depth
				break
			}
			if len(blocks) > 1 {
				// Repeated blocks, e.g. several `statement { }`: the next step picks one.
				if i+1 >= len(p) {
					return nil, fmt.Errorf("there are %d %s blocks — add a match like {sid = \"...\"} to pick one", len(blocks), s.Key)
				}
				next := p[i+1]
				switch {
				case next.Match != nil:
					var found []*hclsyntax.Block
					for _, b := range blocks {
						if matchesAll(blockFields(b), next.Match) {
							found = append(found, b)
						}
					}
					if len(found) != 1 {
						return nil, fmt.Errorf("%s matched %d %s blocks — it needs to match exactly one", next, len(found), s.Key)
					}
					blocks = found
				case next.IsIndex && next.Index >= 0 && next.Index < len(blocks):
					blocks = blocks[next.Index : next.Index+1]
				default:
					return nil, fmt.Errorf("there are %d %s blocks — add a match like {sid = \"...\"} to pick one", len(blocks), s.Key)
				}
				i++
			}
			body = blocks[0].Body
			continue
		}

		e := unwrap(expr)
		switch {
		case s.Match != nil:
			t, ok := e.(*hclsyntax.TupleConsExpr)
			if !ok {
				return nil, fmt.Errorf("%s isn't a list, so it can't be matched with %s", p[:i], s)
			}
			var found []hclsyntax.Expression
			for _, item := range t.Exprs {
				if o, ok := unwrap(item).(*hclsyntax.ObjectConsExpr); ok {
					if f, _, _ := objectFields(o); matchesAll(f, s.Match) {
						found = append(found, item)
					}
				}
			}
			if len(found) != 1 {
				return nil, fmt.Errorf("%s matched %d items in %s — it needs to match exactly one", s, len(found), p[:i])
			}
			expr = found[0]
		case s.IsIndex:
			t, ok := e.(*hclsyntax.TupleConsExpr)
			if !ok || s.Index < 0 || s.Index >= len(t.Exprs) {
				return nil, errNotFound(p, i)
			}
			expr = t.Exprs[s.Index]
		default:
			o, ok := e.(*hclsyntax.ObjectConsExpr)
			if !ok {
				if _, _, isLit := literal(e); !isLit {
					return nil, notPlain(p[:i])
				}
				return nil, errNotFound(p, i)
			}
			var next hclsyntax.Expression
			for _, it := range o.Items {
				if k, ok := objectKey(it.KeyExpr); ok && k == s.Key {
					next = it.ValueExpr
					break
				}
			}
			if next == nil {
				return nil, errNotFound(p, i)
			}
			expr = next
		}
	}
	if expr == nil {
		return nil, fmt.Errorf("%s is a block, not a value — add the attribute name to the key", p)
	}
	return expr, nil
}

func (d *hclDoc) Get(p Path) (string, error) {
	e, err := d.resolve(p)
	if err != nil {
		return "", err
	}
	v, _, ok := literal(e)
	if !ok {
		switch unwrap(e).(type) {
		case *hclsyntax.TupleConsExpr, *hclsyntax.ObjectConsExpr:
			return "", fmt.Errorf("%s is a list or map, not a single value", p)
		}
		return "", notPlain(p)
	}
	return v, nil
}

func (d *hclDoc) Items(p Path, show string) ([]string, error) {
	e, err := d.resolve(p)
	if err != nil {
		return nil, err
	}
	var out []string
	switch x := unwrap(e).(type) {
	case *hclsyntax.TupleConsExpr:
		for _, item := range x.Exprs {
			if v, _, ok := literal(item); ok {
				out = append(out, v)
				continue
			}
			if o, ok := unwrap(item).(*hclsyntax.ObjectConsExpr); ok {
				if show == "" {
					return nil, fmt.Errorf("items in %s have several fields — set show = \"<field>\" to pick which one to list", p)
				}
				if f, _, _ := objectFields(o); f[show] != "" {
					out = append(out, f[show])
				}
			}
		}
	case *hclsyntax.ObjectConsExpr:
		for _, it := range x.Items {
			if k, ok := objectKey(it.KeyExpr); ok {
				out = append(out, k)
			}
		}
	default:
		return nil, notPlain(p)
	}
	return out, nil
}

// ── rendering ───────────────────────────────────────────────────────────────────

var hclEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "${", "$${", "%{", "%%{")

func hclScalar(v string, like Kind) string {
	if k := kindFor(v, like); k != KindString {
		return v
	}
	return `"` + hclEscaper.Replace(v) + `"`
}

func hclName(k string) string {
	if hclsyntax.ValidIdentifier(k) {
		return k
	}
	return `"` + hclEscaper.Replace(k) + `"`
}

// hclItem renders an item; objects go across lines when multi is set.
func hclItem(it Item, sample hclsyntax.Expression, indent string, multi bool) string {
	if !it.IsObject() {
		like := KindString
		if sample != nil {
			if _, k, ok := literal(sample); ok {
				like = k
			}
		}
		return hclScalar(it.Value, like)
	}
	var order []string
	kinds := map[string]Kind{}
	if sample != nil {
		if o, ok := unwrap(sample).(*hclsyntax.ObjectConsExpr); ok {
			_, order, kinds = objectFields(o)
		}
	}
	keys := it.orderedKeys(order)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = hclName(k) + " = " + hclScalar(it.Fields[k], kinds[k])
	}
	if !multi {
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	return "{\n" + indent + "  " + strings.Join(parts, "\n"+indent+"  ") + "\n" + indent + "}"
}

func (d *hclDoc) start(e hclsyntax.Expression) int { return e.Range().Start.Byte }
func (d *hclDoc) end(e hclsyntax.Expression) int   { return e.Range().End.Byte }

func (d *hclDoc) spansLines(from, to int) bool {
	return bytes.Contains(d.src[from:to], []byte("\n"))
}

// afterComma returns the offset just past a comma that directly follows pos
// (ignoring spaces), or -1 if there isn't one.
func (d *hclDoc) afterComma(pos int) int {
	for pos < len(d.src) && (d.src[pos] == ' ' || d.src[pos] == '\t') {
		pos++
	}
	if pos < len(d.src) && d.src[pos] == ',' {
		return pos + 1
	}
	return -1
}

// insertLast adds text after the last element (ending at lastEnd) of a list or object
// whose brackets span [open, close]. Multi-line collections get the text on its own
// line, matching the existing trailing-comma style.
func (d *hclDoc) insertLast(open, close, lastEnd int, text string, isList bool) {
	if !d.spansLines(lastEnd, close) {
		sep := ", "
		d.src = splice(d.src, lastEnd, lastEnd, sep+text)
		return
	}
	indent := indentOf(d.src, lastEnd)
	if c := d.afterComma(lastEnd); c >= 0 {
		d.src = splice(d.src, c, c, "\n"+indent+text+",")
		return
	}
	if isList {
		d.src = splice(d.src, lastEnd, lastEnd, ",\n"+indent+text)
		return
	}
	d.src = splice(d.src, lastEnd, lastEnd, "\n"+indent+text)
}

// insertEmpty fills an empty [] or {} spanning [open, close].
func (d *hclDoc) insertEmpty(open, close int, text string, multi bool) {
	if !multi {
		if strings.HasPrefix(text, "{") || d.src[open] == '[' {
			d.src = splice(d.src, open+1, close, text)
		} else {
			d.src = splice(d.src, open+1, close, " "+text+" ")
		}
		return
	}
	indent := indentOf(d.src, open)
	d.src = splice(d.src, open+1, close, "\n"+indent+"  "+text+"\n"+indent)
}

// removeSpan deletes one element [from, to) of a collection, with its comma, taking
// whole lines when the element sits on its own lines.
func (d *hclDoc) removeSpan(from, to, prevEnd, nextStart int) {
	ls := lineStartOf(d.src, from)
	if isBlankBetween(d.src, ls, from) {
		end := to
		if c := d.afterComma(end); c >= 0 {
			end = c
		}
		le := lineEndOf(d.src, end)
		if isBlankBetween(d.src, end, le) {
			d.src = splice(d.src, ls, le, "")
			return
		}
	}
	switch {
	case nextStart >= 0:
		d.src = splice(d.src, from, nextStart, "")
	case prevEnd >= 0:
		d.src = splice(d.src, prevEnd, to, "")
	default:
		d.src = splice(d.src, from, to, "")
	}
}

// ── Apply ───────────────────────────────────────────────────────────────────────

func (d *hclDoc) Apply(op Op) (Change, error) {
	ch, err := d.apply(op)
	if err != nil {
		return Change{}, err
	}
	if d.formatted {
		d.src = hclwrite.Format(d.src)
	}
	if perr := d.parse(); perr != nil {
		return Change{}, fmt.Errorf("the change to %s produced invalid HCL (please report this): %w", op.Path, perr)
	}
	return ch, nil
}

func (d *hclDoc) apply(op Op) (Change, error) {
	e, err := d.resolve(op.Path)
	if err != nil {
		return Change{}, err
	}
	what := op.Path.String()
	switch op.Action {
	case "set":
		old, kind, ok := literal(e)
		if !ok {
			return Change{}, notPlain(op.Path)
		}
		if old == op.Value {
			return Change{}, nil
		}
		d.src = splice(d.src, d.start(e), d.end(e), hclScalar(op.Value, kind))
		return Change{What: what, From: old, To: op.Value, Verb: "changed"}, nil

	case "append":
		t, ok := unwrap(e).(*hclsyntax.TupleConsExpr)
		if !ok {
			return Change{}, notListOrPlain(e, op.Path, "list")
		}
		open, close := d.start(t), d.end(t)-1
		if len(t.Exprs) == 0 {
			d.insertEmpty(open, close, hclItem(op.Item, nil, indentOf(d.src, open)+"  ", false), false)
		} else {
			last := t.Exprs[len(t.Exprs)-1]
			multi := d.spansLines(d.start(last), d.end(last))
			d.insertLast(open, close, d.end(last), hclItem(op.Item, last, indentOf(d.src, d.start(last)), multi), true)
		}
		return Change{What: what, To: describeItem(op.Item), Verb: "added"}, nil

	case "remove":
		t, ok := unwrap(e).(*hclsyntax.TupleConsExpr)
		if !ok {
			return Change{}, notListOrPlain(e, op.Path, "list")
		}
		var idx []int
		for i, item := range t.Exprs {
			if hclItemMatches(item, op.Match) {
				idx = append(idx, i)
			}
		}
		if len(idx) == 0 {
			return Change{}, fmt.Errorf("nothing in %s matches %s", op.Path, describeMatch(op.Match))
		}
		for k := len(idx) - 1; k >= 0; k-- {
			i := idx[k]
			prevEnd, nextStart := -1, -1
			if i > 0 {
				prevEnd = d.end(t.Exprs[i-1])
			}
			if i < len(t.Exprs)-1 {
				nextStart = d.start(t.Exprs[i+1])
			}
			d.removeSpan(d.start(t.Exprs[i]), d.end(t.Exprs[i]), prevEnd, nextStart)
			if err := d.parse(); err != nil {
				return Change{}, err
			}
			if e, err = d.resolve(op.Path); err != nil {
				return Change{}, err
			}
			t = unwrap(e).(*hclsyntax.TupleConsExpr)
		}
		return Change{What: what, From: describeMatch(op.Match), Verb: "removed"}, nil

	case "put":
		o, ok := unwrap(e).(*hclsyntax.ObjectConsExpr)
		if !ok {
			return Change{}, notListOrPlain(e, op.Path, "map")
		}
		var sample hclsyntax.Expression
		for _, it := range o.Items {
			if k, _ := objectKey(it.KeyExpr); k == op.Name {
				return Change{}, fmt.Errorf("%s already has %q", op.Path, op.Name)
			}
			sample = it.ValueExpr
		}
		open, close := d.start(o), d.end(o)-1
		if len(o.Items) == 0 {
			multi := op.Item.IsObject()
			indent := indentOf(d.src, open) + "  "
			d.insertEmpty(open, close, hclName(op.Name)+" = "+hclItem(op.Item, nil, indent, multi), multi)
		} else {
			lastItem := o.Items[len(o.Items)-1]
			multi := sample != nil && d.spansLines(d.start(sample), d.end(sample))
			indent := indentOf(d.src, d.start(lastItem.KeyExpr))
			if !d.spansLines(d.start(o), d.end(o)) {
				multi = false
			}
			d.insertLast(open, close, d.end(lastItem.ValueExpr), hclName(op.Name)+" = "+hclItem(op.Item, sample, indent, multi), false)
		}
		return Change{What: what, To: op.Name + " = " + describeItem(op.Item), Verb: "added"}, nil

	case "delete":
		o, ok := unwrap(e).(*hclsyntax.ObjectConsExpr)
		if !ok {
			return Change{}, notListOrPlain(e, op.Path, "map")
		}
		for i, it := range o.Items {
			if k, _ := objectKey(it.KeyExpr); k != op.Name {
				continue
			}
			prevEnd, nextStart := -1, -1
			if i > 0 {
				prevEnd = d.end(o.Items[i-1].ValueExpr)
			}
			if i < len(o.Items)-1 {
				nextStart = d.start(o.Items[i+1].KeyExpr)
			}
			d.removeSpan(d.start(it.KeyExpr), d.end(it.ValueExpr), prevEnd, nextStart)
			return Change{What: what, From: op.Name, Verb: "removed"}, nil
		}
		return Change{}, fmt.Errorf("%s has no %q", op.Path, op.Name)
	}
	return Change{}, fmt.Errorf("unknown action %q", op.Action)
}

func notListOrPlain(e hclsyntax.Expression, p Path, want string) error {
	if _, _, ok := literal(e); ok {
		return fmt.Errorf("%s is a single value, not a %s", p, want)
	}
	switch unwrap(e).(type) {
	case *hclsyntax.TupleConsExpr:
		return fmt.Errorf("%s is a list, not a %s", p, want)
	case *hclsyntax.ObjectConsExpr:
		return fmt.Errorf("%s is a map, not a %s", p, want)
	}
	return notPlain(p)
}

func hclItemMatches(item hclsyntax.Expression, match map[string]string) bool {
	if v, _, ok := literal(item); ok {
		want, has := match["value"]
		return has && len(match) == 1 && v == want
	}
	if o, ok := unwrap(item).(*hclsyntax.ObjectConsExpr); ok {
		f, _, _ := objectFields(o)
		return matchesAll(f, match)
	}
	return false
}
