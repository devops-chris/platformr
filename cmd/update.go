package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/edit"
	ghclient "github.com/devops-chris/platformr/internal/github"
	"github.com/devops-chris/platformr/internal/prompt"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/devops-chris/platformr/internal/template"
)

// updateSession holds the files a change request reads and edits. Files come from
// the target repo's base branch, since that's what the PR will change. Each file is
// opened once, so several questions and changes can edit the same file in order.
type updateSession struct {
	repo   string
	branch string
	// fetch and exists read the target repo's base branch (swapped out in tests).
	fetch    func(path string) (string, error)
	exists   func(path string) (bool, error)
	maps     map[string]map[string]string
	docs     map[string]edit.Doc
	original map[string][]byte
	order    []string
	deletes  []string
}

// errFileNotFound is what fetch returns for a file that isn't in the repo.
var errFileNotFound = errors.New("file not found")

func newUpdateSession(gh *ghclient.Client, resource config.Resource, maps map[string]map[string]string) *updateSession {
	repo, branch := resource.Resolved.Repo, resource.Resolved.BaseBranch
	return &updateSession{
		repo:   repo,
		branch: branch,
		fetch: func(path string) (string, error) {
			c, err := gh.FetchFile(repo, path, branch)
			if ghclient.IsNotFound(err) {
				return "", errFileNotFound
			}
			return c, err
		},
		exists:   func(path string) (bool, error) { return gh.FileExists(repo, path, branch) },
		maps:     maps,
		docs:     map[string]edit.Doc{},
		original: map[string][]byte{},
	}
}

func (u *updateSession) render(s string, values map[string]string) string {
	return template.RenderString(s, values, u.maps)
}

// open returns the file at path, reading it from the repo the first time.
func (u *updateSession) open(path, format string, key any) (edit.Doc, error) {
	path = strings.TrimPrefix(path, "/")
	if s, ok := key.(string); ok && strings.HasPrefix(s, edit.MarkerPrefix) && format == "" {
		format = string(edit.Marker)
	}
	if d, ok := u.docs[path]; ok {
		return d, nil
	}
	f, err := edit.DetectFormat(path, format)
	if err != nil {
		return nil, err
	}
	content, err := u.fetch(path)
	if err != nil {
		if errors.Is(err, errFileNotFound) {
			return nil, fmt.Errorf("%s doesn't exist in %s (%s branch) — check what was picked, or the request's update_file", path, u.repo, u.branch)
		}
		return nil, err
	}
	d, err := edit.Open(f, path, []byte(content))
	if err != nil {
		return nil, err
	}
	u.docs[path] = d
	u.original[path] = []byte(content)
	u.order = append(u.order, path)
	return d, nil
}

func fileFor(fieldFile, requestFile string) string {
	if fieldFile != "" {
		return fieldFile
	}
	return requestFile
}

// prepareField fills in a question from the file before it's asked: a `key` field
// starts on the current value (shown in its label), and a `list` field offers the
// items that exist.
func (u *updateSession) prepareField(field config.Field, resource config.Resource, values map[string]string) (config.Field, error) {
	file := u.render(fileFor(field.UpdateFile, resource.UpdateFile), values)
	if file == "" {
		return field, fmt.Errorf("question %q uses key/list but the request has no update_file", field.Name)
	}
	render := func(s string) string { return u.render(s, values) }
	if field.List != nil {
		p, err := edit.ParsePath(field.List, render)
		if err != nil {
			return field, fmt.Errorf("question %q list: %w", field.Name, err)
		}
		d, err := u.open(file, fileFor(field.Format, resource.Format), field.List)
		if err != nil {
			return field, err
		}
		items, err := d.Items(p, field.Show)
		if err != nil {
			return field, fmt.Errorf("%s: %w", file, err)
		}
		if len(items) == 0 {
			return field, fmt.Errorf("there's nothing in %s in %s to pick from", p, file)
		}
		field.Options, field.Source = items, ""
	}
	if field.Key != nil && field.Type != "computed" {
		p, err := edit.ParsePath(field.Key, render)
		if err != nil {
			return field, fmt.Errorf("question %q key: %w", field.Name, err)
		}
		d, err := u.open(file, fileFor(field.Format, resource.Format), field.Key)
		if err != nil {
			return field, err
		}
		current, err := d.Get(p)
		if err != nil {
			return field, fmt.Errorf("%s: %w", file, err)
		}
		field.Default = current
		label := field.Label
		if label == "" {
			label = field.Name
		}
		field.Label = fmt.Sprintf("%s (now: %s)", label, current)
		if field.Type == "select" && field.Source == "" && !contains(field.Options, current) {
			// Keep the current value pickable even if it's no longer on the approved list.
			field.Options = append([]string{current}, field.Options...)
		}
	}
	return field, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// apply makes every change the request describes and returns what changed plus the
// files to commit. Values that already match are skipped silently.
func (u *updateSession) apply(resource config.Resource, values map[string]string) ([]edit.Change, []ghclient.PRFile, error) {
	render := func(s string) string { return u.render(s, values) }
	var changes []edit.Change
	record := func(file string, ch edit.Change, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if ch.Verb != "" {
			changes = append(changes, ch)
		}
		return nil
	}

	for _, f := range resource.Fields {
		if f.Key == nil {
			continue
		}
		v, answered := values[f.Name]
		if !answered || (v == "" && f.When != "") {
			continue // skipped by its `when`
		}
		file := render(fileFor(f.UpdateFile, resource.UpdateFile))
		p, err := edit.ParsePath(f.Key, render)
		if err != nil {
			return nil, nil, fmt.Errorf("question %q key: %w", f.Name, err)
		}
		d, err := u.open(file, fileFor(f.Format, resource.Format), f.Key)
		if err != nil {
			return nil, nil, err
		}
		ch, err := d.Apply(edit.Op{Action: "set", Path: p, Value: v})
		if f.Label != "" && ch.Verb != "" {
			ch.What = f.Label
		}
		if err := record(file, ch, err); err != nil {
			return nil, nil, err
		}
	}

	for i, c := range resource.Changes {
		if c.When != "" && render(c.When) != "true" {
			continue
		}
		label := fmt.Sprintf("change #%d (%s)", i+1, c.Action)
		if c.Action == "delete_file" {
			path := strings.TrimPrefix(render(c.File), "/")
			exists, err := u.exists(path)
			if err != nil {
				return nil, nil, err
			}
			if !exists {
				return nil, nil, fmt.Errorf("%s doesn't exist, so there's nothing to delete", path)
			}
			u.deletes = append(u.deletes, path)
			changes = append(changes, edit.Change{What: "file", From: path, Verb: "removed"})
			continue
		}
		file := render(fileFor(c.File, resource.UpdateFile))
		if file == "" {
			return nil, nil, fmt.Errorf("%s has no file — set update_file on the request or file on the change", label)
		}
		p, err := edit.ParsePath(c.Key, render)
		if err != nil {
			return nil, nil, fmt.Errorf("%s key: %w", label, err)
		}
		d, err := u.open(file, fileFor(c.Format, resource.Format), c.Key)
		if err != nil {
			return nil, nil, err
		}
		op := edit.Op{Action: c.Action, Path: p, Name: render(c.Name), Value: render(c.Value)}
		switch c.Action {
		case "append", "put":
			if op.Item, err = toItem(c.Item, render); err != nil {
				return nil, nil, fmt.Errorf("%s item: %w", label, err)
			}
		case "remove":
			if op.Match, err = toMatch(c.Match, render); err != nil {
				return nil, nil, fmt.Errorf("%s match: %w", label, err)
			}
		case "set", "delete":
		default:
			return nil, nil, fmt.Errorf("%s: unknown action %q — use set, append, remove, put, delete or delete_file", label, c.Action)
		}
		if c.Action == "append" && c.UniqueBy != "" {
			show, want := c.UniqueBy, op.Item.Value
			if op.Item.IsObject() {
				want = op.Item.Fields[c.UniqueBy]
			} else {
				show = ""
			}
			existing, err := d.Items(p, show)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", file, err)
			}
			if contains(existing, want) {
				return nil, nil, fmt.Errorf("%s is already in %s", want, p)
			}
		}
		ch, err := d.Apply(op)
		if err := record(file, ch, err); err != nil {
			return nil, nil, err
		}
	}

	var files []ghclient.PRFile
	for _, path := range u.order {
		if b := u.docs[path].Bytes(); !bytes.Equal(b, u.original[path]) {
			files = append(files, ghclient.PRFile{Path: path, Content: string(b)})
		}
	}
	for _, path := range u.deletes {
		files = append(files, ghclient.PRFile{Path: path, Delete: true})
	}
	return changes, files, nil
}

func toItem(v any, render func(string) string) (edit.Item, error) {
	switch x := v.(type) {
	case string:
		return edit.Item{Value: render(x)}, nil
	case int64, float64, bool:
		return edit.Item{Value: fmt.Sprint(x)}, nil
	case map[string]any:
		fields := map[string]string{}
		for k, fv := range x {
			if s, ok := fv.(string); ok {
				fields[k] = render(s)
			} else {
				fields[k] = fmt.Sprint(fv)
			}
		}
		return edit.Item{Fields: fields}, nil
	case nil:
		return edit.Item{}, fmt.Errorf("missing — append and put need an item")
	}
	return edit.Item{}, fmt.Errorf("must be a value or a { field = value } table, not %T", v)
}

func toMatch(v any, render func(string) string) (map[string]string, error) {
	switch x := v.(type) {
	case string:
		return map[string]string{"value": render(x)}, nil
	case map[string]any:
		m := map[string]string{}
		for k, mv := range x {
			if s, ok := mv.(string); ok {
				m[k] = render(s)
			} else {
				m[k] = fmt.Sprint(mv)
			}
		}
		return m, nil
	case nil:
		return nil, fmt.Errorf("missing — remove needs a match")
	}
	return nil, fmt.Errorf("must be a value or a { field = value } table, not %T", v)
}

// updateBranch picks a branch name for a change request. If an open request is
// already changing the same file, it says so instead (a second change would start
// from out-of-date values). A leftover branch from a merged request gets a suffix.
func updateBranch(gh *ghclient.Client, resource config.Resource, files []ghclient.PRFile) (string, error) {
	base := fmt.Sprintf("platformr/%s-%s", resource.Name, slugify(files[0].Path))
	for n := 1; n <= 20; n++ {
		branch := base
		if n > 1 {
			branch = fmt.Sprintf("%s-%d", base, n)
		}
		exists, err := gh.BranchExists(resource.Resolved.Repo, branch)
		if err != nil {
			return "", err
		}
		if !exists {
			return branch, nil
		}
		if url, open, err := gh.OpenPRForBranch(resource.Resolved.Repo, branch); err == nil && open {
			return "", fmt.Errorf("there's already an open request changing %s: %s\nWait for it to be merged or closed, then try again — otherwise this change would start from out-of-date values", files[0].Path, url)
		}
	}
	return "", fmt.Errorf("too many old branches named %s-* — delete some merged ones", base)
}

func changesMarkdown(changes []edit.Change) string {
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n### Changes\n\n")
	for _, c := range changes {
		fmt.Fprintf(&b, "- %s\n", c)
	}
	return b.String()
}

func printChanges(changes []edit.Change) {
	for _, c := range changes {
		fmt.Println("  " + c.String())
	}
}

// printUpdateDryRun shows the summary and, for each file, the lines that change.
func printUpdateDryRun(changes []edit.Change, files []ghclient.PRFile, before map[string][]byte) {
	fmt.Printf("\n  %s  %s\n\n", ui.SectionHeader("Dry run"), ui.Subtle("no PR will be opened"))
	fmt.Printf("  %s\n", ui.SectionHeader("Changes"))
	printChanges(changes)
	for _, f := range files {
		fmt.Printf("\n  %s %s\n", ui.Subtle("→"), f.Path)
		if f.Delete {
			fmt.Println("    (deleted)")
			continue
		}
		for _, line := range lineDiff(string(before[f.Path]), f.Content) {
			fmt.Println("    " + line)
		}
	}
	fmt.Println()
}

// lineDiff shows the changed region of a file with two lines of context. platformr's
// edits touch one spot at a time, so trimming the common start and end is enough.
func lineDiff(a, b string) []string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	pre := 0
	for pre < len(al) && pre < len(bl) && al[pre] == bl[pre] {
		pre++
	}
	suf := 0
	for suf < len(al)-pre && suf < len(bl)-pre && al[len(al)-1-suf] == bl[len(bl)-1-suf] {
		suf++
	}
	var out []string
	for i := max(0, pre-2); i < pre; i++ {
		out = append(out, "  "+al[i])
	}
	for _, l := range al[pre : len(al)-suf] {
		out = append(out, "- "+l)
	}
	for _, l := range bl[pre : len(bl)-suf] {
		out = append(out, "+ "+l)
	}
	for i := len(al) - suf; i < len(al) && i < len(al)-suf+2; i++ {
		if al[i] != "" || i < len(al)-1 {
			out = append(out, "  "+al[i])
		}
	}
	return out
}

// runUpdate finishes a change request once the questions are answered: apply the
// changes, show them, confirm, and open the PR.
func runUpdate(resource config.Resource, repos []*config.RepoConfig, values map[string]string, sess *updateSession, ghWrite, gh *ghclient.Client, binaryName string) error {
	changes, files, err := sess.apply(resource, values)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Println(ui.Success("Nothing to change — everything is already set that way."))
		return nil
	}

	if requestDryRun {
		printUpdateDryRun(changes, files, sess.original)
		return nil
	}

	branch, err := updateBranch(gh, resource, files)
	if err != nil {
		return err
	}

	comment, err := prompt.PromptComment()
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println(ui.SectionHeader("This request changes:"))
	printChanges(changes)
	fmt.Println()

	var confirmed bool
	conf := huh.NewConfirm().
		Title("Open a pull request with this change?").
		Description(fmt.Sprintf("→ %s (%d file(s))", resource.Resolved.Repo, len(files))).
		Value(&confirmed)
	conf.WithTheme(ui.Theme())
	if err := conf.Run(); err != nil {
		return err
	}
	if !confirmed {
		fmt.Println(ui.Warning("Aborted."))
		return nil
	}

	reviewers := append([]string(nil), resource.Reviewers...)
	teamReviewers := append([]string(nil), resource.TeamReviewers...)
	for _, f := range resource.Fields {
		if v := values[f.Name]; v != "" {
			switch f.Type {
			case "reviewer":
				reviewers = append(reviewers, v)
			case "team_reviewer":
				teamReviewers = append(teamReviewers, v)
			}
		}
	}

	maps := remote.MapsFor(resource, repos)
	var prURL string
	var prErr error
	_ = spinner.New().
		Title("Opening PR...").
		Action(func() {
			prURL, prErr = ghWrite.CreatePR(ghclient.PRRequest{
				Repo:          resource.Resolved.Repo,
				Branch:        branch,
				BaseBranch:    resource.Resolved.BaseBranch,
				Title:         template.RenderString(resource.PRTitle, values, maps),
				Body:          buildPRBody(resource.Name, values, comment, template.RenderString(resource.Instructions, values, maps), changesMarkdown(changes)+outputSectionMarkdown(resource, values, repos)),
				Files:         files,
				Reviewers:     reviewers,
				TeamReviewers: teamReviewers,
			})
		}).
		Run()
	if prErr != nil {
		return fmt.Errorf("creating PR: %w", withAuthHint(prErr, binaryName))
	}
	fmt.Println(ui.Success("PR opened: " + hyperlink(prURL)))
	return nil
}

// checkUpdateConfig catches change-request config mistakes before any question is
// asked (and in `platformr doctor`): missing files, unknown formats or actions, and
// changes missing what their action needs.
func checkUpdateConfig(r config.Resource) []error {
	var errs []error
	format := func(file, override string, key any) {
		if s, ok := key.(string); ok && strings.HasPrefix(s, edit.MarkerPrefix) {
			return
		}
		if _, err := edit.DetectFormat(file, override); err != nil {
			errs = append(errs, err)
		}
	}
	for _, f := range r.Fields {
		if f.Key == nil && f.List == nil {
			continue
		}
		file := fileFor(f.UpdateFile, r.UpdateFile)
		if file == "" {
			errs = append(errs, fmt.Errorf("question %q uses key/list, but there's no update_file on the request or the question", f.Name))
			continue
		}
		format(file, fileFor(f.Format, r.Format), f.Key)
	}
	for i, c := range r.Changes {
		label := fmt.Sprintf("change #%d (%s)", i+1, c.Action)
		need := func(ok bool, what string) {
			if !ok {
				errs = append(errs, fmt.Errorf("%s needs %s", label, what))
			}
		}
		switch c.Action {
		case "delete_file":
			need(c.File != "", "file = the file to delete")
			continue
		case "set":
			need(c.Value != "", "value")
		case "append":
			need(c.Item != nil, "item")
		case "put":
			need(c.Item != nil, "item")
			need(c.Name != "", "name")
		case "remove":
			need(c.Match != nil, "match")
		case "delete":
			need(c.Name != "", "name")
		default:
			errs = append(errs, fmt.Errorf("%s: unknown action — use set, append, remove, put, delete or delete_file", label))
			continue
		}
		need(c.Key != nil, "key")
		file := fileFor(c.File, r.UpdateFile)
		if file == "" {
			errs = append(errs, fmt.Errorf("%s has no file — set update_file on the request or file on the change", label))
			continue
		}
		format(file, fileFor(c.Format, r.Format), c.Key)
	}
	return errs
}
