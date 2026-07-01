package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/devops-chris/platformr/internal/config"
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

func initConfig() {
	var err error
	localCfg, err = config.LoadLocal()
	if err != nil {
		log.Fatal("Error loading local config", "err", err)
	}
}
