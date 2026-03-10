package sync

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shamith/mongodiff/pkg/diff"
)

func TestPlan_AddedCollection(t *testing.T) {
	s := New(nil, nil)
	result := &diff.DiffResult{
		Collections: []diff.CollectionDiff{
			{
				Name:     "new_collection",
				DiffType: diff.Added,
				Documents: []diff.DocumentDiff{
					{ID: "doc1", DiffType: diff.Added, Source: bson.M{"_id": "doc1"}},
					{ID: "doc2", DiffType: diff.Added, Source: bson.M{"_id": "doc2"}},
				},
				Stats: diff.DiffStats{DocumentsAdded: 2},
			},
		},
	}

	plan := s.Plan(result)
	if len(plan.Actions) != 2 {
		t.Fatalf("expected 2 actions (create + insert), got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "create_collection" {
		t.Errorf("expected create_collection, got %s", plan.Actions[0].Action)
	}
	if plan.Actions[1].Action != "insert" {
		t.Errorf("expected insert, got %s", plan.Actions[1].Action)
	}
	if plan.Actions[1].Count != 2 {
		t.Errorf("expected count=2, got %d", plan.Actions[1].Count)
	}
}

func TestPlan_RemovedCollection(t *testing.T) {
	s := New(nil, nil)
	result := &diff.DiffResult{
		Collections: []diff.CollectionDiff{
			{
				Name:     "old_collection",
				DiffType: diff.Removed,
				Stats:    diff.DiffStats{DocumentsRemoved: 5},
			},
		},
	}

	plan := s.Plan(result)
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "drop_collection" {
		t.Errorf("expected drop_collection, got %s", plan.Actions[0].Action)
	}
}

func TestPlan_ModifiedCollection(t *testing.T) {
	s := New(nil, nil)
	result := &diff.DiffResult{
		Collections: []diff.CollectionDiff{
			{
				Name:     "users",
				DiffType: diff.Modified,
				Documents: []diff.DocumentDiff{
					{ID: "doc1", DiffType: diff.Added, Source: bson.M{"_id": "doc1"}},
					{ID: "doc2", DiffType: diff.Removed, Target: bson.M{"_id": "doc2"}},
					{ID: "doc3", DiffType: diff.Modified, Source: bson.M{"_id": "doc3"}, Target: bson.M{"_id": "doc3"}},
				},
			},
		},
	}

	plan := s.Plan(result)
	if len(plan.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(plan.Actions))
	}

	actions := map[string]bool{}
	for _, a := range plan.Actions {
		actions[a.Action] = true
	}
	if !actions["insert"] {
		t.Error("expected insert action")
	}
	if !actions["delete"] {
		t.Error("expected delete action")
	}
	if !actions["replace"] {
		t.Error("expected replace action")
	}
}

func TestPlan_IdenticalCollection(t *testing.T) {
	s := New(nil, nil)
	result := &diff.DiffResult{
		Collections: []diff.CollectionDiff{
			{Name: "unchanged", DiffType: ""},
		},
	}

	plan := s.Plan(result)
	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions for identical collection, got %d", len(plan.Actions))
	}
}

func TestPlan_ErrorCollection(t *testing.T) {
	s := New(nil, nil)
	result := &diff.DiffResult{
		Collections: []diff.CollectionDiff{
			{Name: "broken", Error: "timeout"},
		},
	}

	plan := s.Plan(result)
	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions for error collection, got %d", len(plan.Actions))
	}
}

func TestPlan_EmptyResult(t *testing.T) {
	s := New(nil, nil)
	result := &diff.DiffResult{}

	plan := s.Plan(result)
	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(plan.Actions))
	}
}

func TestFilterResult_SelectSubset(t *testing.T) {
	oid1 := bson.NewObjectID()
	oid2 := bson.NewObjectID()
	oid3 := bson.NewObjectID()

	result := &diff.DiffResult{
		Database: "testdb",
		Collections: []diff.CollectionDiff{
			{
				Name:     "users",
				DiffType: diff.Modified,
				Documents: []diff.DocumentDiff{
					{ID: oid1, DiffType: diff.Added, Source: bson.M{"_id": oid1}},
					{ID: oid2, DiffType: diff.Removed},
					{ID: oid3, DiffType: diff.Modified, Source: bson.M{"_id": oid3}},
				},
			},
		},
	}

	// Select only the insert and modify, not the delete
	ops := []SyncOperation{
		{Collection: "users", DocID: oid1.Hex(), Type: "insert"},
		{Collection: "users", DocID: oid3.Hex(), Type: "modify"},
	}

	filtered := FilterResult(result, ops)

	if len(filtered.Collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(filtered.Collections))
	}
	coll := filtered.Collections[0]
	if len(coll.Documents) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(coll.Documents))
	}
	if coll.DiffType != diff.Modified {
		t.Errorf("expected DiffType=Modified, got %s", coll.DiffType)
	}
	if coll.Stats.DocumentsAdded != 1 {
		t.Errorf("expected 1 added, got %d", coll.Stats.DocumentsAdded)
	}
	if coll.Stats.DocumentsModified != 1 {
		t.Errorf("expected 1 modified, got %d", coll.Stats.DocumentsModified)
	}
	if coll.Stats.DocumentsRemoved != 0 {
		t.Errorf("expected 0 removed, got %d", coll.Stats.DocumentsRemoved)
	}
}

func TestFilterResult_ExcludesUnselectedCollections(t *testing.T) {
	result := &diff.DiffResult{
		Database: "testdb",
		Collections: []diff.CollectionDiff{
			{
				Name:     "users",
				DiffType: diff.Modified,
				Documents: []diff.DocumentDiff{
					{ID: "doc1", DiffType: diff.Added, Source: bson.M{"_id": "doc1"}},
				},
			},
			{
				Name:     "orders",
				DiffType: diff.Modified,
				Documents: []diff.DocumentDiff{
					{ID: "doc2", DiffType: diff.Removed},
				},
			},
		},
	}

	// Only select from users, not orders
	ops := []SyncOperation{
		{Collection: "users", DocID: "doc1", Type: "insert"},
	}

	filtered := FilterResult(result, ops)

	if len(filtered.Collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(filtered.Collections))
	}
	if filtered.Collections[0].Name != "users" {
		t.Errorf("expected users, got %s", filtered.Collections[0].Name)
	}
}

func TestFilterResult_AddedCollectionBecomesModified(t *testing.T) {
	result := &diff.DiffResult{
		Database: "testdb",
		Collections: []diff.CollectionDiff{
			{
				Name:     "new_coll",
				DiffType: diff.Added,
				Documents: []diff.DocumentDiff{
					{ID: "d1", DiffType: diff.Added, Source: bson.M{"_id": "d1"}},
					{ID: "d2", DiffType: diff.Added, Source: bson.M{"_id": "d2"}},
				},
			},
		},
	}

	ops := []SyncOperation{
		{Collection: "new_coll", DocID: "d1", Type: "insert"},
	}

	filtered := FilterResult(result, ops)

	if len(filtered.Collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(filtered.Collections))
	}
	// Should be Modified (not Added) so Apply doesn't create/drop entire collection
	if filtered.Collections[0].DiffType != diff.Modified {
		t.Errorf("expected Modified, got %s", filtered.Collections[0].DiffType)
	}
	if len(filtered.Collections[0].Documents) != 1 {
		t.Errorf("expected 1 document, got %d", len(filtered.Collections[0].Documents))
	}
}

func TestFilterResult_EmptySelection(t *testing.T) {
	result := &diff.DiffResult{
		Database: "testdb",
		Collections: []diff.CollectionDiff{
			{
				Name:     "users",
				DiffType: diff.Modified,
				Documents: []diff.DocumentDiff{
					{ID: "doc1", DiffType: diff.Added},
				},
			},
		},
	}

	filtered := FilterResult(result, []SyncOperation{})

	if len(filtered.Collections) != 0 {
		t.Errorf("expected 0 collections for empty selection, got %d", len(filtered.Collections))
	}
}

func TestCanonicalID_Types(t *testing.T) {
	oid := bson.NewObjectID()

	tests := []struct {
		name     string
		id       interface{}
		expected string
	}{
		{"ObjectID", oid, oid.Hex()},
		{"string", "hello", "hello"},
		{"hex string", oid.Hex(), oid.Hex()},
		{"float64 int", float64(42), "42"},
		{"int32", int32(42), "42"},
		{"int64", int64(42), "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalID(tt.id)
			if got != tt.expected {
				t.Errorf("canonicalID(%v) = %q, want %q", tt.id, got, tt.expected)
			}
		})
	}
}
