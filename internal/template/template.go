package template

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// The string being operated on is always the LAST argument, so these work in a
// pipeline: {{.name | replace "-" "_"}} passes .name last. Same convention as
// Sprig/Helm. Wrapping the strings package directly would put it first instead.
var funcMap = template.FuncMap{
	"split":      func(sep, s string) []string { return strings.Split(s, sep) },
	"trimPrefix": func(prefix, s string) string { return strings.TrimPrefix(s, prefix) },
	"trimSuffix": func(suffix, s string) string { return strings.TrimSuffix(s, suffix) },
	"trimSpace":  strings.TrimSpace,
	"toLower":    strings.ToLower,
	"toUpper":    strings.ToUpper,
	"contains":   func(substr, s string) bool { return strings.Contains(s, substr) },
	"replace":    func(old, new, s string) string { return strings.ReplaceAll(s, old, new) },
}

// Render executes a template string with the given values.
// Templates use standard Go text/template syntax: {{.name}}, {{.environment}}, etc.
// The template content is fetched remotely by the caller before being passed here.
// An optional maps argument injects named lookup tables under .maps, accessible
// via the built-in index function: {{index .maps.aws_account_ids .account}}
func Render(content string, values map[string]string, maps ...map[string]map[string]string) (string, error) {
	t, err := template.New("resource").Funcs(funcMap).Option("missingkey=zero").Parse(content)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, buildContext(values, maps)); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return buf.String(), nil
}

// RenderString executes a template string (e.g. a pr_title or when expression) with the given values.
// Returns the original string unchanged if parsing or execution fails.
// Accepts an optional maps argument — same semantics as Render.
func RenderString(s string, values map[string]string, maps ...map[string]map[string]string) string {
	t, err := template.New("str").Funcs(funcMap).Option("missingkey=zero").Parse(s)
	if err != nil {
		return s
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, buildContext(values, maps)); err != nil {
		return s
	}
	return buf.String()
}

// buildContext merges field values and optional named maps into a single template data map.
// Field values are available as {{.fieldName}}.
// Maps are available as {{index .maps.<mapName> <key>}}.
func buildContext(values map[string]string, maps []map[string]map[string]string) map[string]any {
	out := make(map[string]any, len(values)+1)
	for k, v := range values {
		out[k] = v
	}
	if len(maps) > 0 && len(maps[0]) > 0 {
		mapsOut := make(map[string]any, len(maps[0]))
		for name, m := range maps[0] {
			inner := make(map[string]any, len(m))
			for k, v := range m {
				inner[k] = v
			}
			mapsOut[name] = inner
		}
		out["maps"] = mapsOut
	}
	return out
}
