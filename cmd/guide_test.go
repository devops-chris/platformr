package cmd

import (
	"strings"
	"testing"

	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
)

func guideRepos() []*config.RepoConfig {
	tf := &config.RepoConfig{RepoName: "org/tf", Resources: []config.Resource{
		{Name: "vpc", Resolved: config.ResolvedResource{Repo: "org/tf", BaseBranch: "main"}},
		{Name: "eks", DisplayName: "New EKS cluster", Resolved: config.ResolvedResource{Repo: "org/tf", BaseBranch: "main"}},
		{Name: "rds", Resolved: config.ResolvedResource{Repo: "org/tf", BaseBranch: "main"}},
		{Name: "environment", Steps: []config.StepConfig{{Title: "x", Request: "vpc"}}},
	}}
	k8s := &config.RepoConfig{RepoName: "org/k8s", Resources: []config.Resource{
		{Name: "cluster-addons", Resolved: config.ResolvedResource{Repo: "org/k8s", BaseBranch: "main"}},
	}}
	return []*config.RepoConfig{tf, k8s}
}

var envGuide = config.Resource{
	Name: "environment",
	Fields: []config.Field{
		{Name: "account", Type: "select"},
		{Name: "region", Type: "select"},
		{Name: "where", Type: "computed"},
	},
	Steps: []config.StepConfig{
		{Title: "Network", Request: "vpc", DoneWhen: "dirs:cloud/aws/{{.account}}/{{.where}}/vpc"},
		{Title: "Cluster", Request: "eks", DoneWhen: "dirs:cloud/aws/{{.account}}/{{.where}}/eks"},
		{Title: "Database", Request: "rds", Optional: true, Hint: "only if the team needs one"},
		{Title: "Cluster apps", Request: "cluster-addons", DoneWhen: "dirs:cloud/aws/{{.account}}/use1",
			When: `{{if ne .region "cac1"}}true{{end}}`},
	},
}

var envValues = map[string]string{"account": "pt-x-dev-services", "region": "use1", "where": "use1"}

func TestGuideStepsRendersAndFindsRequests(t *testing.T) {
	steps, err := guideSteps(envGuide, envValues, guideRepos())
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 4 {
		t.Fatalf("want 4 steps, got %d", len(steps))
	}
	if got := steps[0].DoneWhen; got != "dirs:cloud/aws/pt-x-dev-services/use1/vpc" {
		t.Errorf("done_when not rendered: %q", got)
	}
	if steps[3].res.Resolved.Repo != "org/k8s" {
		t.Errorf("a step's request is found in any repo, got %q", steps[3].res.Resolved.Repo)
	}
}

func TestGuideStepsWhenLeavesStepOut(t *testing.T) {
	v := map[string]string{"account": "a", "region": "cac1", "where": "cac1"}
	steps, err := guideSteps(envGuide, v, guideRepos())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.Title == "Cluster apps" {
			t.Error("step with a false when should be left out")
		}
	}
}

func TestGuideStepsTitleDefaultsToRequestLabel(t *testing.T) {
	g := config.Resource{Name: "g", Steps: []config.StepConfig{{Request: "eks"}}}
	steps, err := guideSteps(g, envValues, guideRepos())
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Title != "New EKS cluster" {
		t.Errorf("got %q", steps[0].Title)
	}
}

func TestGuideStepsConfigMistakes(t *testing.T) {
	for name, step := range map[string]config.StepConfig{
		"unknown request": {Title: "Nope", Request: "nope"},
		"a guide":         {Title: "Loop", Request: "environment"},
	} {
		g := config.Resource{Name: "g", Steps: []config.StepConfig{step}}
		_, err := guideSteps(g, envValues, guideRepos())
		if err == nil || !strings.Contains(err.Error(), "platformr.toml") {
			t.Errorf("%s: want a plain config error, got %v", name, err)
		}
	}
}

func prBody(request string, details map[string]string) string {
	b := "## " + request + " request\n\nOpened via `platformr`\n\n### Details\n\n"
	for k, v := range details {
		b += "- **" + k + "**: " + v + "\n"
	}
	return b
}

func TestPRIsForStep(t *testing.T) {
	same := ghclient.PRStatus{Body: prBody("vpc", map[string]string{"account": "pt-x-dev-services", "where": "use1", "name": "services", "cidr": "10.0.0.0/16"})}
	if !prIsForStep(same, "vpc", envValues) {
		t.Error("PR with the guide's answers should match")
	}
	if prIsForStep(same, "eks", envValues) {
		t.Error("PR from another request should not match")
	}
	other := ghclient.PRStatus{Body: prBody("vpc", map[string]string{"account": "pt-y-dev-services", "where": "use1"})}
	if prIsForStep(other, "vpc", envValues) {
		t.Error("PR for another account should not match")
	}
	otherEnv := ghclient.PRStatus{Body: prBody("vpc", map[string]string{"account": "pt-x-dev-services", "where": "qa/use1"})}
	if prIsForStep(otherEnv, "vpc", envValues) {
		t.Error("PR for another environment in the same account should not match")
	}
	nothingShared := ghclient.PRStatus{Body: prBody("vpc", map[string]string{"name": "services"})}
	if prIsForStep(nothingShared, "vpc", envValues) {
		t.Error("PR sharing no answers with the guide should not match")
	}
}

func TestSetStepStates(t *testing.T) {
	steps, err := guideSteps(envGuide, envValues, guideRepos())
	if err != nil {
		t.Fatal(err)
	}
	var checked []string
	hasDirs := func(repo, ref, path string) (bool, error) {
		checked = append(checked, repo+"@"+ref+":"+path)
		return path == "cloud/aws/pt-x-dev-services/use1/vpc", nil
	}
	exists := func(repo, ref, path string) (bool, error) { return false, nil }
	prs := []ghclient.PRStatus{
		{State: "open", Number: 7, URL: "https://x/7", Body: prBody("eks", map[string]string{"account": "pt-x-dev-services", "where": "use1"})},
		{State: "merged", Number: 6, URL: "https://x/6", Body: prBody("rds", map[string]string{"account": "pt-x-dev-services"})},
	}
	setStepStates(steps, envValues, prs, exists, hasDirs)

	want := []stepState{stepDone, stepWaiting, stepTodo, stepTodo}
	for i, w := range want {
		if steps[i].state != w {
			t.Errorf("step %d (%s): state %d, want %d", i, steps[i].Title, steps[i].state, w)
		}
	}
	if steps[1].prNumber != 7 {
		t.Errorf("waiting step should carry its PR, got #%d", steps[1].prNumber)
	}
	// Database has no done_when, so three paths are checked.
	if len(checked) != 3 || checked[0] != "org/tf@main:cloud/aws/pt-x-dev-services/use1/vpc" || checked[2] != "org/k8s@main:cloud/aws/pt-x-dev-services/use1" {
		t.Errorf("done_when should be checked in the step request's repo at its base branch: %v", checked)
	}

	// A step with no done_when is never done, and a failed check counts as not done.
	if stepIsDone(config.StepConfig{}, config.Resource{}, exists, hasDirs) {
		t.Error("no done_when: never done")
	}
	failing := func(repo, ref, path string) (bool, error) { return true, errFake }
	if stepIsDone(config.StepConfig{DoneWhen: "a/b"}, config.Resource{}, failing, failing) {
		t.Error("a check that errors should count as not done")
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "boom" }

var errFake = fakeErr{}

func TestNextStep(t *testing.T) {
	mk := func(states ...stepState) []guideStep {
		var out []guideStep
		for _, s := range states {
			out = append(out, guideStep{state: s})
		}
		return out
	}
	if got := nextStep(mk(stepDone, stepTodo, stepTodo)); got != 1 {
		t.Errorf("next is the first required step to do, got %d", got)
	}
	if got := nextStep(mk(stepDone, stepWaiting, stepTodo)); got != -1 {
		t.Errorf("no next while the first unfinished step waits on its PR, got %d", got)
	}
	opt := mk(stepDone, stepTodo, stepTodo)
	opt[1].Optional = true
	if got := nextStep(opt); got != 2 {
		t.Errorf("optional steps are never next, got %d", got)
	}
	if got := nextStep(mk(stepDone, stepDone)); got != -1 || !allRequiredDone(mk(stepDone, stepDone)) {
		t.Errorf("all done: no next, got %d", got)
	}

	before := mk(stepDone, stepTodo, stepTodo)
	before[1].Title = "Network"
	if got := unfinishedBefore(before, 2); got != "Network" {
		t.Errorf("unfinishedBefore = %q", got)
	}
	if got := unfinishedBefore(before, 1); got != "" {
		t.Errorf("nothing unfinished before step 1, got %q", got)
	}
}

func TestStepHint(t *testing.T) {
	cases := []struct {
		s      guideStep
		isNext bool
		want   string
	}{
		{guideStep{state: stepDone, StepConfig: config.StepConfig{Hint: "h"}}, false, "done"},
		{guideStep{state: stepWaiting, prNumber: 361}, false, "waiting: PR #361 isn't merged yet"},
		{guideStep{state: stepWaiting}, false, "waiting: its PR isn't merged yet"},
		{guideStep{StepConfig: config.StepConfig{Hint: "needs a VPC"}}, true, "next · needs a VPC"},
		{guideStep{StepConfig: config.StepConfig{Optional: true}}, false, "optional"},
	}
	for _, c := range cases {
		if got := stepHint(c.s, c.isNext); got != c.want {
			t.Errorf("stepHint = %q, want %q", got, c.want)
		}
	}
}

func TestPresetAnswer(t *testing.T) {
	presets := map[string]string{"account": "a", "env_folder": "", "where": "use1"}
	if v, ok := presetAnswer(config.Field{Name: "account", Type: "select"}, presets); !ok || v != "a" {
		t.Errorf("a question the guide answered is preset: %q %v", v, ok)
	}
	if v, ok := presetAnswer(config.Field{Name: "env_folder", Type: "select", Optional: true}, presets); !ok || v != "" {
		t.Errorf("an empty answer (no environment folder) is still an answer: %q %v", v, ok)
	}
	for _, f := range []config.Field{
		{Name: "where", Type: "computed"},
		{Name: "account", Type: "file_lookup"},
		{Name: "account", Type: "select", Key: "locals.account"},
		{Name: "name", Type: "input"},
	} {
		if _, ok := presetAnswer(f, presets); ok {
			t.Errorf("%s (%s) should not be preset", f.Name, f.Type)
		}
	}
	if _, ok := presetAnswer(config.Field{Name: "account"}, nil); ok {
		t.Error("no guide: nothing preset")
	}
}

func TestGuideSearchAndSummary(t *testing.T) {
	if got := guideSearch(envGuide, envValues); got != `"pt-x-dev-services"` {
		t.Errorf("guideSearch = %s", got)
	}
	if got := guideSummary(envGuide, envValues); got != "pt-x-dev-services · use1" {
		t.Errorf("guideSummary = %q", got)
	}
}

func TestGuideRendersNoTemplates(t *testing.T) {
	org := &config.OrgConfig{}
	repo := &config.RepoConfig{RepoName: "org/tf", Resources: []config.Resource{envGuide}}
	config.Resolve(org, repo)
	if d := repo.Resources[0].Resolved.TemplateDir; d != "" {
		t.Errorf("a guide has no template folder, got %q", d)
	}
}

func TestRenderInstructionsHasPRBranch(t *testing.T) {
	values := map[string]string{"account": "a"}
	got := renderInstructions("git switch {{.pr_branch}}\ncd cloud/aws/{{.account}}", values, "platformr/vpc-a", nil)
	if got != "git switch platformr/vpc-a\ncd cloud/aws/a" {
		t.Errorf("got %q", got)
	}
	if _, ok := values["pr_branch"]; ok {
		t.Error("pr_branch must not be added to the answers (it would show in Details)")
	}
	if renderInstructions("", values, "b", nil) != "" {
		t.Error("no instructions: nothing")
	}
}
