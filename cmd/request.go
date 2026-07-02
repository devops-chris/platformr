package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/devops-chris/platformr/internal/template"
	"github.com/devops-chris/platformr/internal/ui"
	"github.com/spf13/cobra"
)

var requestCmd = &cobra.Command{
	Use:   "request [resource]",
	Short: "Request a new resource",
	Long: `Interactively request a new infrastructure resource or service via a GitOps PR.

Optionally specify the resource type directly to skip the picker:

  platformr request eks
  platformr request vpc`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRequest,
}

func init() {
	rootCmd.AddCommand(requestCmd)
}

func runRequest(cmd *cobra.Command, args []string) error {
	if localCfg == nil || localCfg.ConnectedOrg == "" {
		fmt.Println(ui.Warning("Not connected. Run `platformr connect <org>` first."))
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
			return fmt.Errorf("resource %q not found — run `platformr catalog` to see available resources", args[0])
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
	values, err := collectFields(resource, repos, gh, ghWrite)
	if err != nil {
		return err
	}

	// Prompt for optional PR comment
	comment, err := ui.PromptComment()
	if err != nil {
		return err
	}

	// Fetch template(s) from the IaC repo and render
	var prFiles []ghclient.PRFile
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
				targetPath := template.RenderString(resource.Resolved.TargetPath, values)
				for _, tf := range tmplFiles {
					rendered, err := template.Render(tf.Content, values)
					if err != nil {
						tmplErr = fmt.Errorf("rendering %s: %w", tf.Name, err)
						return
					}
					outName := template.RenderString(strings.TrimSuffix(tf.Name, ".tmpl"), values)
					prFiles = append(prFiles, ghclient.PRFile{
						Path:    targetPath + outName,
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
				rendered, err := template.Render(content, values)
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

	// Confirm — show target path (dir for multi-file, full path for single-file)
	confirmDesc := fmt.Sprintf("→ %s/%s", resource.Resolved.Repo, prFiles[0].Path)
	if len(prFiles) > 1 {
		targetPath := template.RenderString(resource.Resolved.TargetPath, values)
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
				Title:         template.RenderString(resource.PRTitle, values),
				Body:          buildPRBody(resource.Name, values, comment),
				Files:         prFiles,
				Reviewers:     reviewers,
				TeamReviewers: teamReviewers,
			})
		}).
		Run()

	if prErr != nil {
		return fmt.Errorf("creating PR: %w", prErr)
	}

	fmt.Println(ui.Success("PR opened: " + prURL))
	return nil
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
		opts[i] = huh.NewOption(ui.PickerItem(r.Name, r.Description), r.Name)
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

func collectFields(resource config.Resource, repos []*config.RepoConfig, gh *ghclient.Client, ghWrite *ghclient.Client) (map[string]string, error) {
	values := make(map[string]string)

	for _, field := range resource.Fields {
		// Evaluate conditional — skip field and set to "" if condition is not met
		if field.When != "" && template.RenderString(field.When, values) != "true" {
			values[field.Name] = ""
			continue
		}

		ctx := buildFieldContext(field, resource, repos, gh, values)

		val, err := ui.PromptField(field, values, ctx)
		if err != nil {
			return nil, err
		}

		// Handle inline dependency creation
		if val == ui.CreateNewOption {
			resourceType := strings.TrimPrefix(field.Source, "resource.")
			depResource, found := remote.FindResource(resourceType, repos)
			if !found {
				return nil, fmt.Errorf("dependency resource type %q not found", resourceType)
			}

			fmt.Println(ui.Warning(fmt.Sprintf("Creating a new %s first...", resourceType)))
			depValues, err := collectFields(depResource, repos, gh, ghWrite)
			if err != nil {
				return nil, err
			}

			// Open the dependency PR inline
			var depPRURL string
			var depErr error
			_ = spinner.New().
				Title(fmt.Sprintf("Opening PR for new %s...", resourceType)).
				Action(func() {
					var depFiles []ghclient.PRFile
					if depResource.Resolved.TemplateDir != "" {
						tmplFiles, err := gh.FetchTemplateDir(depResource.Resolved.TemplateRepo, depResource.Resolved.TemplateDir, depResource.Resolved.TemplateRef)
						if err != nil {
							depErr = err
							return
						}
						targetPath := template.RenderString(depResource.Resolved.TargetPath, depValues)
						for _, tf := range tmplFiles {
							rendered, err := template.Render(tf.Content, depValues)
							if err != nil {
								depErr = fmt.Errorf("rendering %s: %w", tf.Name, err)
								return
							}
							depFiles = append(depFiles, ghclient.PRFile{
								Path:    targetPath + strings.TrimSuffix(tf.Name, ".tmpl"),
								Content: rendered,
							})
						}
					} else {
						tmplContent, err := gh.FetchFile(depResource.Resolved.TemplateRepo, depResource.Resolved.Template, depResource.Resolved.TemplateRef)
						if err != nil {
							depErr = err
							return
						}
						rendered, err := template.Render(tmplContent, depValues)
						if err != nil {
							depErr = err
							return
						}
						depFiles = []ghclient.PRFile{{
							Path:    resolveFilePath(depResource, depValues),
							Content: rendered,
						}}
					}
					depPRURL, depErr = ghWrite.CreatePR(ghclient.PRRequest{
						Repo:       depResource.Resolved.Repo,
						Branch:     fmt.Sprintf("platformr/%s-%s", depResource.Name, resolveSlug(depResource, depValues)),
						BaseBranch: depResource.Resolved.BaseBranch,
						Title:      template.RenderString(depResource.PRTitle, depValues),
						Body:       buildPRBody(depResource.Name, depValues, ""),
						Files:      depFiles,
					})
				}).
				Run()

			if depErr != nil {
				return nil, fmt.Errorf("creating dependency PR: %w", depErr)
			}

			fmt.Println(ui.Success(fmt.Sprintf("%s PR opened: %s", resourceType, depPRURL)))
			fmt.Println(ui.Warning(fmt.Sprintf("Merge that PR before this %s is ready.", resource.Name)))
			val = depValues["name"]
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
					exists, _ = gh.FileExists(resource.Resolved.Repo, candidatePath)
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

func buildFieldContext(field config.Field, resource config.Resource, repos []*config.RepoConfig, gh *ghclient.Client, values map[string]string) *ui.FieldContext {
	if field.Source == "" {
		return &ui.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) { return field.Options, nil },
		}
	}

	if strings.HasPrefix(field.Source, "dirs:") {
		dirPath := template.RenderString(strings.TrimPrefix(field.Source, "dirs:"), values)
		return &ui.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) {
				return gh.ListDirs(resource.Resolved.TemplateRepo, dirPath, resource.Resolved.TemplateRef)
			},
		}
	}

	if strings.HasPrefix(field.Source, "team:") {
		teamSlug := strings.TrimPrefix(field.Source, "team:")
		return &ui.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) {
				return gh.ListTeamMembers(resource.Resolved.Org, teamSlug)
			},
		}
	}

	if field.Source == "collaborators" {
		return &ui.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) {
				return gh.ListCollaborators(resource.Resolved.Repo)
			},
		}
	}

	if !strings.HasPrefix(field.Source, "resource.") {
		return &ui.FieldContext{
			ListFiles: func(_, _ string) ([]string, error) { return field.Options, nil },
		}
	}

	resourceType := strings.TrimPrefix(field.Source, "resource.")
	depResource, found := remote.FindResource(resourceType, repos)
	if !found {
		return nil
	}

	return &ui.FieldContext{
		ListFiles: func(_, _ string) ([]string, error) {
			return gh.ListFiles(depResource.Resolved.Repo, depResource.Resolved.TargetPath)
		},
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

// resolveSlug returns a short identifier for branch names.
func resolveSlug(resource config.Resource, values map[string]string) string {
	slug := resource.FileName
	if slug == "" {
		slug = "{{." + firstFieldName(resource) + "}}"
	}
	return template.RenderString(slug, values)
}

// firstFieldName returns the name of the first field defined on the resource.
func firstFieldName(resource config.Resource) string {
	if len(resource.Fields) > 0 {
		return resource.Fields[0].Name
	}
	return "name"
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func buildPRBody(resourceName string, values map[string]string, comment string) string {
	body := fmt.Sprintf("## %s request\n\nOpened via `platformr`\n\n### Details\n\n", resourceName)
	for k, v := range values {
		body += fmt.Sprintf("- **%s**: %s\n", k, v)
	}
	if comment != "" {
		body += fmt.Sprintf("\n### Notes\n\n%s\n", comment)
	}
	return body
}
