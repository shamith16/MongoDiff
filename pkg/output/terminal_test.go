package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/shamith/mongodiff/pkg/diff"
)

func TestTerminalRenderer_EmptyResult(t *testing.T) {
	r := NewTerminalRenderer()
	result := &diff.DiffResult{
		Source:    "localhost",
		Target:    "staging",
		Database:  "testdb",
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	if err := r.Render(&buf, result); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "localhost") {
		t.Error("output should contain source")
	}
	if !strings.Contains(out, "staging") {
		t.Error("output should contain target")
	}
}

func TestTerminalRenderer_WithCollections(t *testing.T) {
	r := NewTerminalRenderer()
	result := &diff.DiffResult{
		Source:    "localhost",
		Target:    "staging",
		Database:  "testdb",
		Timestamp: time.Now(),
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
		},
		Stats: diff.DiffStats{CollectionsMatched: 1, DocumentsModified: 1},
	}

	var buf bytes.Buffer
	r.Render(&buf, result)

	out := buf.String()
	if !strings.Contains(out, "users") {
		t.Error("output should contain collection name")
	}
	if !strings.Contains(out, "name") {
		t.Error("output should contain field name")
	}
}

func TestTerminalRenderer_SummaryOnly(t *testing.T) {
	r := NewTerminalRenderer()
	r.SummaryOnly = true

	result := &diff.DiffResult{
		Source:    "localhost",
		Target:    "staging",
		Database:  "testdb",
		Timestamp: time.Now(),
		Collections: []diff.CollectionDiff{
			{
				Name:     "users",
				DiffType: diff.Modified,
				Documents: []diff.DocumentDiff{
					{
						ID:       "user1",
						DiffType: diff.Modified,
						Fields: []diff.FieldDiff{
							{Path: "secret", DiffType: diff.Modified, OldValue: "x", NewValue: "y"},
						},
					},
				},
				Stats: diff.DiffStats{DocumentsModified: 1},
			},
		},
		Stats: diff.DiffStats{CollectionsMatched: 1, DocumentsModified: 1},
	}

	var buf bytes.Buffer
	r.Render(&buf, result)

	out := buf.String()
	if strings.Contains(out, "secret") {
		t.Error("SummaryOnly should not show field details")
	}
	if !strings.Contains(out, "users") {
		t.Error("SummaryOnly should still show collection names")
	}
}
