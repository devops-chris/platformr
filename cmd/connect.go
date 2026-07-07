package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/log"
	"github.com/devops-chris/platformr/internal/auth"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/devops-chris/platformr/internal/ui"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect <org>",
	Short: "Connect to a GitHub org",
	Long: `Connect to a GitHub org by reading from github.com/<org>/.platformr/config.toml.

The org must have a .platformr repository containing a config.toml that lists
the IaC repos platformr should discover resources from.

Example:
  platformr connect acme-corp`,
	Args: cobra.ExactArgs(1),
	RunE: runConnect,
}

func init() {
	rootCmd.AddCommand(connectCmd)
}

func runConnect(cmd *cobra.Command, args []string) error {
	binaryName := filepath.Base(os.Args[0])

	org := args[0]
	token := resolveToken()
	if token == "" {
		log.Fatal("No GitHub token found. Set GITHUB_TOKEN, GH_TOKEN, or run `gh auth login`.")
	}

	loader := remote.New(token)

	var orgCfg *config.OrgConfig
	var repos []*config.RepoConfig
	var loadErr error
	_ = spinner.New().
		Title(fmt.Sprintf("Connecting to %s...", org)).
		Action(func() {
			orgCfg, repos, loadErr = loader.LoadAll(org)
		}).
		Run()

	if loadErr != nil {
		return fmt.Errorf("could not connect to %s: %w\n\nEnsure %s/.platformr/config.toml exists and your token has access.", org, loadErr, org)
	}

	// Resolve branding: org config wins; fall back to binary name + default description.
	branding := orgCfg.Branding
	if branding.Name == "" {
		branding.Name = binaryName
	}
	if branding.Description == "" {
		branding.Description = "developer self-service platform CLI"
	}

	fmt.Println(ui.Banner(branding.Name, branding.Description))

	all := remote.AllResources(repos)

	if err := config.SaveLocal(&config.LocalConfig{
		ConnectedOrg: org,
		Branding:     branding,
	}); err != nil {
		return fmt.Errorf("saving local config: %w", err)
	}

	fmt.Println(ui.Success(fmt.Sprintf("Connected to %s", org)))

	if len(all) > 0 {
		fmt.Printf("\n  %d resource type(s) available across %d repo(s):\n\n", len(all), len(repos))
		for _, r := range all {
			fmt.Printf("    %-16s %s\n", r.Name, ui.Subtle(r.Description))
		}
		fmt.Println()
		fmt.Printf("  Run %s to get started.\n\n", ui.Subtle("`"+binaryName+" request`"))
	} else {
		fmt.Printf("\n  No resources found yet — add IaC repos to %s/.platformr/config.toml\n\n", org)
		fmt.Printf("  Then run %s to authorize PR creation.\n\n", ui.Subtle("`"+binaryName+" auth`"))
	}

	return nil
}

// resolveToken returns the best available token for read + write operations.
// Precedence: stored app token → GITHUB_TOKEN → GH_TOKEN → gh auth token
func resolveToken() string {
	if t := auth.LoadToken(); t != "" {
		return t
	}
	return resolveReadToken()
}

// execGH runs a gh CLI subcommand and returns trimmed stdout.
func execGH(args ...string) (string, error) {
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// openBrowser opens a URL in the default browser cross-platform.
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	return exec.Command(cmd, args...).Start()
}
