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
		Summary:   Summary{Inserted: 1, Replaced: 1, Deleted: 1},
		Operations: []Operation{
			{Collection: "users", DocID: "664b1234a1b0", Type: "insert"},
			{
				Collection: "users",
				DocID:      "664c9012e4f5",
				Type:       "replace",
				Fields: []FieldChange{
					{Path: "name", OldValue: `"Alice"`, NewValue: `"Bob"`},
					{Path: "age", OldValue: "25", NewValue: "30"},
				},
			},
			{Collection: "orders", DocID: "664d3456a7b8", Type: "delete"},
		},
		BackupPath: ".mongodiff/backups/2026-03-10T14-30-00Z.json",
	}
}

func TestExportMarkdown_Content(t *testing.T) {
	md := ExportMarkdown([]Entry{testEntry()})

	checks := []string{
		"# Sync Report",
		"localhost:27017",
		"staging.example.com:27017",
		"myapp",
		"## users",
		"664b1234a1b0",
		"insert",
		"664c9012e4f5",
		"replace",
		"| Field | Source | Target |",
		"| `name` |",
		`"Alice"`,
		`"Bob"`,
		"| `age` |",
		"25",
		"30",
		"## orders",
		"664d3456a7b8",
		"delete",
		"1 inserted, 1 replaced, 1 deleted",
	}

	for _, check := range checks {
		if !strings.Contains(md, check) {
			t.Errorf("markdown missing: %q\n\nGot:\n%s", check, md)
		}
	}
}

func TestExportMarkdown_InsertNoFields(t *testing.T) {
	e := Entry{
		ID:       "x",
		Database: "db",
		Operations: []Operation{
			{Collection: "c", DocID: "id1", Type: "insert"},
		},
		Summary: Summary{Inserted: 1},
	}
	md := ExportMarkdown([]Entry{e})
	if !strings.Contains(md, "New document inserted from source") {
		t.Error("expected insert placeholder text")
	}
}

func TestExportMarkdown_DeleteNoFields(t *testing.T) {
	e := Entry{
		ID:       "x",
		Database: "db",
		Operations: []Operation{
			{Collection: "c", DocID: "id1", Type: "delete"},
		},
		Summary: Summary{Deleted: 1},
	}
	md := ExportMarkdown([]Entry{e})
	if !strings.Contains(md, "Document deleted from target") {
		t.Error("expected delete placeholder text")
	}
}

func TestExportMarkdown_MultipleEntries(t *testing.T) {
	e1 := testEntry()
	e2 := testEntry()
	e2.ID = "def67890"
	e2.Database = "other"

	md := ExportMarkdown([]Entry{e1, e2})

	if strings.Count(md, "# Sync Report") != 1 {
		t.Error("expected single header")
	}
	if !strings.Contains(md, "myapp") || !strings.Contains(md, "other") {
		t.Error("expected both databases")
	}
}

func TestExportMarkdown_Empty(t *testing.T) {
	md := ExportMarkdown([]Entry{})
	if !strings.Contains(md, "# Sync Report") {
		t.Error("expected header even with no entries")
	}
}

func TestExportMarkdown_FieldAbsentValues(t *testing.T) {
	e := Entry{
		ID:       "x",
		Database: "db",
		Operations: []Operation{
			{
				Collection: "c",
				DocID:      "id1",
				Type:       "replace",
				Fields: []FieldChange{
					{Path: "newField", NewValue: `"hello"`},
					{Path: "removedField", OldValue: "42"},
				},
			},
		},
		Summary: Summary{Replaced: 1},
	}
	md := ExportMarkdown([]Entry{e})
	if !strings.Contains(md, "_(absent)_") {
		t.Error("expected absent marker for missing values")
	}
}
