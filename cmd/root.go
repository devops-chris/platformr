package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/spf13/cobra"
)

var localCfg *config.LocalConfig

var rootCmd = &cobra.Command{
	Use:   "platformr",
	Short: "Developer self-service platform CLI",
	Long: `platformr is a configurable self-service CLI for developers to
request infrastructure and services via GitOps pull requests.

Run 'platformr connect <org>' to get started.`,
}

func Execute() {
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
