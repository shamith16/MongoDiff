package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shamith/mongodiff/pkg/diff"
	mongoclient "github.com/shamith/mongodiff/pkg/mongo"
)

// SyncResult summarizes what was applied.
type SyncResult struct {
	CollectionsCreated int
	CollectionsDropped int
	DocumentsInserted  int
	DocumentsReplaced  int
	DocumentsDeleted   int
	BackupPath         string
	Errors             []string
}

// SyncPlan describes what sync will do without actually doing it.
type SyncPlan struct {
	Actions []SyncAction
}

// SyncAction describes a single sync operation.
type SyncAction struct {
	Collection string
	Action     string // "create_collection", "drop_collection", "insert", "replace", "delete"
	Count      int
	Details    string
}

// Syncer applies a DiffResult to the target database.
type Syncer struct {
	target *mongoclient.Client
	source *mongoclient.Client
}

// New creates a new Syncer.
func New(source, target *mongoclient.Client) *Syncer {
	return &Syncer{source: source, target: target}
}

// Plan generates a sync plan from a diff result without applying anything.
func (s *Syncer) Plan(result *diff.DiffResult) *SyncPlan {
	plan := &SyncPlan{}

	for _, coll := range result.Collections {
		switch coll.DiffType {
		case diff.Added:
			plan.Actions = append(plan.Actions, SyncAction{
				Collection: coll.Name,
				Action:     "create_collection",
				Count:      1,
				Details:    fmt.Sprintf("Create collection and insert %d documents", coll.Stats.DocumentsAdded),
			})
			if coll.Stats.DocumentsAdded > 0 {
				plan.Actions = append(plan.Actions, SyncAction{
					Collection: coll.Name,
					Action:     "insert",
					Count:      coll.Stats.DocumentsAdded,
					Details:    fmt.Sprintf("Insert %d documents", coll.Stats.DocumentsAdded),
				})
			}
		case diff.Removed:
			plan.Actions = append(plan.Actions, SyncAction{
				Collection: coll.Name,
				Action:     "drop_collection",
				Count:      1,
				Details:    fmt.Sprintf("Drop collection (%d documents)", coll.Stats.DocumentsRemoved),
			})
		case diff.Modified:
			for _, doc := range coll.Documents {
				switch doc.DiffType {
				case diff.Added:
					plan.Actions = append(plan.Actions, SyncAction{
						Collection: coll.Name,
						Action:     "insert",
						Count:      1,
						Details:    fmt.Sprintf("Insert document %v", diff.FormatValue(doc.ID)),
					})
				case diff.Removed:
					plan.Actions = append(plan.Actions, SyncAction{
						Collection: coll.Name,
						Action:     "delete",
						Count:      1,
						Details:    fmt.Sprintf("Delete document %v", diff.FormatValue(doc.ID)),
					})
				case diff.Modified:
					plan.Actions = append(plan.Actions, SyncAction{
						Collection: coll.Name,
						Action:     "replace",
						Count:      1,
						Details:    fmt.Sprintf("Replace document %v (%d field changes)", diff.FormatValue(doc.ID), len(doc.Fields)),
					})
				}
			}
		}
	}

	return plan
}

// Backup saves the current state of affected collections to a JSON file.
func (s *Syncer) Backup(ctx context.Context, result *diff.DiffResult) (string, error) {
	backupDir := ".mongodiff/backups"
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02T15-04-05Z")
	backupFile := filepath.Join(backupDir, timestamp+".json")

	backup := make(map[string][]bson.M)

	for _, coll := range result.Collections {
		if coll.DiffType == "" || coll.Error != "" {
			continue
		}

		// Backup the target collection's current state for affected documents
		switch coll.DiffType {
		case diff.Removed:
			// Collection exists only in target — back it all up before dropping
			docs, err := s.target.FetchAllDocuments(ctx, result.Database, coll.Name)
			if err != nil {
				return "", fmt.Errorf("backup failed for collection %s: %w", coll.Name, err)
			}
			docList := make([]bson.M, 0, len(docs))
			for _, doc := range docs {
				docList = append(docList, doc)
			}
			backup[coll.Name] = docList

		case diff.Modified:
			// Back up affected documents from target
			var ids []interface{}
			for _, doc := range coll.Documents {
				if doc.DiffType == diff.Removed || doc.DiffType == diff.Modified {
					ids = append(ids, doc.ID)
				}
			}
			if len(ids) > 0 {
				docs, err := s.target.FetchDocuments(ctx, result.Database, coll.Name, ids)
				if err != nil {
					return "", fmt.Errorf("backup failed for collection %s: %w", coll.Name, err)
				}
				docList := make([]bson.M, 0, len(docs))
				for _, doc := range docs {
					docList = append(docList, doc)
				}
				backup[coll.Name] = docList
			}
		}
	}

	file, err := os.Create(backupFile)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(backup); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	return backupFile, nil
}

// Apply applies the diff result to the target database.
func (s *Syncer) Apply(ctx context.Context, result *diff.DiffResult) (*SyncResult, error) {
	sr := &SyncResult{}

	for _, coll := range result.Collections {
		switch coll.DiffType {
		case diff.Added:
			if err := s.applyAddedCollection(ctx, result.Database, coll); err != nil {
				sr.Errors = append(sr.Errors, fmt.Sprintf("collection %s: %v", coll.Name, err))
				continue
			}
			sr.CollectionsCreated++
			sr.DocumentsInserted += coll.Stats.DocumentsAdded

		case diff.Removed:
			if err := s.target.DropCollection(ctx, result.Database, coll.Name); err != nil {
				sr.Errors = append(sr.Errors, fmt.Sprintf("drop collection %s: %v", coll.Name, err))
				continue
			}
			sr.CollectionsDropped++

		case diff.Modified:
			if err := s.applyModifiedCollection(ctx, result.Database, coll, sr); err != nil {
				sr.Errors = append(sr.Errors, fmt.Sprintf("collection %s: %v", coll.Name, err))
			}
		}
	}

	return sr, nil
}

func (s *Syncer) applyAddedCollection(ctx context.Context, database string, coll diff.CollectionDiff) error {
	if err := s.target.CreateCollection(ctx, database, coll.Name); err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	if len(coll.Documents) > 0 {
		docs := make([]bson.M, 0, len(coll.Documents))
		for _, doc := range coll.Documents {
			if doc.Source != nil {
				docs = append(docs, doc.Source)
			}
		}
		if err := s.target.InsertDocuments(ctx, database, coll.Name, docs); err != nil {
			return fmt.Errorf("insert documents: %w", err)
		}
	}

	return nil
}

func (s *Syncer) applyModifiedCollection(ctx context.Context, database string, coll diff.CollectionDiff, sr *SyncResult) error {
	// Batch inserts
	var insertDocs []bson.M
	var deleteIDs []interface{}

	for _, doc := range coll.Documents {
		switch doc.DiffType {
		case diff.Added:
			if doc.Source != nil {
				insertDocs = append(insertDocs, doc.Source)
			}
		case diff.Removed:
			deleteIDs = append(deleteIDs, doc.ID)
		case diff.Modified:
			if doc.Source != nil {
				if err := s.target.ReplaceDocument(ctx, database, coll.Name, doc.ID, doc.Source); err != nil {
					return fmt.Errorf("replace document %v: %w", doc.ID, err)
				}
				sr.DocumentsReplaced++
			}
		}
	}

	if len(insertDocs) > 0 {
		if err := s.target.InsertDocuments(ctx, database, coll.Name, insertDocs); err != nil {
			return fmt.Errorf("insert documents: %w", err)
		}
		sr.DocumentsInserted += len(insertDocs)
	}

	if len(deleteIDs) > 0 {
		if err := s.target.DeleteDocuments(ctx, database, coll.Name, deleteIDs); err != nil {
			return fmt.Errorf("delete documents: %w", err)
		}
		sr.DocumentsDeleted += len(deleteIDs)
	}

	return nil
}
