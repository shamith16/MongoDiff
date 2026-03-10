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

// SyncOperation identifies a specific document operation selected by the user.
type SyncOperation struct {
	Collection string      `json:"collection"`
	DocID      interface{} `json:"docId"`
	Type       string      `json:"type"` // "insert", "modify", "delete"
}

// canonicalID converts a document ID to a string for cross-type comparison.
// BSON ObjectIDs and their JSON-decoded hex strings both produce the same output.
func canonicalID(id interface{}) string {
	switch v := id.(type) {
	case bson.ObjectID:
		return v.Hex()
	case string:
		// Handle ObjectId("hex") format from JSON renderer
		if len(v) == 36 && v[:10] == "ObjectId(\"" && v[35] == ')' {
			return v[10:34]
		}
		// Strip surrounding quotes added by FormatValue for string IDs
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			return v[1 : len(v)-1]
		}
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// opDiffType maps frontend operation type strings to diff.DiffType.
func opDiffType(opType string) diff.DiffType {
	switch opType {
	case "insert":
		return diff.Added
	case "delete":
		return diff.Removed
	case "modify":
		return diff.Modified
	default:
		return ""
	}
}

// FilterResult returns a copy of result containing only the specified operations.
// Filtered collections use DiffType=Modified so Apply processes documents individually
// rather than creating/dropping entire collections.
func FilterResult(result *diff.DiffResult, ops []SyncOperation) *diff.DiffResult {
	type opKey struct {
		collection string
		docID      string
		diffType   diff.DiffType
	}
	selected := make(map[opKey]bool, len(ops))
	for _, op := range ops {
		selected[opKey{
			collection: op.Collection,
			docID:      canonicalID(op.DocID),
			diffType:   opDiffType(op.Type),
		}] = true
	}

	filtered := &diff.DiffResult{
		Database: result.Database,
		Source:   result.Source,
		Target:   result.Target,
	}

	for _, coll := range result.Collections {
		var filteredDocs []diff.DocumentDiff
		for _, doc := range coll.Documents {
			key := opKey{
				collection: coll.Name,
				docID:      canonicalID(doc.ID),
				diffType:   doc.DiffType,
			}
			if selected[key] {
				filteredDocs = append(filteredDocs, doc)
			}
		}
		if len(filteredDocs) == 0 {
			continue
		}

		filteredColl := coll
		filteredColl.Documents = filteredDocs
		// Always use Modified so Apply handles documents individually
		// instead of creating/dropping entire collections.
		filteredColl.DiffType = diff.Modified
		filteredColl.Stats = diff.DiffStats{}
		for _, doc := range filteredDocs {
			switch doc.DiffType {
			case diff.Added:
				filteredColl.Stats.DocumentsAdded++
			case diff.Removed:
				filteredColl.Stats.DocumentsRemoved++
			case diff.Modified:
				filteredColl.Stats.DocumentsModified++
			}
		}
		filtered.Collections = append(filtered.Collections, filteredColl)
	}

	return filtered
}

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

// Restore reads a backup file and upserts documents back into the target database.
func (s *Syncer) Restore(ctx context.Context, database, backupPath string) (*RestoreResult, error) {
	file, err := os.Open(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open backup file: %w", err)
	}
	defer file.Close()

	var backup map[string][]bson.M
	if err := json.NewDecoder(file).Decode(&backup); err != nil {
		return nil, fmt.Errorf("failed to parse backup file: %w", err)
	}

	result := &RestoreResult{}
	for collName, docs := range backup {
		// Ensure collection exists
		_ = s.target.CreateCollection(ctx, database, collName)

		for _, doc := range docs {
			if err := s.target.UpsertDocument(ctx, database, collName, doc); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", collName, err))
				continue
			}
			result.DocumentsRestored++
		}
		result.CollectionsAffected++
	}

	return result, nil
}

// RestoreResult summarizes the restore operation.
type RestoreResult struct {
	CollectionsAffected int
	DocumentsRestored   int
	Errors              []string
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
