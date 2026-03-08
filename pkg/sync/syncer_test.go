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
