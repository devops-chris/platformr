package cmd

import (
	"testing"

	"github.com/devops-chris/platformr/internal/config"
)

func TestTemplateFileWanted(t *testing.T) {
	r := config.Resource{TemplateFiles: []config.TemplateFileConfig{
		{Name: "datadog-values.yaml", When: `{{if contains "datadog" .addons}}true{{end}}`},
		{Name: "argocd-values.yaml", TargetPath: "argocd/"},
	}}
	cases := []struct {
		file, addons string
		want         bool
	}{
		{"datadog-values.yaml", "datadog,platform", true},
		{"datadog-values.yaml", "platform", false},
		{"argocd-values.yaml", "", true},  // entry without when
		{"not-configured.yaml", "", true}, // no entry at all
	}
	for _, c := range cases {
		if got := templateFileWanted(r, c.file, map[string]string{"addons": c.addons}, nil); got != c.want {
			t.Errorf("%s with addons=%q: got %v, want %v", c.file, c.addons, got, c.want)
		}
	}
}
