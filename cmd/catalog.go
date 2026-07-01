package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/devops-chris/platformr/internal/ui"
	"github.com/spf13/cobra"
)

var catalogJSON bool

var catalogCmd = &cobra.Command{
	Use:   "catalog [resource]",
	Short: "List available resources and their schemas",
	Long: `Show all resource types available in your connected org, or the detailed
schema for a specific resource type.

Examples:
  platformr catalog                # list all resources
  platformr catalog service        # schema for the service resource
  platformr catalog service --json # machine-readable JSON schema`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCatalog,
}

func init() {
	rootCmd.AddCommand(catalogCmd)
	catalogCmd.Flags().BoolVar(&catalogJSON, "json", false, "Output schema as JSON")
}

func runCatalog(cmd *cobra.Command, args []string) error {
	if localCfg == nil || localCfg.ConnectedOrg == "" {
		fmt.Println(ui.Warning("Not connected. Run `platformr connect <org>` first."))
		os.Exit(1)
	}

	token := resolveToken()
	loader := remote.New(token)

	var repos []*config.RepoConfig
	var loadErr error
	_ = spinner.New().
		Title("Loading catalog...").
		Action(func() {
			_, repos, loadErr = loader.LoadAll(localCfg.ConnectedOrg)
		}).
		Run()

	if loadErr != nil {
		return fmt.Errorf("loading catalog: %w", loadErr)
	}

	allResources := remote.AllResources(repos)

	// Single resource — show detailed schema
	if len(args) == 1 {
		return showResourceSchema(args[0], allResources, repos)
	}

	// No args — list all resources
	if catalogJSON {
		return printAllJSON(allResources)
	}
	return printResourceList(allResources, repos)
}

// ── List view ────────────────────────────────────────────────────────────────

func printResourceList(resources []config.Resource, repos []*config.RepoConfig) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"})
	nameStyle := lipgloss.NewStyle().Bold(true).Width(16)
	repoStyle := lipgloss.NewStyle().Faint(true)

	fmt.Printf("\n  %s — %s\n\n",
		headerStyle.Render("platformr catalog"),
		ui.Subtle(localCfg.ConnectedOrg),
	)

	currentRepo := ""
	for _, r := range resources {
		if r.Resolved.Repo != currentRepo {
			currentRepo = r.Resolved.Repo
			fmt.Printf("  %s\n", repoStyle.Render("→ "+currentRepo))
		}
		fmt.Printf("    %s %s\n",
			nameStyle.Render(r.Name),
			ui.Subtle(r.Description),
		)
	}

	fmt.Printf("\n  %s\n\n",
		ui.Subtle(fmt.Sprintf("Run `platformr catalog <name>` for field details.")),
	)
	return nil
}

// ── Schema view ──────────────────────────────────────────────────────────────

func showResourceSchema(name string, allResources []config.Resource, repos []*config.RepoConfig) error {
	var resource *config.Resource
	for i := range allResources {
		if allResources[i].Name == name {
			resource = &allResources[i]
			break
		}
	}
	if resource == nil {
		return fmt.Errorf("resource %q not found — run `platformr catalog` to see available resources", name)
	}

	if catalogJSON {
		return printSchemaJSON(resource)
	}
	return printSchemaHuman(resource)
}

func printSchemaHuman(r *config.Resource) error {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"})
	labelStyle := lipgloss.NewStyle().Faint(true).Width(14)
	fieldNameStyle := lipgloss.NewStyle().Bold(true).Width(16)
	typeStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}).Width(10)
	noteStyle := lipgloss.NewStyle().Faint(true)

	fmt.Printf("\n  %s  %s\n\n",
		titleStyle.Render(r.Name),
		ui.Subtle(r.Description),
	)

	fmt.Printf("  %s %s\n", labelStyle.Render("PR target:"), r.Resolved.Repo+"/"+r.Resolved.TargetPath)
	fmt.Printf("  %s %s\n", labelStyle.Render("Template:"), r.Resolved.TemplateRepo+"/"+r.Resolved.Template)
	fmt.Printf("  %s %s\n\n", labelStyle.Render("Base branch:"), r.Resolved.BaseBranch)

	fmt.Printf("  %s\n", lipgloss.NewStyle().Bold(true).Render("Fields"))
	fmt.Printf("  %s\n\n", strings.Repeat("─", 60))

	for _, f := range r.Fields {
		label := f.Label
		if label == "" {
			label = f.Name
		}
		notes := fieldNotes(f)
		fmt.Printf("  %s %s %s\n",
			fieldNameStyle.Render(f.Name),
			typeStyle.Render(f.Type),
			noteStyle.Render(notes),
		)
		if f.Label != "" {
			fmt.Printf("  %s %s\n", labelStyle.Render(""), ui.Subtle("label: "+label))
		}
		if len(f.Options) > 0 {
			fmt.Printf("  %s %s\n", labelStyle.Render(""), ui.Subtle("options: "+strings.Join(f.Options, ", ")))
		}
		if f.Source != "" {
			fmt.Printf("  %s %s\n", labelStyle.Render(""), ui.Subtle("source: "+f.Source))
		}
		if f.Default != "" {
			fmt.Printf("  %s %s\n", labelStyle.Render(""), ui.Subtle("default: "+f.Default))
		}
		fmt.Println()
	}

	return nil
}

func fieldNotes(f config.Field) string {
	var notes []string
	if f.Validate == "unique" {
		notes = append(notes, "must be unique")
	}
	if f.AllowCreate {
		notes = append(notes, "can create new")
	}
	if f.Source != "" {
		notes = append(notes, "dynamic")
	}
	if f.Default != "" {
		notes = append(notes, "has default")
	}
	if len(notes) == 0 {
		return ""
	}
	return "(" + strings.Join(notes, ", ") + ")"
}

// ── JSON output ───────────────────────────────────────────────────────────────

type catalogFieldJSON struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Label       string   `json:"label,omitempty"`
	Source      string   `json:"source,omitempty"`
	AllowCreate bool     `json:"allow_create,omitempty"`
	Options     []string `json:"options,omitempty"`
	Default     string   `json:"default,omitempty"`
	Validate    string   `json:"validate,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

type catalogResourceJSON struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Target      string             `json:"target"`
	Template    string             `json:"template"`
	BaseBranch  string             `json:"base_branch"`
	Fields      []catalogFieldJSON `json:"fields"`
}

func toSchemaJSON(r *config.Resource) catalogResourceJSON {
	fields := make([]catalogFieldJSON, len(r.Fields))
	for i, f := range r.Fields {
		fields[i] = catalogFieldJSON{
			Name:        f.Name,
			Type:        f.Type,
			Label:       f.Label,
			Source:      f.Source,
			AllowCreate: f.AllowCreate,
			Options:     f.Options,
			Default:     f.Default,
			Validate:    f.Validate,
			Placeholder: f.Placeholder,
		}
	}
	return catalogResourceJSON{
		Name:        r.Name,
		Description: r.Description,
		Target:      r.Resolved.Repo + "/" + r.Resolved.TargetPath,
		Template:    r.Resolved.TemplateRepo + "/" + r.Resolved.Template,
		BaseBranch:  r.Resolved.BaseBranch,
		Fields:      fields,
	}
}

func printSchemaJSON(r *config.Resource) error {
	out, err := json.MarshalIndent(toSchemaJSON(r), "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func printAllJSON(resources []config.Resource) error {
	schemas := make([]catalogResourceJSON, len(resources))
	for i := range resources {
		schemas[i] = toSchemaJSON(&resources[i])
	}
	out, err := json.MarshalIndent(schemas, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
