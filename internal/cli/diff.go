package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shamith/mongodiff/pkg/diff"
	mongoclient "github.com/shamith/mongodiff/pkg/mongo"
	"github.com/shamith/mongodiff/pkg/output"
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compare two MongoDB databases and show differences",
	Long: `Connect to two MongoDB instances (source and target), compare them at the
collection and document level, and produce a color-coded diff showing
exactly what's different.`,
	RunE: runDiff,
}

var (
	sourceURI string
	targetURI string
	database  string
	include   string
	exclude   string
	timeout   int
)

func init() {
	diffCmd.Flags().StringVar(&sourceURI, "source", "", "Source MongoDB connection URI")
	diffCmd.Flags().StringVar(&targetURI, "target", "", "Target MongoDB connection URI")
	diffCmd.Flags().StringVar(&database, "db", "", "Database name to compare")
	diffCmd.Flags().StringVar(&include, "include", "", "Comma-separated list of collections to include")
	diffCmd.Flags().StringVar(&exclude, "exclude", "", "Comma-separated list of collections to exclude")
	diffCmd.Flags().IntVar(&timeout, "timeout", 30, "Connection timeout in seconds")

	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	// Resolve flags from environment variables if not set
	if sourceURI == "" {
		sourceURI = os.Getenv("MONGODIFF_SOURCE")
	}
	if targetURI == "" {
		targetURI = os.Getenv("MONGODIFF_TARGET")
	}

	// Validate required flags
	if sourceURI == "" {
		return fmt.Errorf("--source is required (or set MONGODIFF_SOURCE environment variable)")
	}
	if targetURI == "" {
		return fmt.Errorf("--target is required (or set MONGODIFF_TARGET environment variable)")
	}
	if database == "" {
		return fmt.Errorf("--db is required")
	}

	timeoutDuration := time.Duration(timeout) * time.Second
	ctx := context.Background()

	// Connect to source (timeout applies per-operation via Mongo driver's SetTimeout)
	connectCtx, connectCancel := context.WithTimeout(ctx, timeoutDuration)
	defer connectCancel()
	source, err := mongoclient.Connect(connectCtx, sourceURI, timeoutDuration)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	defer source.Disconnect(context.Background())

	// Connect to target
	target, err := mongoclient.Connect(connectCtx, targetURI, timeoutDuration)
	if err != nil {
		return fmt.Errorf("target: %w", err)
	}
	defer target.Disconnect(context.Background())

	// Check if database exists on both sides
	if err := validateDatabase(ctx, source, "source", database); err != nil {
		return err
	}
	if err := validateDatabase(ctx, target, "target", database); err != nil {
		return err
	}

	// Build diff options
	opts := diff.Options{}
	if include != "" {
		opts.IncludeCollections = splitCSV(include)
	}
	if exclude != "" {
		opts.ExcludeCollections = splitCSV(exclude)
	}

	// Run the diff
	differ := diff.New(source, target, opts)
	result, err := differ.Diff(ctx, database)
	if err != nil {
		return fmt.Errorf("diff failed: %w", err)
	}

	// Set display names
	result.Source = mongoclient.RedactURI(sourceURI)
	result.Target = mongoclient.RedactURI(targetURI)

	// Render output
	renderer := output.NewTerminalRenderer()
	return renderer.Render(os.Stdout, result)
}

func validateDatabase(ctx context.Context, client *mongoclient.Client, label, dbName string) error {
	databases, err := client.ListDatabases(ctx)
	if err != nil {
		return fmt.Errorf("failed to list databases on %s: %w", label, err)
	}

	for _, db := range databases {
		if db == dbName {
			return nil
		}
	}

	return fmt.Errorf("database %q not found on %s. Available databases: %s",
		dbName, label, strings.Join(databases, ", "))
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
