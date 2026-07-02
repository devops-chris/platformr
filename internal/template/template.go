package template

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

var funcMap = template.FuncMap{
	"split":      strings.Split,
	"trimPrefix": strings.TrimPrefix,
	"trimSuffix": strings.TrimSuffix,
	"trimSpace":  strings.TrimSpace,
	"toLower":    strings.ToLower,
	"toUpper":    strings.ToUpper,
	"contains":   strings.Contains,
	"replace":    strings.ReplaceAll,
}

// Render executes a template string with the given values.
// Templates use standard Go text/template syntax: {{.name}}, {{.environment}}, etc.
// The template content is fetched remotely by the caller before being passed here.
func Render(content string, values map[string]string) (string, error) {
	t, err := template.New("resource").Funcs(funcMap).Option("missingkey=zero").Parse(content)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, toAny(values)); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return buf.String(), nil
}

// RenderString executes a template string (e.g. a pr_title) with the given values.
// Returns the original string unchanged if parsing or execution fails.
func RenderString(s string, values map[string]string) string {
	t, err := template.New("str").Parse(s)
	if err != nil {
		return s
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, toAny(values)); err != nil {
		return s
	}
	return buf.String()
}

func toAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
