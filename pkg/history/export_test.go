package history

import (
	"strings"
	"testing"
	"time"
)

func testEntry() Entry {
	return Entry{
		ID:        "abc12345",
		Timestamp: time.Date(2026, 3, 10, 14, 30, 0, 0, time.UTC),
		Source:    "localhost:27017",
		Target:    "staging.example.com:27017",
		Database:  "myapp",
		Summary:   Summary{Inserted: 2, Replaced: 1, Deleted: 1},
		Operations: []Operation{
			{Collection: "users", DocID: "664b1234a1b0", Type: "insert"},
			{Collection: "users", DocID: "664b5678c2d3", Type: "insert"},
			{Collection: "users", DocID: "664c9012e4f5", Type: "replace"},
			{Collection: "orders", DocID: "664d3456a7b8", Type: "delete"},
		},
		BackupPath: ".mongodiff/backups/2026-03-10T14-30-00Z.json",
	}
}

func TestExportMarkdown_Content(t *testing.T) {
	md := ExportMarkdown([]Entry{testEntry()})

	checks := []string{
		"# Migration Guide",
		"localhost:27017",
		"staging.example.com:27017",
		"myapp",
		"## users (3 operation",
		"**Inserted:**",
		"664b1234a1b0",
		"**Replaced:**",
		"664c9012e4f5",
		"## orders (1 operation)",
		"**Deleted:**",
		"664d3456a7b8",
		"2 inserted, 1 replaced, 1 deleted",
	}

	for _, check := range checks {
		if !strings.Contains(md, check) {
			t.Errorf("markdown missing: %q\n\nGot:\n%s", check, md)
		}
	}
}

func TestExportMarkdown_MultipleEntries(t *testing.T) {
	e1 := testEntry()
	e2 := testEntry()
	e2.ID = "def67890"
	e2.Database = "other"

	md := ExportMarkdown([]Entry{e1, e2})

	if strings.Count(md, "# Migration Guide") != 1 {
		t.Error("expected single header")
	}
	if !strings.Contains(md, "myapp") || !strings.Contains(md, "other") {
		t.Error("expected both databases")
	}
}

func TestExportMarkdown_Empty(t *testing.T) {
	md := ExportMarkdown([]Entry{})
	if !strings.Contains(md, "# Migration Guide") {
		t.Error("expected header even with no entries")
	}
}

func TestExportMongosh_Content(t *testing.T) {
	script := ExportMongosh([]Entry{testEntry()})

	checks := []string{
		"use(\"myapp\")",
		"db.users.insertOne(",
		"db.users.replaceOne(",
		"db.orders.deleteOne(",
	}

	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("mongosh missing: %q\n\nGot:\n%s", check, script)
		}
	}
}

func TestExportMongosh_DeletesHaveNoTODO(t *testing.T) {
	e := Entry{
		ID:       "x",
		Database: "db",
		Operations: []Operation{
			{Collection: "c", DocID: "664d3456a7b80000abcdef12", Type: "delete"},
		},
	}
	script := ExportMongosh([]Entry{e})

	if strings.Contains(script, "TODO") {
		t.Error("delete operations should not have TODO comments")
	}
	if !strings.Contains(script, "deleteOne") {
		t.Error("expected deleteOne")
	}
}

func TestExportMongosh_NonObjectID(t *testing.T) {
	e := Entry{
		ID:       "x",
		Database: "db",
		Operations: []Operation{
			{Collection: "c", DocID: "short", Type: "delete"},
		},
	}
	script := ExportMongosh([]Entry{e})

	if strings.Contains(script, "ObjectId") {
		t.Error("short IDs should not be wrapped in ObjectId")
	}
	if !strings.Contains(script, "\"short\"") {
		t.Error("expected quoted string ID")
	}
}
