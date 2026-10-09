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
}
