package edit

import (
	"fmt"
	"regexp"
	"strings"
)

// ── key=value files: .env, .properties, .ini ───────────────────────────────────

// envDoc edits KEY=value lines. Keys may sit under [section] headers (INI), reached
// as "section.key". `export KEY=value` and `key: value` (properties) also work.
// There are no lists or maps in these files, so only "set" is supported.
type envDoc struct{ src []byte }

func openEnv(content []byte) (*envDoc, error) {
	return &envDoc{src: append([]byte(nil), content...)}, nil
}

func (d *envDoc) Bytes() []byte { return d.src }

var envLine = regexp.MustCompile(`^(\s*(?:export\s+)?)([^=:#;\s\[][^=:]*?)(\s*[=:]\s*)(.*?)(\s*)$`)

// find returns the byte span of the value for key (with surrounding quotes, if any).
func (d *envDoc) find(p Path) (int, int, string, error) {
	if len(p) == 0 || len(p) > 2 {
		return 0, 0, "", fmt.Errorf("%s: key=value files only have keys, or [section].key", p)
	}
	section, key := "", p[len(p)-1].Key
	if len(p) == 2 {
		section = p[0].Key
	}
	current := ""
	off := 0
	for _, line := range strings.SplitAfter(string(d.src), "\n") {
		body := strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(body)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			current = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		} else if !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, ";") {
			if m := envLine.FindStringSubmatchIndex(body); m != nil && current == section && body[m[4]:m[5]] == key {
				start, end := off+m[8], off+m[9]
				return start, end, string(d.src[start:end]), nil
			}
		}
		off += len(line)
	}
	return 0, 0, "", fmt.Errorf("%s not found", p)
}

func unquote(v string) (string, byte) {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1], v[0]
	}
	return v, 0
}

func (d *envDoc) Get(p Path) (string, error) {
	_, _, raw, err := d.find(p)
	v, _ := unquote(raw)
	return v, err
}

func (d *envDoc) Items(p Path, _ string) ([]string, error) {
	return nil, fmt.Errorf("key=value files don't have lists — %s can't be listed", p)
}

func (d *envDoc) Apply(op Op) (Change, error) {
	if op.Action != "set" {
		return Change{}, fmt.Errorf("key=value files only support set, not %s", op.Action)
	}
	start, end, raw, err := d.find(op.Path)
	if err != nil {
		return Change{}, err
	}
	old, q := unquote(raw)
	if old == op.Value {
		return Change{}, nil
	}
	nv := op.Value
	if q != 0 {
		nv = string(q) + strings.ReplaceAll(op.Value, string(q), `\`+string(q)) + string(q)
	} else if strings.ContainsAny(op.Value, " #\"'") {
		nv = `"` + strings.ReplaceAll(op.Value, `"`, `\"`) + `"`
	}
	d.src = splice(d.src, start, end, nv)
	return Change{What: op.Path.String(), From: old, To: op.Value, Verb: "changed"}, nil
}

// ── line markers: any text file ─────────────────────────────────────────────────

// markerDoc edits the value on a line tagged with a comment like
// `// platformr:node_count` or `# platformr:sku`. "The value" is the last string,
// number or true/false before the marker's comment. Works in any language with
// comments, which is what makes platformr usable with formats it doesn't parse
// (Bicep, Jsonnet, CUE, Pulumi/CDK code, ...).
type markerDoc struct{ src []byte }

func openMarker(content []byte) (*markerDoc, error) {
	return &markerDoc{src: append([]byte(nil), content...)}, nil
}

func (d *markerDoc) Bytes() []byte { return d.src }

var markerValue = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\btrue\b|\bfalse\b|-?\b\d+(?:\.\d+)?\b`)

func markerName(p Path) (string, error) {
	if len(p) != 1 || !strings.HasPrefix(p[0].Key, MarkerPrefix) {
		return "", fmt.Errorf("line-marker keys look like \"marker:<name>\", not %s", p)
	}
	return strings.TrimPrefix(p[0].Key, MarkerPrefix), nil
}

func (d *markerDoc) find(p Path) (int, int, string, error) {
	name, err := markerName(p)
	if err != nil {
		return 0, 0, "", err
	}
	tag := "platformr:" + name
	off := 0
	found := -1
	var start, end int
	for _, line := range strings.SplitAfter(string(d.src), "\n") {
		if i := strings.Index(line, tag); i >= 0 {
			after := line[i+len(tag):]
			if after != "" && (after[0] == '_' || after[0] == '-' || isAlnum(after[0])) {
				off += len(line)
				continue // platformr:node_count shouldn't match platformr:node_count_max
			}
			if found >= 0 {
				return 0, 0, "", fmt.Errorf("the marker %s appears on more than one line — each marker must be unique", tag)
			}
			before := line[:i]
			// Cut off the comment that holds the marker.
			for _, c := range []string{"//", "#", "--", ";", "/*"} {
				if j := strings.LastIndex(before, c); j >= 0 {
					before = before[:j]
					break
				}
			}
			locs := markerValue.FindAllStringIndex(before, -1)
			if len(locs) == 0 {
				return 0, 0, "", fmt.Errorf("the line with %s has no string, number or true/false before the marker", tag)
			}
			last := locs[len(locs)-1]
			found, start, end = off, off+last[0], off+last[1]
		}
		off += len(line)
	}
	if found < 0 {
		return 0, 0, "", fmt.Errorf("no line has the marker %s", tag)
	}
	return start, end, string(d.src[start:end]), nil
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (d *markerDoc) Get(p Path) (string, error) {
	_, _, raw, err := d.find(p)
	v, _ := unquote(raw)
	return v, err
}

func (d *markerDoc) Items(p Path, _ string) ([]string, error) {
	return nil, fmt.Errorf("line markers mark a single value — %s can't be listed", p)
}

func (d *markerDoc) Apply(op Op) (Change, error) {
	if op.Action != "set" {
		return Change{}, fmt.Errorf("line markers only support set, not %s", op.Action)
	}
	start, end, raw, err := d.find(op.Path)
	if err != nil {
		return Change{}, err
	}
	old, q := unquote(raw)
	if old == op.Value {
		return Change{}, nil
	}
	nv := op.Value
	if q != 0 {
		nv = string(q) + strings.ReplaceAll(op.Value, string(q), `\`+string(q)) + string(q)
	} else {
		like := KindNumber
		if raw == "true" || raw == "false" {
			like = KindBool
		}
		if kindFor(op.Value, like) == KindString {
			nv = `"` + strings.ReplaceAll(op.Value, `"`, `\"`) + `"`
		}
	}
	d.src = splice(d.src, start, end, nv)
	return Change{What: op.Path.String(), From: old, To: op.Value, Verb: "changed"}, nil
}
