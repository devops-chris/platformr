package cmd

import (
	"testing"

	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
)

func TestRequestSummaryAndLabel(t *testing.T) {
	body := "## account-location request\n\nOpened via `platformr`\n\n### Details\n\n" +
		"- **env_dir**: qa\n- **where**: qa/use1\n- **account**: my-account\n- **account_env**: \n"
	pr := ghclient.PRStatus{Repo: "Org/IaC", Title: "feat(my-account): add qa/use1", Body: body}

	res := config.Resource{Name: "account-location", DisplayName: "Add an environment or region to an account"}
	res.Resolved.Repo = "org/iac"
	withFields := res
	withFields.StatusFields = []string{"account", "where"}
	emptyFields := res
	emptyFields.StatusFields = []string{"account_env"}

	cases := []struct {
		name      string
		resources map[string]config.Resource
		summary   string
		label     string
	}{
		{"status_fields set", requestResources([]*config.RepoConfig{{Resources: []config.Resource{withFields}}}),
			"my-account · qa/use1", "Add an environment or region to an account"},
		{"no status_fields", requestResources([]*config.RepoConfig{{Resources: []config.Resource{res}}}),
			pr.Title, "Add an environment or region to an account"},
		{"fields with no answer", requestResources([]*config.RepoConfig{{Resources: []config.Resource{emptyFields}}}),
			pr.Title, "Add an environment or region to an account"},
		{"request since removed", map[string]config.Resource{},
			pr.Title, "account-location"},
	}
	for _, c := range cases {
		if got := requestSummary(pr, c.resources); got != c.summary {
			t.Errorf("%s: requestSummary = %q, want %q", c.name, got, c.summary)
		}
		if got := requestLabel(pr, c.resources); got != c.label {
			t.Errorf("%s: requestLabel = %q, want %q", c.name, got, c.label)
		}
	}
}
