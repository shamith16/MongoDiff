package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shamith/mongodiff/pkg/diff"
	mongoclient "github.com/shamith/mongodiff/pkg/mongo"
	"github.com/shamith/mongodiff/pkg/output"
	syncer "github.com/shamith/mongodiff/pkg/sync"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Apply differences from source to target database",
	Long: `Run a diff between source and target, then apply the changes to the target
so it matches the source. Creates a backup before applying.

Use --dry-run to see what would be changed without applying anything.`,
	RunE: runSync,
}

var (
	syncSourceURI    string
	syncTargetURI    string
	syncDatabase     string
	syncInclude      string
	syncExclude      string
	syncTimeout      int
	syncDryRun       bool
	syncIgnoreFields string
)

func init() {
	syncCmd.Flags().StringVar(&syncSourceURI, "source", "", "Source MongoDB connection URI")
	syncCmd.Flags().StringVar(&syncTargetURI, "target", "", "Target MongoDB connection URI")
	syncCmd.Flags().StringVar(&syncDatabase, "db", "", "Database name to sync")
	syncCmd.Flags().StringVar(&syncInclude, "include", "", "Comma-separated list of collections to include")
	syncCmd.Flags().StringVar(&syncExclude, "exclude", "", "Comma-separated list of collections to exclude")
	syncCmd.Flags().IntVar(&syncTimeout, "timeout", 30, "Connection timeout in seconds")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "Show what would be changed without applying")
	syncCmd.Flags().StringVar(&syncIgnoreFields, "ignore-fields", "", "Comma-separated list of fields to ignore (e.g. __v,meta.modified)")

	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	// Resolve from env vars
	if syncSourceURI == "" {
		syncSourceURI = os.Getenv("MONGODIFF_SOURCE")
	}
	if syncTargetURI == "" {
		syncTargetURI = os.Getenv("MONGODIFF_TARGET")
	}

	if syncSourceURI == "" {
		return fmt.Errorf("--source is required (or set MONGODIFF_SOURCE environment variable)")
	}
	if syncTargetURI == "" {
		return fmt.Errorf("--target is required (or set MONGODIFF_TARGET environment variable)")
	}
	if syncDatabase == "" {
		return fmt.Errorf("--db is required")
	}

	timeoutDuration := time.Duration(syncTimeout) * time.Second
	ctx := context.Background()

	connectCtx, connectCancel := context.WithTimeout(ctx, timeoutDuration)
	defer connectCancel()

	source, err := mongoclient.Connect(connectCtx, syncSourceURI, timeoutDuration)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	defer source.Disconnect(context.Background())

	target, err := mongoclient.Connect(connectCtx, syncTargetURI, timeoutDuration)
	if err != nil {
		return fmt.Errorf("target: %w", err)
	}
	defer target.Disconnect(context.Background())

	if err := validateDatabase(ctx, source, "source", syncDatabase); err != nil {
		return err
	}
	if err := validateDatabase(ctx, target, "target", syncDatabase); err != nil {
		return err
	}

	// Build diff options
	opts := diff.Options{}
	if syncInclude != "" {
		opts.IncludeCollections = splitCSV(syncInclude)
	}
	if syncExclude != "" {
		opts.ExcludeCollections = splitCSV(syncExclude)
	}
	if syncIgnoreFields != "" {
		opts.IgnoreFields = splitCSV(syncIgnoreFields)
	}

	// Run the diff first
	differ := diff.New(source, target, opts)
	result, err := differ.Diff(ctx, syncDatabase)
	if err != nil {
		return fmt.Errorf("diff failed: %w", err)
	}

	result.Source = mongoclient.RedactURI(syncSourceURI)
	result.Target = mongoclient.RedactURI(syncTargetURI)

	// Check if there's anything to sync
	if result.Stats.DocumentsAdded == 0 && result.Stats.DocumentsRemoved == 0 &&
		result.Stats.DocumentsModified == 0 && result.Stats.CollectionsAdded == 0 &&
		result.Stats.CollectionsRemoved == 0 {
		fmt.Println("Databases are identical. Nothing to sync.")
		return nil
	}

	// Show the diff summary first
	tr := output.NewTerminalRenderer()
	tr.SummaryOnly = true
	tr.Render(os.Stdout, result)

	// Generate and display the sync plan
	s := syncer.New(source, target)
	plan := s.Plan(result)

	fmt.Printf("\n\033[1m━━━ Sync Plan ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m\n\n")
	for _, action := range plan.Actions {
		symbol := "  "
		color := "\033[0m"
		switch action.Action {
		case "create_collection", "insert":
			symbol = "+ "
			color = "\033[32m"
		case "drop_collection", "delete":
			symbol = "- "
			color = "\033[31m"
		case "replace":
			symbol = "~ "
			color = "\033[33m"
		}
		fmt.Printf("  %s%s%s: %s\033[0m\n", color, symbol, action.Collection, action.Details)
	}

	totalChanges := result.Stats.DocumentsAdded + result.Stats.DocumentsRemoved +
		result.Stats.DocumentsModified + result.Stats.CollectionsAdded + result.Stats.CollectionsRemoved
	fmt.Printf("\n  \033[1m%d total changes across %d collections\033[0m\n\n",
		totalChanges, countAffectedCollections(result))

	if syncDryRun {
		fmt.Println("Dry run complete. No changes applied.")
		return nil
	}

	// Confirmation prompt
	fmt.Printf("\033[33m⚠ This will modify the target database at %s\033[0m\n", mongoclient.RedactURI(syncTargetURI))
	fmt.Print("Proceed? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println("Sync cancelled.")
		return nil
	}

	// Create backup
	fmt.Print("Creating backup... ")
	backupPath, err := s.Backup(ctx, result)
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	fmt.Printf("saved to %s\n", backupPath)

	// Apply
	fmt.Println("Applying changes...")
	syncResult, err := s.Apply(ctx, result)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// Report results
	fmt.Printf("\n\033[1m━━━ Sync Complete ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m\n\n")
	if syncResult.CollectionsCreated > 0 {
		fmt.Printf("  \033[32m%d collections created\033[0m\n", syncResult.CollectionsCreated)
	}
	if syncResult.CollectionsDropped > 0 {
		fmt.Printf("  \033[31m%d collections dropped\033[0m\n", syncResult.CollectionsDropped)
	}
	if syncResult.DocumentsInserted > 0 {
		fmt.Printf("  \033[32m%d documents inserted\033[0m\n", syncResult.DocumentsInserted)
	}
	if syncResult.DocumentsReplaced > 0 {
		fmt.Printf("  \033[33m%d documents replaced\033[0m\n", syncResult.DocumentsReplaced)
	}
	if syncResult.DocumentsDeleted > 0 {
		fmt.Printf("  \033[31m%d documents deleted\033[0m\n", syncResult.DocumentsDeleted)
	}
	fmt.Printf("  Backup: %s\n", backupPath)

	if len(syncResult.Errors) > 0 {
		fmt.Printf("\n  \033[31mErrors (%d):\033[0m\n", len(syncResult.Errors))
		for _, e := range syncResult.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}

	fmt.Println()
	return nil
}

func countAffectedCollections(result *diff.DiffResult) int {
	count := 0
	for _, c := range result.Collections {
		if c.DiffType != "" && c.Error == "" {
			count++
		}
	}
	return count
}
