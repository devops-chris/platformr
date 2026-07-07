package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	lgtable "github.com/charmbracelet/lipgloss/table"
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

	token := resolveReadToken()
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
	return printResourceList(allResources)
}

// ── List view ────────────────────────────────────────────────────────────────

func printResourceList(resources []config.Resource) error {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"})

	fmt.Printf("\n  %s  %s\n\n",
		titleStyle.Render("platformr catalog"),
		ui.Subtle(localCfg.ConnectedOrg),
	)

	// Only show category headers if at least one resource has a category set.
	hasCategories := false
	for _, r := range resources {
		if r.Category != "" {
			hasCategories = true
			break
		}
	}

	// Sort by category so same-category resources from multiple repos are adjacent.
	sorted := make([]config.Resource, len(resources))
	copy(sorted, resources)
	if hasCategories {
		sort.SliceStable(sorted, func(i, j int) bool {
			ci, cj := sorted[i].Category, sorted[j].Category
			if ci == "" {
				ci = "General"
			}
			if cj == "" {
				cj = "General"
			}
			return ci < cj
		})
	}

	currentCategory := ""
	for _, r := range sorted {
		if hasCategories {
			cat := r.Category
			if cat == "" {
				cat = "General"
			}
			if cat != currentCategory {
				if currentCategory != "" {
					fmt.Println()
				}
				currentCategory = cat
				fmt.Printf("%s\n", ui.PickerCategory(cat))
			}
		}
		label := r.Label()
		if r.DisplayName != "" {
			label += "  " + ui.Subtle("("+r.Name+")")
		}
		fmt.Printf("%s\n", ui.PickerItem(label, r.Description))
	}

	binaryName := filepath.Base(os.Args[0])
	fmt.Printf("\n  %s\n\n",
		ui.Subtle("Run `"+binaryName+" catalog <name>` for field details."),
	)
	return nil
}

// ── Schema view ──────────────────────────────────────────────────────────────

func showResourceSchema(name string, allResources []config.Resource, repos []*config.RepoConfig) error {
	nameLower := strings.ToLower(name)
	var resource *config.Resource
	for i := range allResources {
		r := &allResources[i]
		if r.Name == name || strings.ToLower(r.Name) == nameLower || strings.ToLower(r.DisplayName) == nameLower {
			resource = r
			break
		}
	}
	binaryName := filepath.Base(os.Args[0])
	if resource == nil {
		return fmt.Errorf("resource %q not found — run `%s catalog` to see available resources", name, binaryName)
	}

	if catalogJSON {
		return printSchemaJSON(resource)
	}
	return printSchemaHuman(resource)
}

func printSchemaHuman(r *config.Resource) error {
	purple := lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	green := lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}
	muted := lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#5C5C5C"}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(purple)
	labelStyle := lipgloss.NewStyle().Faint(true).Width(14)

	title := r.Label()
	if r.DisplayName != "" {
		title += "  " + ui.Subtle("("+r.Name+")")
	}
	fmt.Printf("\n  %s  %s\n\n",
		titleStyle.Render(title),
		ui.Subtle(r.Description),
	)

	fmt.Printf("  %s %s\n", labelStyle.Render("PR target:"), r.Resolved.Repo+"/"+r.Resolved.TargetPath)
	fmt.Printf("  %s %s\n", labelStyle.Render("Template:"), r.Resolved.TemplateRepo+"/"+r.Resolved.Template)
	fmt.Printf("  %s %s\n\n", labelStyle.Render("Base branch:"), r.Resolved.BaseBranch)

	if len(r.Fields) == 0 {
		fmt.Printf("  %s\n\n", ui.Subtle("No fields defined."))
		return nil
	}

	// Build table rows
	var rows [][]string
	for _, f := range r.Fields {
		sourceOrOptions := f.Source
		if sourceOrOptions == "" && len(f.Options) > 0 {
			sourceOrOptions = strings.Join(f.Options, ", ")
			if len(sourceOrOptions) > 40 {
				sourceOrOptions = sourceOrOptions[:37] + "..."
			}
		}
		rows = append(rows, []string{
			f.Name,
			f.Type,
			sourceOrOptions,
			fieldFlags(f),
		})
	}

	t := lgtable.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(purple)).
		Headers("Field", "Type", "Source / Options", "Flags").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == lgtable.HeaderRow:
				return lipgloss.NewStyle().Bold(true).Foreground(purple).Padding(0, 1)
			case col == 0:
				return lipgloss.NewStyle().Bold(true).Foreground(green).Padding(0, 1)
			case col == 3:
				return lipgloss.NewStyle().Foreground(muted).Padding(0, 1)
			default:
				return lipgloss.NewStyle().Padding(0, 1)
			}
		})

	fmt.Println(lipgloss.NewStyle().MarginLeft(2).Render(t.Render()))
	fmt.Println()
	return nil
}

func fieldFlags(f config.Field) string {
	var flags []string
	if f.Validate == "unique" {
		flags = append(flags, "unique")
	}
	if f.Optional {
		flags = append(flags, "optional")
	}
	if f.AllowCreate {
		flags = append(flags, "allow create")
	}
	if f.Default != "" {
		flags = append(flags, "default: "+f.Default)
	}
	if f.When != "" {
		flags = append(flags, "conditional")
	}
	return strings.Join(flags, ", ")
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
	Placeholder  string   `json:"placeholder,omitempty"`
	FilterPrefix string   `json:"filter_prefix,omitempty"`
	StripPrefix  string   `json:"strip_prefix,omitempty"`
	When         string   `json:"when,omitempty"`
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
