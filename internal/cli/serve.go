package cli

import (
	"github.com/spf13/cobra"

	"github.com/shamith/mongodiff/internal/server"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the mongodiff web server",
	Long: `Start an HTTP server with an embedded Web UI for running diffs
and syncs from the browser.`,
	RunE: runServe,
}

var servePort int

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	s := server.New(servePort)
	return s.Start()
}
