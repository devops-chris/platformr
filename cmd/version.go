package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("platformr %s (%s) built %s\n", Version, Commit, BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
