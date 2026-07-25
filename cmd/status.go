package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	lgtable "github.com/charmbracelet/lipgloss/table"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of your requests",
	Long: `Checks the IaC repos in your connected org for pull requests you opened via
` + "`platformr request`" + ` and reports whether each is open, merged, or closed.`,
	Args: cobra.NoArgs,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	binaryName := filepath.Base(os.Args[0])
	if localCfg == nil || localCfg.ConnectedOrg == "" {
		fmt.Println(ui.Warning("Not connected. Run `" + binaryName + " connect <org>` first."))
		os.Exit(1)
	}

	token := resolveReadToken()
	loader := remote.New(token)
	gh := ghclient.New(token)

	var repos []*config.RepoConfig
	var currentUser string
	var loadErr error
	_ = spinner.New().
		Title("Loading resources...").
		Action(func() {
			_, repos, loadErr = loader.LoadAll(localCfg.ConnectedOrg)
			if loadErr != nil {
				return
			}
			currentUser, loadErr = gh.CurrentUser()
		}).
		Run()
	if loadErr != nil {
		return fmt.Errorf("loading resources: %w", loadErr)
	}

	// Collect the unique set of target repos requests are opened against.
	targetRepos := map[string]bool{}
	for _, r := range remote.AllResources(repos) {
		if r.Resolved.Repo != "" {
			targetRepos[r.Resolved.Repo] = true
		}
	}

	var mine []ghclient.PRStatus
	var fetchErr error
	_ = spinner.New().
		Title("Checking PR status...").
		Action(func() {
			for repo := range targetRepos {
				prs, err := gh.MyRequestPRs(repo, currentUser)
				if err != nil {
					fetchErr = err
					return
				}
				mine = append(mine, prs...)
			}
		}).
		Run()
	if fetchErr != nil {
		return fmt.Errorf("checking PR status: %w", fetchErr)
	}

	if len(mine) == 0 {
		fmt.Println(ui.Subtle("No requests found for " + currentUser + "."))
		return nil
	}

	sort.Slice(mine, func(i, j int) bool { return mine[i].CreatedAt.After(mine[j].CreatedAt) })

	return printStatusTable(currentUser, mine)
}

func printStatusTable(user string, prs []ghclient.PRStatus) error {
	fmt.Printf("\n  %s  %s\n\n", ui.SectionHeader("Your requests"), ui.Subtle(user))

	var rows [][]string
	for _, pr := range prs {
		rows = append(rows, []string{
			pr.Title,
			pr.Repo,
			fmt.Sprintf("#%d", pr.Number),
			statusLabel(pr.State),
			humanAge(pr.CreatedAt),
		})
	}

	t := lgtable.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.ColorPurple)).
		Headers("Request", "Repo", "PR", "Status", "Opened").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == lgtable.HeaderRow:
				return lipgloss.NewStyle().Bold(true).Foreground(ui.ColorPurple).Padding(0, 1)
			default:
				return lipgloss.NewStyle().Padding(0, 1)
			}
		})

	fmt.Println(lipgloss.NewStyle().MarginLeft(2).Render(t.Render()))
	fmt.Println()
	for _, pr := range prs {
		fmt.Printf("    %s %s\n", ui.Subtle(fmt.Sprintf("#%d:", pr.Number)), pr.URL)
	}
	fmt.Println()
	return nil
}

func statusLabel(state string) string {
	switch state {
	case "open":
		return ui.Warning("Open")
	case "merged":
		return ui.Success("Merged")
	default:
		return ui.Subtle("Closed")
	}
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
