package remote

import (
	"errors"
	"strings"
	"testing"

	"github.com/devops-chris/platformr/internal/config"
)

func TestAllCategoriesOrgFirst(t *testing.T) {
	org := &config.OrgConfig{Categories: []config.CategoryConfig{
		{Name: "Network", Description: "VPCs"},
		{Name: "Apps", Description: "from the org config"},
		{Name: "Platform team"},
	}}
	repos := []*config.RepoConfig{
		{Categories: []config.CategoryConfig{{Name: "Apps", Description: "repo copy, ignored"}}},
		{Categories: []config.CategoryConfig{{Name: "Extras", Description: "repo-only"}}},
	}
	resources := []config.Resource{
		{Name: "a", Category: "Extras"}, {Name: "b", Category: "Platform team"},
		{Name: "c", Category: "Apps"}, {Name: "d", Category: "Network"},
	}
	var got []string
	for _, c := range config.MenuCategories(resources, AllCategories(org, repos)) {
		got = append(got, c.Name+"="+c.Description)
	}
	want := "Network=VPCs,Apps=from the org config,Platform team=,Extras=repo-only"
	if strings.Join(got, ",") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, ","), want)
	}
	if len(AllCategories(nil, nil)) != 0 {
		t.Error("no org config and no repos should give no categories")
	}
}

func TestFallbackRef(t *testing.T) {
	exists := func(b bool) func() (bool, error) { return func() (bool, error) { return b, nil } }
	cases := []struct {
		name, ref, configured string
		branchExists          func() (bool, error)
		want                  string
		wantOK                bool
	}{
		{"--ref branch missing here: back to the configured ref", "feat/x", "", exists(false), "", true},
		{"--ref branch missing here: back to a configured branch", "feat/x", "release", exists(false), "release", true},
		{"--ref branch exists but has no platformr.toml: keep it", "feat/x", "", exists(true), "", false},
		{"no override: nothing to fall back from", "", "", exists(false), "", false},
		{"override equals the configured ref", "release", "release", exists(false), "", false},
		{"branch check fails: don't guess", "feat/x", "", func() (bool, error) { return false, errors.New("rate limited") }, "", false},
	}
	for _, c := range cases {
		got, ok := fallbackRef(c.ref, c.configured, c.branchExists)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}
