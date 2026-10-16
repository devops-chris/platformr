package cmd

import (
	"strings"
	"testing"

	"github.com/devops-chris/platformr/internal/config"
)

func TestFileLookup(t *testing.T) {
	repo := map[string]string{
		"cloud/aws/acct-a/account.hcl":   "locals {\n  vertical = \"ortho\"\n  brand    = \"tops\"\n}\n",
		"cloud/aws/acct-b/account.hcl":   "locals {\n  vertical = \"shared\"\n}\n",
		"cloud/aws/acct-a/use1/env.hcl":  "locals {\n  environment = \"dev\"\n}\n",
		"cloud/aws/acct-b/env.hcl":       "locals {\n  environment = \"sdlc\"\n}\n",
		"cloud/aws/acct-c/dev/env.hcl":   "locals {\n  environment = \"dev\"\n}\n",
		"cloud/aws/acct-c/dev/use1/x.tf": "",
	}
	fetch := func(p string) (string, error) {
		if c, ok := repo[p]; ok {
			return c, nil
		}
		return "", errFileNotFound
	}
	brand := config.Field{Name: "brand", Pattern: `brand\s*=\s*"([^"]*)"`, Optional: true}
	env := config.Field{Name: "env", Pattern: `environment\s*=\s*"([^"]*)"`, SearchParents: true}

	cases := []struct {
		field config.Field
		file  string
		want  string
	}{
		{brand, "cloud/aws/acct-a/account.hcl", "tops"},
		{brand, "cloud/aws/acct-b/account.hcl", ""},                // optional: no brand → ""
		{env, "cloud/aws/acct-a/use1/env.hcl", "dev"},              // right where it says
		{env, "cloud/aws/acct-b/use1/env.hcl", "sdlc"},             // found at the account root
		{env, "cloud/aws/acct-c/dev/use1/vpc/main/env.hcl", "dev"}, // several levels up
	}
	for _, c := range cases {
		got, err := fileLookup(c.field, c.file, fetch)
		if err != nil || got != c.want {
			t.Errorf("%s in %s = %q, %v; want %q", c.field.Name, c.file, got, err, c.want)
		}
	}

	// Not optional: missing file and no match are errors that say what's missing.
	_, err := fileLookup(config.Field{Name: "env", Pattern: `environment = "(.*)"`, SearchParents: true}, "cloud/aws/nope/use1/env.hcl", fetch)
	if err == nil || !strings.Contains(err.Error(), "or any folder above it") {
		t.Errorf("missing with search_parents: %v", err)
	}
	_, err = fileLookup(config.Field{Name: "brand", Pattern: `brand = "(.*)"`}, "cloud/aws/acct-b/account.hcl", fetch)
	if err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Errorf("no match, not optional: %v", err)
	}

	// not_found: the request's own plain message instead, for a missing file or no match.
	msg := "This cluster isn't set up for platform projects yet. Ask the platform team."
	for _, file := range []string{"cloud/aws/nope/x.yaml", "cloud/aws/acct-b/account.hcl"} {
		_, err = fileLookup(config.Field{Name: "brand", Pattern: `brand = "(.*)"`, NotFound: msg}, file, fetch)
		if err == nil || err.Error() != msg {
			t.Errorf("not_found for %s: %v", file, err)
		}
	}
}

func TestSourceRepo(t *testing.T) {
	res := config.Resource{Resolved: config.ResolvedResource{TemplateRepo: "acme/k8s", TemplateRef: "feature"}}
	repos := []*config.RepoConfig{
		{RepoName: "acme/k8s", RepoRef: "feature"},
		{RepoName: "acme/terraform", RepoRef: ""},
	}
	cases := []struct{ repo, wantRepo, wantRef, wantErr string }{
		{"", "acme/k8s", "feature", ""},              // the request's own repo
		{"terraform", "acme/terraform", "", ""},      // short name
		{"acme/terraform", "acme/terraform", "", ""}, // full name
		{"other", "", "", "isn't connected to platformr"},
		{"form", "", "", "isn't connected to platformr"}, // no partial-name matches
	}
	for _, c := range cases {
		repo, ref, err := sourceRepo(config.Field{Name: "x", Repo: c.repo}, res, repos)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("repo %q: err = %v, want %q", c.repo, err, c.wantErr)
			}
			continue
		}
		if err != nil || repo != c.wantRepo || ref != c.wantRef {
			t.Errorf("repo %q: got %s@%s, %v; want %s@%s", c.repo, repo, ref, err, c.wantRepo, c.wantRef)
		}
	}
}
