package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh/spinner"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/auth"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authorize the CLI to open pull requests on your behalf",
	Long: `Runs a GitHub App device flow to authorize platformr to create branches
and pull requests against your org's IaC repos.

You will be shown a short code to enter in your browser — no password or
personal token is required. The resulting token is stored locally and reused
on subsequent requests.

Requires platformr to be connected first: platformr connect <org>`,
	RunE: runAuth,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove the stored authorization token",
	RunE:  runAuthLogout,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authLogoutCmd)
}

func runAuth(cmd *cobra.Command, args []string) error {
	binaryName := filepath.Base(os.Args[0])
	if localCfg == nil || localCfg.ConnectedOrg == "" {
		fmt.Println(ui.Warning("Not connected. Run `" + binaryName + " connect <org>` first."))
		os.Exit(1)
	}

	// Fetch org config to get the App Client ID
	readToken := resolveReadToken()
	loader := remote.New(readToken)

	var clientID string
	var loadErr error
	_ = spinner.New().
		Title("Fetching org config...").
		Action(func() {
			orgCfg, _, err := loader.LoadAll(localCfg.ConnectedOrg)
			if err != nil {
				loadErr = err
				return
			}
			clientID = orgCfg.GitHub.AppClientID
		}).
		Run()

	if loadErr != nil {
		return fmt.Errorf("loading org config: %w", loadErr)
	}
	if clientID == "" {
		return fmt.Errorf("no app_client_id configured in %s/.platformr/config.toml\n\nAsk your platform team to create a GitHub App and add its Client ID to the org config.", localCfg.ConnectedOrg)
	}

	// Determine GitHub host (GHES support)
	host := ghHost()

	fmt.Println()

	// Run device flow — show code immediately, poll in background
	var token string
	var authErr error

	_ = spinner.New().
		Title("Waiting for authorization...").
		Action(func() {
			token, authErr = auth.DeviceFlow(clientID, host, func(result auth.DeviceFlowResult) {
				// This runs as soon as the code is available, before polling starts.
				// The spinner is running concurrently so we print above it.
				fmt.Printf("\n  Opening browser for authorization...\n  %s\n\n  Enter code: %s\n\n",
					result.VerificationURI, ui.Highlight(result.UserCode))
				// Best-effort browser open — ignore errors (headless envs, etc.)
				_ = openBrowser(result.VerificationURI)
			})
		}).
		Run()

	if authErr != nil {
		return authErr
	}

	if err := auth.SaveToken(token); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Println(ui.Success(binaryName + " can now open pull requests on your behalf."))
	return nil
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	if err := auth.ClearToken(); err != nil {
		if os.IsNotExist(err) {
			fmt.Println(ui.Warning("No stored token found."))
			return nil
		}
		return err
	}
	fmt.Println(ui.Success("Token removed."))
	return nil
}

// resolveReadToken returns a token suitable for reading — personal token or gh CLI.
// Does not include the stored app token since that's for writes.
func resolveReadToken() string {
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if t := os.Getenv(env); t != "" {
			return t
		}
	}
	out, err := execGH("auth", "token")
	if err == nil {
		return out
	}
	return ""
}

func ghHost() string {
	if h := os.Getenv("GH_HOST"); h != "" {
		return h
	}
	return ""
}
