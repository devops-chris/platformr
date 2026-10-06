package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const rootTOML = `
[defaults]
target_path       = "cloud/{{.account}}/{{.resource}}/{{.name}}/"
template_dir_path = "platformr/templates/{{.resource}}"

  [[defaults.fields]]
  name    = "vertical"
  type    = "file_lookup"
  source  = "cloud/{{.account}}/account.hcl"
  pattern = 'vertical = "(.*)"'

[maps.sizes]
small = "t3.small"

[[resources]]
name = "vpc"
`

func loadRoot(t *testing.T) *RepoConfig {
	t.Helper()
	var rc RepoConfig
	if _, err := toml.Decode(rootTOML, &rc); err != nil {
		t.Fatal(err)
	}
	rc.RepoName = "acme/infra"
	return &rc
}

func TestAddRequestFileAddsRequestsAndMaps(t *testing.T) {
	rc := loadRoot(t)
	err := rc.AddRequestFile("platformr/requests/eks.toml", `
[maps.versions]
latest = "1.33"

[[resources]]
name = "eks"

  [[resources.fields]]
  name = "vertical"

[[resources]]
name = "eks-upgrade"
`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rc.Resources {
		names = append(names, r.Name)
	}
	if got := strings.Join(names, ","); got != "vpc,eks,eks-upgrade" {
		t.Errorf("requests = %s, want vpc,eks,eks-upgrade (root first, then file order)", got)
	}
	if rc.Maps["versions"]["latest"] != "1.33" || rc.Maps["sizes"]["small"] != "t3.small" {
		t.Errorf("maps not merged: %v", rc.Maps)
	}

	// Root defaults and the reusable field library apply to requests from files too.
	Resolve(&OrgConfig{}, rc)
	eks := rc.Resources[1]
	if eks.Resolved.TargetPath != "cloud/{{.account}}/eks/{{.name}}/" {
		t.Errorf("target_path = %q, root default not applied", eks.Resolved.TargetPath)
	}
	if eks.Resolved.TemplateDir != "platformr/templates/eks" {
		t.Errorf("template dir = %q", eks.Resolved.TemplateDir)
	}
	if eks.Fields[0].Type != "file_lookup" {
		t.Errorf("bare field %q not filled from [defaults.fields]", eks.Fields[0].Name)
	}
}

func TestAddRequestFileRejectsProblemsAndAddsNothing(t *testing.T) {
	cases := map[string]struct{ content, wantErr string }{
		"name already in root":   {"[[resources]]\nname = \"vpc\"\n", `request named "vpc" already exists`},
		"duplicate within file":  {"[[resources]]\nname = \"a\"\n[[resources]]\nname = \"a\"\n", `request named "a" already exists`},
		"map already in root":    {"[maps.sizes]\nx = \"y\"\n[[resources]]\nname = \"a\"\n", `map named "sizes" already exists`},
		"defaults in a file":     {"[defaults]\nbase_branch = \"main\"\n[[resources]]\nname = \"a\"\n", `"defaults" can only be set in platformr.toml`},
		"requests_dir in a file": {"requests_dir = \"x\"\n[[resources]]\nname = \"a\"\n", `"requests_dir" can only be set in platformr.toml`},
		"no requests":            {"[maps.x]\na = \"b\"\n", "no [[resources]] found"},
		"missing name":           {"[[resources]]\ndescription = \"x\"\n", "has no name"},
		"syntax error":           {"[[resources]]\nname = \"a\n", "line 2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rc := loadRoot(t)
			err := rc.AddRequestFile("platformr/requests/bad.toml", c.content)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
			}
			if !strings.Contains(err.Error(), "platformr/requests/bad.toml") {
				t.Errorf("error doesn't name the file: %v", err)
			}
			if len(rc.Resources) != 1 || len(rc.Maps) != 1 {
				t.Errorf("broken file still added something: %d requests, %d maps", len(rc.Resources), len(rc.Maps))
			}
		})
	}
}

func TestDuplicateAcrossTwoFiles(t *testing.T) {
	rc := loadRoot(t)
	if err := rc.AddRequestFile("platformr/requests/a.toml", "[[resources]]\nname = \"eks\"\n"); err != nil {
		t.Fatal(err)
	}
	err := rc.AddRequestFile("platformr/requests/b.toml", "[[resources]]\nname = \"eks\"\n")
	if err == nil || !strings.Contains(err.Error(), "b.toml") {
		t.Fatalf("want duplicate error naming b.toml, got %v", err)
	}
}

func TestRequestsDirPath(t *testing.T) {
	if got := (&RepoConfig{}).RequestsDirPath(); got != "platformr/requests" {
		t.Errorf("default = %q", got)
	}
	if got := (&RepoConfig{RequestsDir: "ops/requests"}).RequestsDirPath(); got != "ops/requests" {
		t.Errorf("custom = %q", got)
	}
}
