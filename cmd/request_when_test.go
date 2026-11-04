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

func TestTemplateFileValues(t *testing.T) {
	r := config.Resource{TemplateFiles: []config.TemplateFileConfig{
		{Name: "terragrunt.hcl", ForEach: "accounts", TargetPath: "cloud/aws/{{.each}}/zone/"},
		{Name: "typo.hcl", ForEach: "acounts"},
	}}

	got, err := templateFileValues(r, "terragrunt.hcl", map[string]string{"accounts": "pt-a-prod, pt-a-dev,", "domain": "x.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0]["each"] != "pt-a-prod" || got[1]["each"] != "pt-a-dev" || got[1]["domain"] != "x.com" {
		t.Errorf("one set of answers per item, with each set: got %v", got)
	}

	if got, _ := templateFileValues(r, "terragrunt.hcl", map[string]string{"accounts": ""}); len(got) != 0 {
		t.Errorf("nothing ticked: want no files, got %v", got)
	}
	if got, _ := templateFileValues(r, "other.hcl", map[string]string{}); len(got) != 1 {
		t.Errorf("no for_each: want the file once, got %v", got)
	}
	if _, err := templateFileValues(r, "typo.hcl", map[string]string{"accounts": "a"}); err == nil {
		t.Error("for_each naming no question: want an error")
	}
}
