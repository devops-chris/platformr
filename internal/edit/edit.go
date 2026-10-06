// Package edit changes values inside existing files without disturbing the rest of
// the file: comments, ordering and formatting stay as they are, and only the text of
// the value being changed (or the item being added/removed) is touched.
//
// It understands a few data formats (YAML, JSON, HCL, key=value) and, for anything
// else, "line markers": a comment like `// platformr:node_count` on the line to change.
// Which one is used comes from the file extension, or an explicit format override.
package edit

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Format names a file format this package can edit.
type Format string

const (
	YAML   Format = "yaml"
	JSON   Format = "json"
	HCL    Format = "hcl"
	Env    Format = "env"    // KEY=value lines: .env, .properties, .ini (with [sections])
	Marker Format = "marker" // any text file; lines tagged with "platformr:<name>"
)

// MarkerPrefix starts a key that refers to a line marker instead of a path:
// key = "marker:node_count" finds the line containing "platformr:node_count".
const MarkerPrefix = "marker:"

// extensions maps file suffixes to formats. Longer suffixes are checked first, so
// "main.tf.json" is JSON, not HCL.
var extensions = map[string]Format{
	".tf.json":     JSON,
	".tfvars.json": JSON,
	".tf":          HCL,
	".tfvars":      HCL,
	".hcl":         HCL,
	".nomad":       HCL,
	".yaml":        YAML,
	".yml":         YAML,
	".json":        JSON,
	".env":         Env,
	".properties":  Env,
	".ini":         Env,
}

// DetectFormat picks the format for a file: the override if set, otherwise its
// extension. Unknown extensions are an error that says how to fix it.
func DetectFormat(file, override string) (Format, error) {
	if override != "" {
		f := Format(strings.ToLower(override))
		switch f {
		case YAML, JSON, HCL, Env, Marker:
			return f, nil
		}
		return "", fmt.Errorf("unknown format %q — use one of: yaml, json, hcl, env, marker", override)
	}
	base := strings.ToLower(path.Base(file))
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return Env, nil
	}
	suffixes := make([]string, 0, len(extensions))
	for s := range extensions {
		suffixes = append(suffixes, s)
	}
	sort.Slice(suffixes, func(i, j int) bool { return len(suffixes[i]) > len(suffixes[j]) })
	for _, s := range suffixes {
		if strings.HasSuffix(base, s) {
			return extensions[s], nil
		}
	}
	return "", fmt.Errorf("platformr doesn't know what format %s is — add format = \"yaml\" (or json, hcl, env, marker) to the request", file)
}

// Step is one step of a path into a file: a name (map key, attribute, or block type
// / label), a position in a list, or "the list item whose fields match".
type Step struct {
	Key   string
	Index int // used when IsIndex
	// Match picks the one list item (or repeated HCL block) whose fields equal these
	// values, e.g. {Sid = "S3Read"} in an IAM policy's Statement list.
	Match   map[string]string
	IsIndex bool
}

func (s Step) String() string {
	switch {
	case s.Match != nil:
		keys := make([]string, 0, len(s.Match))
		for k := range s.Match {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s=%q", k, s.Match[k])
		}
		return "[" + strings.Join(parts, ",") + "]"
	case s.IsIndex:
		return fmt.Sprintf("[%d]", s.Index)
	default:
		return s.Key
	}
}

// Path is a full path into a file.
type Path []Step

func (p Path) String() string {
	var b strings.Builder
	for i, s := range p {
		if i > 0 && s.Match == nil && !s.IsIndex {
			b.WriteByte('.')
		}
		b.WriteString(s.String())
	}
	return b.String()
}

// ParsePath turns a key from platformr.toml into a Path. A string is split on dots
// ("resources.requests.cpu"). A list keeps each element as one step, so keys that
// contain dots or colons work: ["config", "myproject:nodeCount"]. In a list, a number
// is a position and a table is a match: ["Statement", {Sid = "S3Read"}, "Action"].
// render is applied to every string (for {{.field}} values).
func ParsePath(key any, render func(string) string) (Path, error) {
	if render == nil {
		render = func(s string) string { return s }
	}
	switch k := key.(type) {
	case string:
		if strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("key is empty")
		}
		var p Path
		for _, part := range strings.Split(render(k), ".") {
			if part == "" {
				return nil, fmt.Errorf("key %q has an empty part (two dots in a row?)", k)
			}
			p = append(p, Step{Key: part})
		}
		return p, nil
	case []any:
		if len(k) == 0 {
			return nil, fmt.Errorf("key is an empty list")
		}
		var p Path
		for _, el := range k {
			switch e := el.(type) {
			case string:
				p = append(p, Step{Key: render(e)})
			case int64:
				p = append(p, Step{Index: int(e), IsIndex: true})
			case int:
				p = append(p, Step{Index: e, IsIndex: true})
			case map[string]any:
				m := map[string]string{}
				for mk, mv := range e {
					m[mk] = render(fmt.Sprint(mv))
				}
				p = append(p, Step{Match: m})
			default:
				return nil, fmt.Errorf("key element %v isn't a name, number or {field = value} match", el)
			}
		}
		return p, nil
	default:
		return nil, fmt.Errorf("key must be a string or a list, not %T", key)
	}
}

// Item is something added to a collection: either a single value, or an object with
// named fields. Fields are written in the same order as existing items in the
// collection where possible, otherwise alphabetically.
type Item struct {
	Value  string            // used when Fields is nil
	Fields map[string]string // an object
}

// IsObject reports whether the item has named fields.
func (it Item) IsObject() bool { return it.Fields != nil }

// orderedKeys returns the item's field names, following preferred (an existing
// item's order) first, then any remaining names alphabetically.
func (it Item) orderedKeys(preferred []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range preferred {
		if _, ok := it.Fields[k]; ok && !seen[k] {
			out = append(out, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range it.Fields {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// Kind is the type of a value as written in the file, so a changed value keeps it:
// a number stays a bare number, a quoted string stays quoted.
type Kind int

const (
	KindString Kind = iota
	KindNumber
	KindBool
)

// kindFor decides how to write v, given the kind of the value it replaces (or of
// its neighbors in a list). Numbers and booleans stay bare only if v still is one.
func kindFor(v string, like Kind) Kind {
	switch like {
	case KindNumber:
		if _, err := strconv.ParseFloat(v, 64); err == nil {
			return KindNumber
		}
	case KindBool:
		if v == "true" || v == "false" {
			return KindBool
		}
	}
	return KindString
}

// Op is one change to make in a file.
type Op struct {
	Action string // set, append, remove, put, delete
	Path   Path
	Value  string            // set
	Item   Item              // append, put
	Match  map[string]string // remove: fields that identify the item(s); for plain values, {"value": v}
	Name   string            // put, delete: the map key
}

// Change describes what an Op did, for the "old → new" summary.
type Change struct {
	What string // e.g. "replicaCount", "users"
	From string // old value, or "" for added items
	To   string // new value, or "" for removed items
	Verb string // "changed", "added", "removed"
}

func (c Change) String() string {
	switch c.Verb {
	case "added":
		return fmt.Sprintf("%s: + %s", c.What, c.To)
	case "removed":
		return fmt.Sprintf("%s: − %s", c.What, c.From)
	default:
		return fmt.Sprintf("%s: %s → %s", c.What, c.From, c.To)
	}
}

// Doc is a file opened for editing in one format.
type Doc interface {
	// Get returns the plain value at path (a string, number or boolean).
	Get(p Path) (string, error)
	// Items lists a collection for a picker: plain values, the `show` field of each
	// object in a list, or the names in a map.
	Items(p Path, show string) ([]string, error)
	// Apply makes one change and returns what it did. A "set" to the value already
	// there returns a zero Change and leaves the file alone.
	Apply(op Op) (Change, error)
	// Bytes returns the file's current content.
	Bytes() []byte
}

// Open parses content in the given format.
func Open(format Format, file string, content []byte) (Doc, error) {
	var (
		d   Doc
		err error
	)
	switch format {
	case YAML:
		d, err = openYAML(content)
	case JSON:
		d, err = openJSON(content)
	case HCL:
		d, err = openHCL(file, content)
	case Env:
		d, err = openEnv(content)
	case Marker:
		d, err = openMarker(content)
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
	if err != nil {
		return nil, fmt.Errorf("%s isn't valid %s: %w", file, format, err)
	}
	return d, nil
}

// errNotFound builds the standard "key isn't there" error.
func errNotFound(p Path, upTo int) error {
	return fmt.Errorf("%s not found", p[:upTo+1].String())
}

// ── shared text helpers ──────────────────────────────────────────────────────────

// lineStarts returns the byte offset of the start of every line.
func lineStarts(b []byte) []int {
	starts := []int{0}
	for i, c := range b {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// indentOf returns the leading whitespace of the line containing offset.
func indentOf(b []byte, offset int) string {
	start := offset
	for start > 0 && b[start-1] != '\n' {
		start--
	}
	end := start
	for end < len(b) && (b[end] == ' ' || b[end] == '\t') {
		end++
	}
	return string(b[start:end])
}

// splice replaces b[start:end] with s.
func splice(b []byte, start, end int, s string) []byte {
	out := make([]byte, 0, len(b)-(end-start)+len(s))
	out = append(out, b[:start]...)
	out = append(out, s...)
	return append(out, b[end:]...)
}

// lineStartOf returns the offset of the start of the line containing offset.
func lineStartOf(b []byte, offset int) int {
	for offset > 0 && b[offset-1] != '\n' {
		offset--
	}
	return offset
}

// lineEndOf returns the offset just past the newline ending the line containing
// offset (or len(b)).
func lineEndOf(b []byte, offset int) int {
	for offset < len(b) && b[offset] != '\n' {
		offset++
	}
	if offset < len(b) {
		offset++
	}
	return offset
}

// isBlankBetween reports whether b[from:to] is only whitespace.
func isBlankBetween(b []byte, from, to int) bool {
	for i := from; i < to && i < len(b); i++ {
		if b[i] != ' ' && b[i] != '\t' && b[i] != '\n' && b[i] != '\r' {
			return false
		}
	}
	return true
}

// matchesAll reports whether every match field equals the corresponding value.
func matchesAll(fields map[string]string, match map[string]string) bool {
	for k, v := range match {
		got, ok := fields[k]
		if !ok || got != v {
			return false
		}
	}
	return true
}
