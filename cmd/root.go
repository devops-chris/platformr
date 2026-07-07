package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/remote"
	"github.com/spf13/cobra"
)

var localCfg *config.LocalConfig

var rootCmd = &cobra.Command{
	Short: "Developer self-service platform CLI",
}

func Execute() {
	// Use the actual binary name so orgs can distribute under any name
	// (e.g. pt-platform, devops) and all help text reflects it automatically.
	name := filepath.Base(os.Args[0])
	rootCmd.Use = name
	rootCmd.Long = name + ` is a configurable self-service CLI for developers to
request infrastructure and services via GitOps pull requests.

Run '` + name + ` connect <org>' to get started.`

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
}

// completeResourceNames is the ValidArgsFunction for commands that take a resource
// name argument. Returns resource names with descriptions for shell completion.
func completeResourceNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) >= 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if localCfg == nil || localCfg.ConnectedOrg == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	token := resolveReadToken()
	if token == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	loader := remote.New(token)
	_, repos, err := loader.LoadAll(localCfg.ConnectedOrg)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var completions []string
	for _, r := range remote.AllResources(repos) {
		completions = append(completions, r.Name+"\t"+r.Description)
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func initConfig() {
	var err error
	localCfg, err = config.LoadLocal()
	if err != nil {
		log.Fatal("Error loading local config", "err", err)
	}
}
