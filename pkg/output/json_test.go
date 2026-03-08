package output

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shamith/mongodiff/pkg/diff"
)

func TestJSONRenderer_EmptyResult(t *testing.T) {
	r := NewJSONRenderer()
	result := &diff.DiffResult{
		Source:    "localhost",
		Target:    "staging",
		Database:  "testdb",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	var buf bytes.Buffer
	if err := r.Render(&buf, result); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	if out["source"] != "localhost" {
		t.Errorf("expected source=localhost, got %v", out["source"])
	}
	if out["database"] != "testdb" {
		t.Errorf("expected database=testdb, got %v", out["database"])
	}
}

func TestJSONRenderer_WithCollections(t *testing.T) {
	r := NewJSONRenderer()
	result := &diff.DiffResult{
		Source:    "localhost",
		Target:    "staging",
		Database:  "testdb",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Collections: []diff.CollectionDiff{
			{
				Name:     "users",
				DiffType: diff.Modified,
				Documents: []diff.DocumentDiff{
					{
						ID:       "user1",
						DiffType: diff.Modified,
						Fields: []diff.FieldDiff{
							{Path: "name", DiffType: diff.Modified, OldValue: "Alice", NewValue: "Bob"},
						},
					},
				},
				Stats: diff.DiffStats{DocumentsModified: 1},
			},
			{
				Name:     "logs",
				DiffType: diff.Added,
				Stats:    diff.DiffStats{DocumentsAdded: 5},
			},
		},
		Stats: diff.DiffStats{CollectionsMatched: 1, CollectionsAdded: 1, DocumentsModified: 1, DocumentsAdded: 5},
	}

	var buf bytes.Buffer
	if err := r.Render(&buf, result); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	colls, ok := out["collections"].([]interface{})
	if !ok || len(colls) != 2 {
		t.Fatalf("expected 2 collections, got %v", out["collections"])
	}

	first := colls[0].(map[string]interface{})
	if first["name"] != "users" {
		t.Errorf("expected first collection=users, got %v", first["name"])
	}
	if first["diffType"] != "modified" {
		t.Errorf("expected diffType=modified, got %v", first["diffType"])
	}

	docs := first["documents"].([]interface{})
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
}

func TestJSONRenderer_SummaryOnly(t *testing.T) {
	r := NewJSONRenderer()
	r.SummaryOnly = true

	result := &diff.DiffResult{
		Source:    "localhost",
		Target:    "staging",
		Database:  "testdb",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Collections: []diff.CollectionDiff{
			{
				Name:     "users",
				DiffType: diff.Modified,
				Documents: []diff.DocumentDiff{
					{ID: "user1", DiffType: diff.Modified},
				},
				Stats: diff.DiffStats{DocumentsModified: 1},
			},
		},
	}

	var buf bytes.Buffer
	if err := r.Render(&buf, result); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	var out map[string]interface{}
	json.Unmarshal(buf.Bytes(), &out)

	colls := out["collections"].([]interface{})
	first := colls[0].(map[string]interface{})
	if first["documents"] != nil {
		t.Error("SummaryOnly should omit documents")
	}
}

func TestJSONRenderer_ErrorCollection(t *testing.T) {
	r := NewJSONRenderer()
	result := &diff.DiffResult{
		Source:    "localhost",
		Target:    "staging",
		Database:  "testdb",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Collections: []diff.CollectionDiff{
			{Name: "broken", Error: "connection timeout"},
		},
	}

	var buf bytes.Buffer
	r.Render(&buf, result)

	var out map[string]interface{}
	json.Unmarshal(buf.Bytes(), &out)

	colls := out["collections"].([]interface{})
	first := colls[0].(map[string]interface{})
	if first["diffType"] != "error" {
		t.Errorf("expected diffType=error, got %v", first["diffType"])
	}
	if first["error"] != "connection timeout" {
		t.Errorf("expected error message, got %v", first["error"])
	}
}

func TestCollectionToJSON(t *testing.T) {
	coll := diff.CollectionDiff{
		Name:     "products",
		DiffType: diff.Added,
		Documents: []diff.DocumentDiff{
			{
				ID:       bson.NewObjectID(),
				DiffType: diff.Added,
				Source:   bson.M{"name": "Widget"},
			},
		},
		Stats: diff.DiffStats{DocumentsAdded: 1},
	}

	result := CollectionToJSON(coll)
	if result["name"] != "products" {
		t.Errorf("expected name=products, got %v", result["name"])
	}
	if result["diffType"] != "added" {
		t.Errorf("expected diffType=added, got %v", result["diffType"])
	}

	docs := result["documents"].([]map[string]interface{})
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
}
