package remote

import (
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
