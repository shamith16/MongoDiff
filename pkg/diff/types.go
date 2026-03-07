package diff

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type DiffType string

const (
	Added    DiffType = "added"
	Removed  DiffType = "removed"
	Modified DiffType = "modified"
)

// DiffResult is the top-level result of a diff operation.
type DiffResult struct {
	Source      string
	Target      string
	Database    string
	Timestamp   time.Time
	Collections []CollectionDiff
	Stats       DiffStats
}

type DiffStats struct {
	CollectionsAdded   int
	CollectionsRemoved int
	CollectionsMatched int
	DocumentsAdded     int
	DocumentsRemoved   int
	DocumentsModified  int
	DocumentsIdentical int
}

// CollectionDiff represents the diff for a single collection.
type CollectionDiff struct {
	Name      string
	DiffType  DiffType
	Documents []DocumentDiff
	Stats     DiffStats
}

// DocumentDiff represents the diff for a single document.
type DocumentDiff struct {
	ID       interface{}
	DiffType DiffType
	Fields   []FieldDiff
	Source   bson.M
	Target   bson.M
}

// FieldDiff represents the diff for a single field within a document.
type FieldDiff struct {
	Path     string
	DiffType DiffType
	OldValue interface{}
	NewValue interface{}
}
