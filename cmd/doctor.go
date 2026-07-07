package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh/spinner"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/auth"
	"github.com/devops-chris/platformr/internal/config"
	ghclient "github.com/devops-chris/platformr/internal/github"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check configuration, connectivity, and auth",
	Long: `Run a series of health checks to diagnose issues with your platformr setup.

Checks local config, GitHub auth tokens, org config accessibility,
and each registered IaC repo — with hints on how to fix any failures.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
	pass := func(msg string) { fmt.Println(ui.CheckPass(msg)) }
	fail := func(msg, hint string) { fmt.Println(ui.CheckFail(msg, hint)) }
	warn := func(msg, hint string) { fmt.Println(ui.CheckWarn(msg, hint)) }
	section := func(name string) { fmt.Print(ui.CheckSection(name)) }

	binaryName := filepath.Base(os.Args[0])

	org := ""
	if localCfg != nil {
		org = localCfg.ConnectedOrg
	}
	fmt.Printf("\n  %s  %s\n", ui.SectionHeader(binaryName+" doctor"), ui.Subtle(org))

	// ── Local config ─────────────────────────────────────────────────────────

	section("Local config")

	if localCfg == nil || localCfg.ConnectedOrg == "" {
		fail("Not connected to any org", "Run `"+binaryName+" connect <org>` to get started.")
		fmt.Println()
		return nil
	}
	pass(fmt.Sprintf("Connected org: %s", localCfg.ConnectedOrg))

	// ── Auth ─────────────────────────────────────────────────────────────────

	section("Auth")

	readToken := resolveReadToken()
	if readToken == "" {
		fail("No read token found", "Set GITHUB_TOKEN or GH_TOKEN, or run `gh auth login`.")
	} else {
		src := "gh CLI"
		if os.Getenv("GITHUB_TOKEN") != "" {
			src = "GITHUB_TOKEN"
		} else if os.Getenv("GH_TOKEN") != "" {
			src = "GH_TOKEN"
		}
		pass(fmt.Sprintf("Read token: %s", src))
	}

	if appToken := auth.LoadToken(); appToken == "" {
		warn("No app token stored",
			"PRs will be opened under your personal token.\n      Run `"+binaryName+" auth` to authorize the GitHub App instead.")
	} else {
		pass("Write token: GitHub App (keychain)")
	}

	if readToken == "" {
		fmt.Println()
		return nil
	}

	// ── Org config ───────────────────────────────────────────────────────────

	section("Org config")

	gh := ghclient.New(readToken)
	loader := remote.New(readToken)

	var orgCfg *config.OrgConfig
	var repos []*config.RepoConfig
	var loadErr error

	_ = spinner.New().
		Title("Checking org config...").
		Action(func() {
			orgCfg, repos, loadErr = loader.LoadAll(localCfg.ConnectedOrg)
		}).
		Run()

	if loadErr != nil {
		hint := fmt.Sprintf("Error: %v", loadErr)
		if strings.Contains(loadErr.Error(), "404") || strings.Contains(loadErr.Error(), "Not Found") {
			hint = "GitHub returned 404 — on internal/private repos this usually means the wrong account is active.\n" +
				"      Check which account `gh auth status` shows as active and switch if needed:\n" +
				"      gh auth switch --user <username>"
		} else if strings.Contains(loadErr.Error(), "401") || strings.Contains(loadErr.Error(), "Bad credentials") {
			hint = "Token rejected (401). Run `gh auth login` or set GITHUB_TOKEN to a valid token."
		}
		fail(
			fmt.Sprintf("Cannot access %s/.platformr/config.toml", localCfg.ConnectedOrg),
			hint,
		)
		fmt.Println()
		return nil
	}

	pass(fmt.Sprintf("%s/.platformr — %d repo(s) registered", localCfg.ConnectedOrg, len(orgCfg.Repos)))

	if orgCfg.GitHub.AppClientID == "" {
		warn("No app_client_id in org config",
			"Add app_client_id to [github] in .platformr/config.toml to enable `"+binaryName+" auth`.")
	} else {
		pass(fmt.Sprintf("GitHub App client ID: %s", orgCfg.GitHub.AppClientID))
	}

	// ── IaC repos ────────────────────────────────────────────────────────────

	section("IaC repos")

	if len(orgCfg.Repos) == 0 {
		warn("No repos registered", "Add [[repos]] entries to your .platformr/config.toml.")
		fmt.Println()
		return nil
	}

	// Index repos that loaded successfully
	loaded := map[string]*config.RepoConfig{}
	for _, r := range repos {
		loaded[r.RepoName] = r
	}

	for _, ref := range orgCfg.Repos {
		repoURL := remote.ResolveRepoURL(ref.URL, orgCfg.GitHub.DefaultOrg)
		refLabel := ref.Ref
		if refLabel == "" {
			refLabel = "default branch"
		}
		label := fmt.Sprintf("%s (ref: %s)", repoURL, refLabel)

		if repo, ok := loaded[repoURL]; ok {
			count := len(repo.Resources)
			noun := "resources"
			if count == 1 {
				noun = "resource"
			}
			pass(fmt.Sprintf("%s — %d %s", label, count, noun))
			if count == 0 {
				warn(fmt.Sprintf("%s has no resources defined", repoURL),
					"Add at least one [[resources]] entry to its platformr.toml.")
			}
		} else {
			// Repo failed to load — probe to give a specific hint
			_, err := gh.FetchFile(repoURL, "platformr.toml", ref.Ref)
			if err != nil {
				hint := fmt.Sprintf("Error: %v", err)
				if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
					if ref.Ref != "" {
						hint = fmt.Sprintf("platformr.toml not found on ref %q.\nCheck that the branch exists and has been pushed.", ref.Ref)
					} else {
						hint = "platformr.toml not found in the repo root."
					}
				}
				fail(label, hint)
			} else {
				fail(label, "platformr.toml found but failed to parse — check for TOML syntax errors.")
			}
		}
	}

	fmt.Println()
	return nil
}
