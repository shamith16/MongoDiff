package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mongodiff",
	Short: "A CLI tool to diff MongoDB databases",
	Long:  "mongodiff connects to two MongoDB instances and shows what's different between them.",
}

func Execute() error {
	return rootCmd.Execute()
}
