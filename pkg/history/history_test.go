package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPairHash_Deterministic(t *testing.T) {
	h1 := PairHash("localhost:27017", "staging:27017")
	h2 := PairHash("localhost:27017", "staging:27017")
	if h1 != h2 {
		t.Errorf("expected same hash, got %s and %s", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("expected 16 chars, got %d", len(h1))
	}
}

func TestPairHash_DifferentPairs(t *testing.T) {
	h1 := PairHash("localhost:27017", "staging:27017")
	h2 := PairHash("localhost:27017", "prod:27017")
	if h1 == h2 {
		t.Error("expected different hashes for different pairs")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	entries, err := Load(t.TempDir(), "s", "t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestAppend_And_Load(t *testing.T) {
	dir := t.TempDir()
	entry := Entry{
		ID:        "abc12345",
		Timestamp: time.Now().UTC(),
		Source:    "localhost:27017",
		Target:    "staging:27017",
		Database:  "myapp",
		Summary:   Summary{Inserted: 2, Replaced: 1, Deleted: 0},
		Operations: []Operation{
			{Collection: "users", DocID: "id1", Type: "insert"},
			{Collection: "users", DocID: "id2", Type: "insert"},
			{Collection: "users", DocID: "id3", Type: "replace"},
		},
		BackupPath: ".mongodiff/backups/test.json",
	}

	if err := Append(dir, "localhost:27017", "staging:27017", entry); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	entries, err := Load(dir, "localhost:27017", "staging:27017")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != "abc12345" {
		t.Errorf("expected id abc12345, got %s", entries[0].ID)
	}
	if entries[0].Summary.Inserted != 2 {
		t.Errorf("expected 2 inserted, got %d", entries[0].Summary.Inserted)
	}
	if len(entries[0].Operations) != 3 {
		t.Errorf("expected 3 operations, got %d", len(entries[0].Operations))
	}
}

func TestAppend_Multiple(t *testing.T) {
	dir := t.TempDir()

	e1 := Entry{ID: "aaa", Timestamp: time.Now().UTC(), Source: "s", Target: "t", Database: "db1"}
	e2 := Entry{ID: "bbb", Timestamp: time.Now().UTC(), Source: "s", Target: "t", Database: "db2"}

	Append(dir, "s", "t", e1)
	Append(dir, "s", "t", e2)

	entries, _ := Load(dir, "s", "t")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != "aaa" || entries[1].ID != "bbb" {
		t.Error("entries not in expected order")
	}
}

func TestAppend_DifferentPairs(t *testing.T) {
	dir := t.TempDir()

	e1 := Entry{ID: "aaa", Source: "s1", Target: "t1"}
	e2 := Entry{ID: "bbb", Source: "s2", Target: "t2"}

	Append(dir, "s1", "t1", e1)
	Append(dir, "s2", "t2", e2)

	entries1, _ := Load(dir, "s1", "t1")
	entries2, _ := Load(dir, "s2", "t2")

	if len(entries1) != 1 || entries1[0].ID != "aaa" {
		t.Error("pair 1 entries wrong")
	}
	if len(entries2) != 1 || entries2[0].ID != "bbb" {
		t.Error("pair 2 entries wrong")
	}
}

func TestNewID_Unique(t *testing.T) {
	ids := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID()
		if len(id) != 8 {
			t.Errorf("expected 8 chars, got %d", len(id))
		}
		if ids[id] {
			t.Errorf("duplicate id: %s", id)
		}
		ids[id] = true
	}
}

func TestDefaultDir(t *testing.T) {
	dir := DefaultDir()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".mongodiff", "history")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}
