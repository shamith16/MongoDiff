package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	mongoclient "github.com/shamith/mongodiff/pkg/mongo"
	syncer "github.com/shamith/mongodiff/pkg/sync"
)

var restoreCmd = &cobra.Command{
	Use:   "restore <backup-file>",
	Short: "Restore a database from a sync backup",
	Long: `Restore documents from a .mongodiff backup file into the target database.
Backups are created automatically before each sync operation.

Example:
  mongodiff restore .mongodiff/backups/2026-03-09T14-30-00Z.json --target "mongodb://localhost:27017" --db myapp`,
	Args: cobra.ExactArgs(1),
	RunE: runRestore,
}

var (
	restoreTargetURI string
	restoreDatabase  string
	restoreTimeout   int
)

func init() {
	restoreCmd.Flags().StringVar(&restoreTargetURI, "target", "", "Target MongoDB connection URI")
	restoreCmd.Flags().StringVar(&restoreDatabase, "db", "", "Database to restore into")
	restoreCmd.Flags().IntVar(&restoreTimeout, "timeout", 30, "Connection timeout in seconds")

	rootCmd.AddCommand(restoreCmd)
}

func runRestore(cmd *cobra.Command, args []string) error {
	backupPath := args[0]

	if restoreTargetURI == "" {
		restoreTargetURI = os.Getenv("MONGODIFF_TARGET")
	}
	if restoreTargetURI == "" {
		return fmt.Errorf("--target is required (or set MONGODIFF_TARGET environment variable)")
	}
	if restoreDatabase == "" {
		return fmt.Errorf("--db is required")
	}

	// Verify file exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupPath)
	}

	fmt.Printf("Restore from: %s\n", backupPath)
	fmt.Printf("Target:       %s\n", mongoclient.RedactURI(restoreTargetURI))
	fmt.Printf("Database:     %s\n\n", restoreDatabase)

	fmt.Printf("\033[33mThis will upsert documents from the backup into the target database.\033[0m\n")
	fmt.Print("Proceed? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println("Restore cancelled.")
		return nil
	}

	timeoutDuration := time.Duration(restoreTimeout) * time.Second
	ctx := context.Background()

	connectCtx, connectCancel := context.WithTimeout(ctx, timeoutDuration)
	defer connectCancel()

	target, err := mongoclient.Connect(connectCtx, restoreTargetURI, timeoutDuration)
	if err != nil {
		return fmt.Errorf("target: %w", err)
	}
	defer target.Disconnect(context.Background())

	s := syncer.New(nil, target)
	result, err := s.Restore(ctx, restoreDatabase, backupPath)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Printf("\n\033[1m━━━ Restore Complete ━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m\n\n")
	fmt.Printf("  %d collections affected\n", result.CollectionsAffected)
	fmt.Printf("  %d documents restored\n", result.DocumentsRestored)

	if len(result.Errors) > 0 {
		fmt.Printf("\n  \033[31mErrors (%d):\033[0m\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}

	fmt.Println()
	return nil
}
