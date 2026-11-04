package cmd

import (
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
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

// A request that puts the same files in every ticked account, run through the real
// rendering: one copy per account, each in its own folder, with {{.each}} in it.
func TestRenderTemplateDirForEach(t *testing.T) {
	var rc config.RepoConfig
	if _, err := toml.Decode(`
[[resources]]
name        = "dns-zone"
target_path = 'cloud/aws/'

  [[resources.fields]]
  name   = "accounts"
  type   = "multiselect"
  source = "dirs:cloud/aws"

  [[resources.fields]]
  name = "domain"
  type = "input"

  [[resources.template_files]]
  name        = "terragrunt.hcl"
  for_each    = "accounts"
  target_path = 'cloud/aws/{{.each}}/global/dns-zone/{{.domain}}/'

  [[resources.template_files]]
  name           = "instance.hcl"
  for_each       = "accounts"
  target_path    = 'cloud/aws/{{.each}}/global/dns-zone/{{.domain}}/'
  skip_if_exists = 'cloud/aws/{{.each}}/global/dns-zone/{{.domain}}/instance.hcl'
`, &rc); err != nil {
		t.Fatal(err)
	}
	rc.RepoName = "acme/infra"
	config.Resolve(&config.OrgConfig{}, &rc)
	r := rc.Resources[0]

	tmpl := []ghclient.TemplateFile{
		{Name: "terragrunt.hcl.tmpl", Content: "include \"root\" {}\n"},
		{Name: "instance.hcl.tmpl", Content: "domain = \"{{.domain}}\" # {{.each}}\n"},
	}
	values := map[string]string{"accounts": "pt-a-prod,pt-a-dev", "domain": "x.com"}
	// instance.hcl is already in pt-a-dev: skipped there only.
	exists := func(p string) bool { return p == "cloud/aws/pt-a-dev/global/dns-zone/x.com/instance.hcl" }

	files, skipped, err := renderTemplateDir(r, tmpl, values, nil, exists)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Content
	}
	want := map[string]string{
		"cloud/aws/pt-a-prod/global/dns-zone/x.com/terragrunt.hcl": "include \"root\" {}\n",
		"cloud/aws/pt-a-dev/global/dns-zone/x.com/terragrunt.hcl":  "include \"root\" {}\n",
		"cloud/aws/pt-a-prod/global/dns-zone/x.com/instance.hcl":   "domain = \"x.com\" # pt-a-prod\n",
	}
	if len(got) != len(want) {
		t.Fatalf("files: got %v, want %v", got, want)
	}
	for p, c := range want {
		if got[p] != c {
			t.Errorf("%s: got %q, want %q", p, got[p], c)
		}
	}
	if len(skipped) != 1 || skipped[0] != "instance.hcl" {
		t.Errorf("skipped: got %v, want [instance.hcl]", skipped)
	}
}
