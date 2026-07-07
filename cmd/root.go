package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/ui"
	"github.com/spf13/cobra"
)

var localCfg *config.LocalConfig

var rootCmd = &cobra.Command{
	Short: "Developer self-service platform CLI",
}

// banner returns the app banner using cached org branding when available,
// falling back to the binary name and a default description.
func banner() string {
	name := filepath.Base(os.Args[0])
	description := "developer self-service platform CLI"
	if localCfg != nil {
		if localCfg.Branding.Name != "" {
			name = localCfg.Branding.Name
		}
		if localCfg.Branding.Description != "" {
			description = localCfg.Branding.Description
		}
	}
	return ui.Banner(name, description)
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

func initConfig() {
	var err error
	localCfg, err = config.LoadLocal()
	if err != nil {
		log.Fatal("Error loading local config", "err", err)
	}
}
