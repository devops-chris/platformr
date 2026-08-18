package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/auth"
	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
	"github.com/devops-chris/platformr/internal/prompt"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/devops-chris/platformr/internal/template"
	"github.com/spf13/cobra"
)

var requestDryRun bool

var requestCmd = &cobra.Command{
	Use:   "request [resource]",
	Short: "Request a new resource",
	Long: `Interactively request a new infrastructure resource or service via a GitOps PR.

Optionally specify the resource type directly to skip the picker:

  platformr request eks
  platformr request vpc

Use --dry-run to see computed values and the rendered template without opening a PR:

  platformr request platform-project --dry-run`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRequest,
}

func init() {
	rootCmd.AddCommand(requestCmd)
	requestCmd.Flags().BoolVar(&requestDryRun, "dry-run", false, "Show computed values and rendered output without opening a PR")
	requestCmd.ValidArgsFunction = completeResourceNames
}

func runRequest(cmd *cobra.Command, args []string) error {
	binaryName := filepath.Base(os.Args[0])
	if localCfg == nil || localCfg.ConnectedOrg == "" {
		fmt.Println(ui.Warning("Not connected. Run `" + binaryName + " connect <org>` first."))
		os.Exit(1)
	}

	// Read operations (config, templates, dir listings) use the read token so
	// they work regardless of which repos the GitHub App is installed on.
	// Write operations (PR creation) use the full token which includes the app token.
	readToken := resolveReadToken()
	writeToken := resolveToken()
	gh := ghclient.New(readToken)
	ghWrite := ghclient.New(writeToken)
	loader := remote.New(readToken)

	// Fail fast on a bad/expired write token — better than discovering it only
	// when CreatePR 401s, after a full round of prompts.
	if _, err := ghWrite.CurrentUser(); err != nil {
		return withAuthHint(err, binaryName)
	}

	// Fetch all resource definitions from registered IaC repos
	var repos []*config.RepoConfig // this is resolved in the spinner
	var loadErr error
	_ = spinner.New().
		Title("Loading resources...").
		Action(func() {
			_, repos, loadErr = loader.LoadAll(localCfg.ConnectedOrg)
		}).
		Run()

	if loadErr != nil {
		return fmt.Errorf("loading resources: %w", loadErr)
	}

	allResources := remote.AllResources(repos)
	if len(allResources) == 0 {
		fmt.Println(ui.Warning("No resources found. Ensure your IaC repos have a platformr.toml."))
		os.Exit(1)
	}

	// Pick resource type
	var resource config.Resource
	if len(args) == 1 {
		found, ok := remote.FindResource(args[0], repos)
		if !ok {
			return fmt.Errorf("resource %q not found — run `%s catalog` to see available resources", args[0], binaryName)
		}
		resource = found
	} else {
		var err error
		resource, err = pickResource(allResources)
		if err != nil {
			return err
		}
	}

	// Collect field values
	values, err := collectFields(resource, repos, gh)
	if err != nil {
		return err
	}

	// Prompt for optional PR comment
	comment, err := prompt.PromptComment()
	if err != nil {
		return err
	}

	// Fetch template(s) from the IaC repo and render
	var prFiles []ghclient.PRFile
	var skippedFiles []string
	var tmplErr error
	_ = spinner.New().
		Title("Fetching template...").
		Action(func() {
			if resource.Resolved.TemplateDir != "" {
				// Multi-file mode: render every .tmpl file in the directory
				tmplFiles, err := gh.FetchTemplateDir(resource.Resolved.TemplateRepo, resource.Resolved.TemplateDir, resource.Resolved.TemplateRef)
				if err != nil {
					tmplErr = err
					return
				}
				if len(tmplFiles) == 0 {
					tmplErr = fmt.Errorf("no .tmpl files found in %s (repo %s, ref %q) — check template_dir_path and the configured ref",
						resource.Resolved.TemplateDir, resource.Resolved.TemplateRepo, resource.Resolved.TemplateRef)
					return
				}
				resMaps := remote.MapsFor(resource, repos)
				targetPath := template.RenderString(resource.Resolved.TargetPath, values, resMaps)
				for _, tf := range tmplFiles {
					rendered, err := template.Render(tf.Content, values, resMaps)
					if err != nil {
						tmplErr = fmt.Errorf("rendering %s: %w", tf.Name, err)
						return
					}
					// sourceName (unrendered) is the matching key against TemplateFiles config —
					// unique on disk even when several files must render to the same outName.
					sourceName := strings.TrimSuffix(tf.Name, ".tmpl")
					outName := template.RenderString(sourceName, values)
					if skipPath := resolveSkipIfExists(resource, sourceName, values, resMaps); skipPath != "" {
						var exists bool
						exists, _ = gh.FileExists(resource.Resolved.Repo, skipPath, resource.Resolved.BaseBranch)
						if exists {
							skippedFiles = append(skippedFiles, outName)
							continue
						}
					}
					if override := resolveOutputName(resource, sourceName, values, resMaps); override != "" {
						outName = override
					}
					filePath := targetPath + outName
					if override := resolveFileTargetPath(resource, sourceName, values, resMaps); override != "" {
						filePath = override + outName
					}
					prFiles = append(prFiles, ghclient.PRFile{
						Path:    filePath,
						Content: rendered,
					})
				}
			} else {
				// Single-file mode
				content, err := gh.FetchFile(resource.Resolved.TemplateRepo, resource.Resolved.Template, resource.Resolved.TemplateRef)
				if err != nil {
					tmplErr = err
					return
				}
				rendered, err := template.Render(content, values, remote.MapsFor(resource, repos))
				if err != nil {
					tmplErr = fmt.Errorf("rendering template: %w", err)
					return
				}
				prFiles = []ghclient.PRFile{{
					Path:    resolveFilePath(resource, values),
					Content: rendered,
				}}
			}
		}).
		Run()
	if tmplErr != nil {
		return fmt.Errorf("fetching template: %w", tmplErr)
	}

	for _, name := range skippedFiles {
		fmt.Println(ui.Subtle(fmt.Sprintf("Skipped %s (already exists in target repo)", name)))
	}

	if len(prFiles) == 0 {
		fmt.Println(ui.Warning("All template files already exist in the target repo — nothing to commit."))
		return nil
	}

	// Dry-run: print values + rendered output and exit without opening a PR
	if requestDryRun {
		printDryRun(resource, values, prFiles)
		return nil
	}

	// Confirm — show target path (dir for multi-file, full path for single-file)
	confirmDesc := fmt.Sprintf("→ %s/%s", resource.Resolved.Repo, prFiles[0].Path)
	if len(prFiles) > 1 {
		targetPath := template.RenderString(resource.Resolved.TargetPath, values, remote.MapsFor(resource, repos))
		confirmDesc = fmt.Sprintf("→ %s/%s (%d files)", resource.Resolved.Repo, targetPath, len(prFiles))
	}

	var confirmed bool
	conf := huh.NewConfirm().
		Title("Open a pull request with this request?").
		Description(confirmDesc).
		Value(&confirmed)
	conf.WithTheme(ui.Theme())
	if err := conf.Run(); err != nil {
		return err
	}
	if !confirmed {
		fmt.Println(ui.Warning("Aborted."))
		return nil
	}

	// Build reviewer lists: config-driven + any type="reviewer" / type="team_reviewer" fields
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

	// Open PR
	var prURL string
	var prErr error
	_ = spinner.New().
		Title("Opening PR...").
		Action(func() {
			prURL, prErr = ghWrite.CreatePR(ghclient.PRRequest{
				Repo:          resource.Resolved.Repo,
				Branch:        fmt.Sprintf("platformr/%s-%s", resource.Name, resolveSlug(resource, values)),
				BaseBranch:    resource.Resolved.BaseBranch,
				Title:         template.RenderString(resource.PRTitle, values, remote.MapsFor(resource, repos)),
				Body:          buildPRBody(resource.Name, values, comment, outputSectionMarkdown(resource, values, repos)),
				Files:         prFiles,
				Reviewers:     reviewers,
				TeamReviewers: teamReviewers,
			})
		}).
		Run()

	if prErr != nil {
		return fmt.Errorf("creating PR: %w", withAuthHint(prErr, binaryName))
	}

	fmt.Println(ui.Success("PR opened: " + hyperlink(prURL)))
	printOutputPath(resource, values, repos)
	return nil
}

// resolveOutputPrefix renders a resource's output_path with the given values, or
// returns "" if output_path isn't configured. Shared by the terminal printout and
// the PR body section so both stay in sync from one computation.
//
// {{.resource}} is only handled by resolver.go's renderPattern for target_path/
// template_dir — add it here too so output_path can use the same placeholder.
func resolveOutputPrefix(resource config.Resource, values map[string]string, repos []*config.RepoConfig) string {
	if resource.OutputPath == "" {
		return ""
	}
	ctxValues := copyMap(values)
	ctxValues["resource"] = resource.Name
	return template.RenderString(resource.OutputPath, ctxValues, remote.MapsFor(resource, repos))
}

// printOutputPath shows where this resource's outputs will land once applied, if the
// platform team has configured output_path for it. platformr never reads or writes
// anything there itself — this is purely informational.
func printOutputPath(resource config.Resource, values map[string]string, repos []*config.RepoConfig) {
	prefix := resolveOutputPrefix(resource, values, repos)
	if prefix == "" {
		return
	}

	if len(resource.OutputKeys) == 0 {
		fmt.Println(ui.Subtle("Once applied, outputs should be written under: " + prefix))
		return
	}

	fmt.Println(ui.Subtle("Once applied, outputs should be written to:"))
	for _, key := range resource.OutputKeys {
		fmt.Printf("    %-14s %s\n", key, ui.Subtle(prefix+key))
	}
}

// outputSectionMarkdown returns a "### Outputs" section for the PR body, or "" if
// output_path isn't configured. Embedding the already-rendered paths in the PR body
// (rather than just printing them to the terminal) means `platformr status` can read
// them back verbatim later — no re-deriving field values, no separate process to keep
// in sync, no drift beyond someone hand-editing the PR description.
func outputSectionMarkdown(resource config.Resource, values map[string]string, repos []*config.RepoConfig) string {
	prefix := resolveOutputPrefix(resource, values, repos)
	if prefix == "" {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n\n", outputsHeading)
	if resource.OutputCloud != "" {
		fmt.Fprintf(&b, "Platform: %s\n\n", resource.OutputCloud)
	}

	if len(resource.OutputKeys) == 0 {
		fmt.Fprintf(&b, "Once applied, outputs should be written under: `%s`\n", prefix)
		return b.String()
	}

	fmt.Fprintf(&b, "Once applied, outputs should be written to:\n\n")
	for _, key := range resource.OutputKeys {
		fmt.Fprintf(&b, "- **%s**: `%s`\n", key, prefix+key)
	}
	return b.String()
}

// withAuthHint appends a re-auth hint to err when it's a GitHub 401 caused by an
// expired stored App token (see ghclient.IsUnauthorized and auth.LoadToken).
func withAuthHint(err error, binaryName string) error {
	if ghclient.IsUnauthorized(err) && auth.LoadToken() != "" {
		return fmt.Errorf("%w\n\nYour stored platformr token has likely expired — run `%s auth` to reauthorize.", err, binaryName)
	}
	return err
}

func pickResource(allResources []config.Resource) (config.Resource, error) {
	// Collect unique categories in first-seen order.
	seen := map[string]bool{}
	var categories []string
	for _, r := range allResources {
		cat := r.Category
		if cat == "" {
			cat = "General"
		}
		if !seen[cat] {
			seen[cat] = true
			categories = append(categories, cat)
		}
	}

	// No categories or only one — skip category step entirely.
	if len(categories) <= 1 {
		desc := "Select a resource type"
		if len(categories) == 1 {
			desc = categories[0]
		}
		return pickFromList("What would you like to request?", desc, allResources)
	}

	// Step 1: pick a category.
	catOpts := make([]huh.Option[string], len(categories))
	for i, cat := range categories {
		count := len(resourcesInCategory(allResources, cat))
		catOpts[i] = huh.NewOption(ui.CategoryOption(cat, count), cat)
	}
	var selectedCat string
	catSel := huh.NewSelect[string]().
		Title("What type of resource?").
		Description("Select a category").
		Options(catOpts...).
		Value(&selectedCat)
	catSel.WithTheme(ui.Theme())
	if err := catSel.Run(); err != nil {
		return config.Resource{}, err
	}

	// Step 2: pick resource within that category.
	return pickFromList("What would you like to request?", selectedCat, resourcesInCategory(allResources, selectedCat))
}

func resourcesInCategory(all []config.Resource, cat string) []config.Resource {
	var result []config.Resource
	for _, r := range all {
		rc := r.Category
		if rc == "" {
			rc = "General"
		}
		if rc == cat {
			result = append(result, r)
		}
	}
	return result
}

func pickFromList(title, description string, resources []config.Resource) (config.Resource, error) {
	opts := make([]huh.Option[string], len(resources))
	for i, r := range resources {
		opts[i] = huh.NewOption(ui.PickerItem(r.Label(), r.Description), r.Name)
	}
	var selected string
	sel := huh.NewSelect[string]().
		Title(title).
		Description(description).
		Options(opts...).
		Value(&selected)
	sel.WithTheme(ui.Theme())
	if err := sel.Run(); err != nil {
		return config.Resource{}, err
	}
	for _, r := range resources {
		if r.Name == selected {
			return r, nil
		}
	}
	return config.Resource{}, fmt.Errorf("resource %q not found", selected)
}

func collectFields(resource config.Resource, repos []*config.RepoConfig, gh *ghclient.Client) (map[string]string, error) {
	values := make(map[string]string)

	for _, field := range resource.Fields {
		// Evaluate conditional — skip field and set to "" if condition is not met
		if field.When != "" && template.RenderString(field.When, values, remote.MapsFor(resource, repos)) != "true" {
			values[field.Name] = ""
			continue
		}

		// Computed fields derive their value from a template expression — no prompt
		if field.Type == "computed" {
			values[field.Name] = template.RenderString(field.Value, values, remote.MapsFor(resource, repos))
			continue
		}

		ctx := buildFieldContext(field, resource, repos, gh, values)

		val, err := prompt.PromptField(field, values, ctx)
		if err != nil {
			return nil, err
		}

		// "[+ create new]" on a dirs:-sourced field means "let me type one instead of
		// picking an existing directory" — there's no dependency resource to create,
		// just fall through to a plain text prompt.
		if val == prompt.CreateNewOption && strings.HasPrefix(field.Source, "dirs:") {
			typed, err := prompt.PromptField(config.Field{Name: field.Name, Label: field.Label, Type: "input", Placeholder: field.Placeholder}, values, nil)
			if err != nil {
				return nil, err
			}
			val = typed
		}

		// Uniqueness check — build a candidate path using current values + this field
		if field.Validate == "unique" {
			candidateValues := copyMap(values)
			candidateValues[field.Name] = val
			candidatePath := resolveFilePath(resource, candidateValues)
			var exists bool
			_ = spinner.New().
				Title(fmt.Sprintf("Checking if %q already exists...", val)).
				Action(func() {
					exists, _ = gh.FileExists(resource.Resolved.Repo, candidatePath, resource.Resolved.BaseBranch)
				}).
				Run()

			if exists {
				fmt.Println(ui.Error(fmt.Sprintf("A %s named %q already exists.", resource.Name, val)))
				os.Exit(1)
			}
			fmt.Println(ui.Success(fmt.Sprintf("No conflicts found for %q.", val)))
		}

		values[field.Name] = val
	}

	return values, nil
}

func buildFieldContext(field config.Field, resource config.Resource, repos []*config.RepoConfig, gh *ghclient.Client, values map[string]string) *prompt.FieldContext {
	if field.Source == "" {
		return &prompt.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) { return field.Options, nil },
		}
	}

	if strings.HasPrefix(field.Source, "dirs:") {
		dirPath := template.RenderString(strings.TrimPrefix(field.Source, "dirs:"), values, remote.MapsFor(resource, repos))
		return &prompt.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) {
				return gh.ListDirs(resource.Resolved.TemplateRepo, dirPath, resource.Resolved.TemplateRef)
			},
		}
	}

	// Same idea as "dirs:", for resources committed as one flat file per instance
	// (e.g. platform-project/foo.yaml) instead of one directory per instance.
	if strings.HasPrefix(field.Source, "files:") {
		filePath := template.RenderString(strings.TrimPrefix(field.Source, "files:"), values, remote.MapsFor(resource, repos))
		return &prompt.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) {
				return gh.ListFiles(resource.Resolved.TemplateRepo, filePath, resource.Resolved.TemplateRef)
			},
		}
	}

	if strings.HasPrefix(field.Source, "team:") {
		teamSlug := strings.TrimPrefix(field.Source, "team:")
		return &prompt.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) {
				return gh.ListTeamMembers(resource.Resolved.Org, teamSlug)
			},
		}
	}

	if field.Source == "collaborators" {
		return &prompt.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) {
				return gh.ListCollaborators(resource.Resolved.Repo)
			},
		}
	}

	return &prompt.FieldContext{
		ListFiles: func(_, _ string) ([]string, error) { return field.Options, nil },
	}
}

// resolveFilePath builds the full file path for the PR commit.
// target_path is re-rendered with field values, then file_name + file_ext are appended.
// Defaults: file_name = first field value, file_ext = ".yaml"
func resolveFilePath(resource config.Resource, values map[string]string) string {
	targetPath := template.RenderString(resource.Resolved.TargetPath, values)

	fileName := resource.FileName
	if fileName == "" {
		fileName = "{{." + firstFieldName(resource) + "}}"
	}
	fileName = template.RenderString(fileName, values)

	ext := resource.FileExt
	if ext == "" {
		ext = ".yaml"
	}

	return targetPath + fileName + ext
}

// resolveSlug returns a short identifier for branch names, unique enough that two
// different requests never collide on the same branch. Defaulting to just the
// first field's value (e.g. "account") isn't enough — every resource in this repo
// starts with an "account" field, so two requests for the same resource type in
// the same account (different env/region/name) would render the same branch name
// and the second one's CreatePR would 422 on "Reference already exists". Deriving
// the default from the resource's own rendered target_path instead ties the slug
// to everything that actually makes the request unique.
func resolveSlug(resource config.Resource, values map[string]string) string {
	if resource.FileName != "" {
		return template.RenderString(resource.FileName, values)
	}
	targetPath := template.RenderString(resource.Resolved.TargetPath, values)
	return slugify(targetPath)
}

// slugify turns a rendered path (or any string) into a branch-name-safe slug:
// lowercase, "/" and whitespace collapsed to "-", anything else not
// alphanumeric/hyphen stripped, leading/trailing hyphens trimmed.
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// firstFieldName returns the name of the first field defined on the resource.
func firstFieldName(resource config.Resource) string {
	if len(resource.Fields) > 0 {
		return resource.Fields[0].Name
	}
	return "name"
}

// resolveSkipIfExists returns the rendered skip_if_exists path for the given source
// filename (unrendered, matches TemplateFileConfig.Name), or "" if none is configured.
func resolveSkipIfExists(resource config.Resource, sourceName string, values map[string]string, maps map[string]map[string]string) string {
	for _, tf := range resource.TemplateFiles {
		if tf.Name == sourceName && tf.SkipIfExists != "" {
			return template.RenderString(tf.SkipIfExists, values, maps)
		}
	}
	return ""
}

// resolveOutputName returns the rendered output_name override for the given source
// filename (unrendered, matches TemplateFileConfig.Name), or "" if none is configured —
// callers fall back to the file's own rendered name. Needed when multiple source files
// must render to the identical final filename (e.g. several "terragrunt.hcl" files in
// different directories) — source names disambiguate the match, this decides the output.
func resolveOutputName(resource config.Resource, sourceName string, values map[string]string, maps map[string]map[string]string) string {
	for _, tf := range resource.TemplateFiles {
		if tf.Name == sourceName && tf.OutputName != "" {
			return template.RenderString(tf.OutputName, values, maps)
		}
	}
	return ""
}

// resolveFileTargetPath returns the rendered per-file target_path directory for the given output
// filename, or "" if no override is configured (caller uses the resource-level targetPath).
func resolveFileTargetPath(resource config.Resource, outName string, values map[string]string, maps map[string]map[string]string) string {
	for _, tf := range resource.TemplateFiles {
		if tf.Name == outName && tf.TargetPath != "" {
			return template.RenderString(tf.TargetPath, values, maps)
		}
	}
	return ""
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func printDryRun(resource config.Resource, values map[string]string, files []ghclient.PRFile) {
	computedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted).Italic(true)
	nameStyle := lipgloss.NewStyle().Width(18)
	divider := ui.Subtle(strings.Repeat("─", 52))

	// Build computed field set
	computed := map[string]bool{}
	for _, f := range resource.Fields {
		if f.Type == "computed" {
			computed[f.Name] = true
		}
	}

	fmt.Printf("\n  %s  %s\n", ui.SectionHeader("Dry run"), ui.Subtle("no PR will be opened"))

	// Field values — in definition order
	fmt.Printf("\n  %s\n  %s\n", ui.SectionHeader("Field values"), divider)
	for _, f := range resource.Fields {
		v := values[f.Name]
		tag := ""
		if computed[f.Name] {
			tag = "  " + computedStyle.Render("(computed)")
		}
		fmt.Printf("  %s%s%s\n", nameStyle.Render(f.Name), v, tag)
	}

	// Files
	fmt.Printf("\n  %s\n  %s\n", ui.SectionHeader("Files"), divider)
	for _, file := range files {
		fmt.Printf("\n  %s %s\n\n", ui.Subtle("→"), file.Path)
		for _, line := range strings.Split(strings.TrimRight(file.Content, "\n"), "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
	fmt.Println()
}

func buildPRBody(resourceName string, values map[string]string, comment string, outputSection string) string {
	body := fmt.Sprintf("## %s request\n\nOpened via `%s`\n\n### Details\n\n", resourceName, filepath.Base(os.Args[0]))
	for k, v := range values {
		body += fmt.Sprintf("- **%s**: %s\n", k, v)
	}
	if comment != "" {
		body += fmt.Sprintf("\n### Notes\n\n%s\n", comment)
	}
	body += outputSection
	return body
}
