package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/devops-chris/platformr/internal/template"
)

// A guide is a request with [[resources.steps]]: its questions are asked once, then it
// shows its steps' requests in order, each done, waiting on an open PR, or still to do,
// and starts the one picked with the guide's answers filled in. It opens no PR itself.

type stepState int

const (
	stepTodo stepState = iota
	stepDone
	stepWaiting
)

// guideStep is one step that applies to these answers, with its request and state.
type guideStep struct {
	config.StepConfig // strings rendered with the guide's answers
	res               config.Resource
	state             stepState
	prURL             string // the open PR it's waiting on
	prNumber          int    // 0 when the PR was just opened (its number isn't known)
}

// guideDoneValue is the "Done for now" option's value. No step index can be this.
const guideDoneValue = "\x00done"

func runGuide(guide config.Resource, values map[string]string, repos []*config.RepoConfig, gh, ghWrite *ghclient.Client, binaryName string) error {
	steps, err := guideSteps(guide, values, repos)
	if err != nil {
		return err
	}

	// PRs opened from this guide in this run, by step: shown as waiting straight away,
	// since GitHub search takes a while to find a new PR.
	opened := map[int]string{}

	for {
		var warn string
		_ = spinner.New().
			Title("Checking what's done...").
			Action(func() { warn = checkSteps(steps, guideSearch(guide, values), values, gh) }).
			Run()
		for i := range steps {
			if url := opened[i]; url != "" && steps[i].state == stepTodo {
				steps[i].state, steps[i].prURL, steps[i].prNumber = stepWaiting, url, 0
			}
		}

		fmt.Println()
		fmt.Println("  " + ui.SectionHeader(guide.Label()) + "  " + ui.Subtle(guideSummary(guide, values)))
		if warn != "" {
			fmt.Println(ui.Warning(warn))
		}
		for _, s := range steps {
			if s.state == stepWaiting {
				fmt.Println(ui.Subtle(fmt.Sprintf("  %s is waiting on %s", s.Title, hyperlink(s.prURL))))
			}
		}

		next := nextStep(steps)
		current := guideDoneValue
		if next >= 0 {
			current = strconv.Itoa(next)
		}
		opts := make([]menuOption, 0, len(steps)+1)
		for i, s := range steps {
			opts = append(opts, menuOption{stepLabel(i, s), stepHint(s, i == next), strconv.Itoa(i)})
		}
		opts = append(opts, menuOption{"Done for now", "run this again any time to pick up where you left off", guideDoneValue})

		title := "Pick a step to start it"
		if next < 0 && allRequiredDone(steps) {
			title = "Every step is done. Pick one to run it again"
		}
		v, err := askMenu(title, opts, false, current)
		if errors.Is(err, errBack) || v == guideDoneValue {
			fmt.Println(ui.Subtle(fmt.Sprintf("  To come back to this: %s request %s", binaryName, guide.Name)))
			return nil
		}
		if err != nil {
			return err
		}
		i, _ := strconv.Atoi(v)
		s := steps[i]

		if s.state == stepWaiting {
			fmt.Println(ui.Warning(fmt.Sprintf("%s already has an open PR: %s. Merge it (after applying what it says), then come back here.", s.Title, hyperlink(s.prURL))))
			continue
		}
		if before := unfinishedBefore(steps, i); before != "" {
			ok, err := confirm(fmt.Sprintf("%s isn't done yet, and it comes before %s. Start %s anyway?", before, s.Title, s.Title))
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
		}

		if err := runResource(s.res, repos, gh, ghWrite, binaryName, values); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return err
			}
			fmt.Println(ui.Error(err.Error()))
		}
		if lastPRURL != "" {
			opened[i] = lastPRURL
		}
	}
}

// guideSteps renders the guide's steps with its answers, leaves out those whose when
// isn't true, and finds each one's request. A step naming a request that doesn't exist
// (or another guide) is a config mistake, reported before anything is shown.
func guideSteps(guide config.Resource, values map[string]string, repos []*config.RepoConfig) ([]guideStep, error) {
	maps := remote.MapsFor(guide, repos)
	var out []guideStep
	for _, sc := range guide.Steps {
		if sc.When != "" && template.RenderString(sc.When, values, maps) != "true" {
			continue
		}
		res, ok := remote.FindResource(sc.Request, repos)
		if !ok {
			return nil, fmt.Errorf("the %q guide's step %q runs the %q request, which doesn't exist. Ask the platform team to fix it in platformr.toml", guide.Name, sc.Title, sc.Request)
		}
		if res.IsGuide() {
			return nil, fmt.Errorf("the %q guide's step %q runs %q, which is a guide itself. A step must be a request. Ask the platform team to fix it in platformr.toml", guide.Name, sc.Title, sc.Request)
		}
		sc.Title = template.RenderString(sc.Title, values, maps)
		if sc.Title == "" {
			sc.Title = res.Label()
		}
		sc.DoneWhen = template.RenderString(sc.DoneWhen, values, maps)
		sc.Hint = template.RenderString(sc.Hint, values, maps)
		out = append(out, guideStep{StepConfig: sc, res: res})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("none of the %q guide's steps apply to these answers", guide.Name)
	}
	return out, nil
}

// checkSteps sets each step's state from the repos: done when its done_when path is
// there, waiting when an open PR from its request has the guide's answers, otherwise to
// do. Returns a warning when open PRs couldn't be checked (the steps still show done or
// not); a done_when that can't be read counts as not done.
func checkSteps(steps []guideStep, search string, values map[string]string, gh *ghclient.Client) string {
	exists := func(repo, ref, path string) (bool, error) { return gh.FileExists(repo, path, ref) }
	hasDirs := func(repo, ref, path string) (bool, error) {
		dirs, err := gh.ListDirs(repo, path, ref)
		return len(dirs) > 0, err
	}

	var prs []ghclient.PRStatus
	var warn string
	seen := map[string]bool{}
	for _, s := range steps {
		repo := s.res.Resolved.Repo
		if seen[repo] {
			continue
		}
		seen[repo] = true
		found, err := gh.RequestPRs(repo, "", search, 100, false)
		if err != nil {
			warn = "Couldn't check for open PRs, so a step that's waiting on one may show as not done: " + err.Error()
			continue
		}
		prs = append(prs, found...)
	}

	setStepStates(steps, values, prs, exists, hasDirs)
	return warn
}

// pathCheck answers whether a path is there in repo at ref.
type pathCheck func(repo, ref, path string) (bool, error)

func setStepStates(steps []guideStep, values map[string]string, prs []ghclient.PRStatus, exists, hasDirs pathCheck) {
	for i := range steps {
		s := &steps[i]
		s.state, s.prURL, s.prNumber = stepTodo, "", 0
		if stepIsDone(s.StepConfig, s.res, exists, hasDirs) {
			s.state = stepDone
			continue
		}
		for _, pr := range prs {
			if pr.State == "open" && prIsForStep(pr, s.Request, values) {
				s.state, s.prURL, s.prNumber = stepWaiting, pr.URL, pr.Number
				break
			}
		}
	}
}

func stepIsDone(sc config.StepConfig, res config.Resource, exists, hasDirs pathCheck) bool {
	if sc.DoneWhen == "" {
		return false
	}
	var ok bool
	var err error
	if dir, isDirs := strings.CutPrefix(sc.DoneWhen, "dirs:"); isDirs {
		ok, err = hasDirs(res.Resolved.Repo, res.Resolved.BaseBranch, strings.TrimSuffix(dir, "/"))
	} else {
		ok, err = exists(res.Resolved.Repo, res.Resolved.BaseBranch, strings.TrimSuffix(sc.DoneWhen, "/"))
	}
	return err == nil && ok
}

// prIsForStep reports whether pr was opened by request with the guide's answers: every
// answer its Details share with the guide's is the same, and they share at least one
// that isn't empty (so any PR from that request doesn't count).
func prIsForStep(pr ghclient.PRStatus, request string, values map[string]string) bool {
	if parseResourceType(pr.Body) != request {
		return false
	}
	shared := 0
	for _, kv := range parseDetails(pr.Body) {
		v, ok := values[kv[0]]
		if !ok {
			continue
		}
		if v != kv[1] {
			return false
		}
		if v != "" {
			shared++
		}
	}
	return shared > 0
}

// guideSearch narrows the open-PR search to ones mentioning the guide's first answer
// (e.g. the account), which their Details do when they're for the same thing.
func guideSearch(guide config.Resource, values map[string]string) string {
	for _, f := range guide.Fields {
		switch f.Type {
		case "computed", "file_lookup":
			continue
		}
		if v := values[f.Name]; v != "" {
			return strconv.Quote(v)
		}
	}
	return ""
}

// nextStep is the first required step still to do, or -1 when there's none, or the
// first required step not done is waiting on its PR.
func nextStep(steps []guideStep) int {
	for i, s := range steps {
		if s.Optional || s.state == stepDone {
			continue
		}
		if s.state == stepWaiting {
			return -1
		}
		return i
	}
	return -1
}

func allRequiredDone(steps []guideStep) bool {
	for _, s := range steps {
		if !s.Optional && s.state != stepDone {
			return false
		}
	}
	return true
}

// unfinishedBefore names the first required step before i that isn't done, or "".
func unfinishedBefore(steps []guideStep, i int) string {
	for _, s := range steps[:i] {
		if !s.Optional && s.state != stepDone {
			return s.Title
		}
	}
	return ""
}

func stepLabel(i int, s guideStep) string {
	icon := "○"
	switch s.state {
	case stepDone:
		icon = "✓"
	case stepWaiting:
		icon = "…"
	}
	return fmt.Sprintf("%s %d. %s", icon, i+1, s.Title)
}

func stepHint(s guideStep, isNext bool) string {
	var parts []string
	switch {
	case s.state == stepDone:
		parts = append(parts, "done")
	case s.state == stepWaiting && s.prNumber > 0:
		parts = append(parts, fmt.Sprintf("waiting: PR #%d isn't merged yet", s.prNumber))
	case s.state == stepWaiting:
		parts = append(parts, "waiting: its PR isn't merged yet")
	case isNext:
		parts = append(parts, "next")
	case s.Optional:
		parts = append(parts, "optional")
	}
	if s.Hint != "" && s.state != stepDone {
		parts = append(parts, s.Hint)
	}
	return strings.Join(parts, " · ")
}

// guideSummary is the guide's answers to its own questions, e.g. "pt-x-dev-services · use1".
func guideSummary(guide config.Resource, values map[string]string) string {
	var parts []string
	for _, f := range guide.Fields {
		switch f.Type {
		case "computed", "file_lookup":
			continue
		}
		if v := values[f.Name]; v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " · ")
}

// presetAnswer is the guide's answer to field, when it has one and field is a question
// (computed values and lookups are still worked out by the request itself).
func presetAnswer(field config.Field, presets map[string]string) (string, bool) {
	if presets == nil || field.Key != nil || field.List != nil {
		return "", false
	}
	switch field.Type {
	case "computed", "file_lookup", "reviewer", "team_reviewer":
		return "", false
	}
	v, ok := presets[field.Name]
	return v, ok
}

func presetLabel(f config.Field) string {
	if f.Label != "" {
		return f.Label
	}
	return f.Name
}

func presetShown(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

func confirm(question string) (bool, error) {
	var ok bool
	c := huh.NewConfirm().Title(question).Value(&ok)
	c.WithTheme(ui.Theme())
	return ok, c.Run()
}
