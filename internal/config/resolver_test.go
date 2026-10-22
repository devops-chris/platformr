package config

import "testing"

func TestResolveLoginHint(t *testing.T) {
	cases := []struct {
		name, org, repo, own, want string
	}{
		{"request's own wins", "org {{.account}}", "repo {{.account}}", "own {{.account}}", "own {{.account}}"},
		{"repo default next", "org {{.account}}", "repo {{.account}}", "", "repo {{.account}}"},
		{"org default last", "org {{.account}}", "", "", "org {{.account}}"},
		{"none set", "", "", "", ""},
	}
	for _, c := range cases {
		org := &OrgConfig{Defaults: ResourceDefaults{LoginHint: c.org}}
		repo := &RepoConfig{RepoName: "o/r", Defaults: ResourceDefaults{LoginHint: c.repo},
			Resources: []Resource{{Name: "vpc", LoginHint: c.own}}}
		Resolve(org, repo)
		if got := repo.Resources[0].LoginHint; got != c.want {
			t.Errorf("%s: LoginHint = %q, want %q", c.name, got, c.want)
		}
	}
}
