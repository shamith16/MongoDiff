package history

import (
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// FieldChange records a single field-level change within a document.
type FieldChange struct {
	Path     string `json:"path"`
	OldValue string `json:"oldValue,omitempty"`
	NewValue string `json:"newValue,omitempty"`
}

// Operation records a single document-level sync action.
type Operation struct {
	Collection string        `json:"collection"`
	DocID      interface{}   `json:"docId"`
	Type       string        `json:"type"` // "insert", "replace", "delete"
	Fields     []FieldChange `json:"fields,omitempty"`
}

// Summary holds aggregate counts for an entry.
type Summary struct {
	Inserted int `json:"inserted"`
	Replaced int `json:"replaced"`
	Deleted  int `json:"deleted"`
}

// Entry is a single sync history record.
type Entry struct {
	ID         string      `json:"id"`
	Timestamp  time.Time   `json:"timestamp"`
	Source     string      `json:"source"`
	Target     string      `json:"target"`
	Database   string      `json:"database"`
	Summary    Summary     `json:"summary"`
	Operations []Operation `json:"operations"`
	BackupPath string      `json:"backupPath,omitempty"`
}

// PairHash returns a deterministic 16-char hex hash for a source+target pair.
func PairHash(source, target string) string {
	h := sha256.Sum256([]byte(source + "|" + target))
	return fmt.Sprintf("%x", h[:8])
}

// DefaultDir returns ~/.mongodiff/history/
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".mongodiff", "history")
}

func filePath(dir, source, target string) string {
	return filepath.Join(dir, PairHash(source, target)+".json")
}

// Load reads all entries for a source+target pair.
// Returns an empty slice if no history file exists.
func Load(dir, source, target string) ([]Entry, error) {
	data, err := os.ReadFile(filePath(dir, source, target))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, err
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		return []Entry{}, nil
	}
	return entries, nil
}

// Append adds an entry to the history file for the given pair.
func Append(dir, source, target string, entry Entry) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	entries, err := Load(dir, source, target)
	if err != nil {
		return err
	}

	entries = append(entries, entry)

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath(dir, source, target), data, 0600)
}

// NewID generates an 8-char random hex ID.
func NewID() string {
	b := make([]byte, 4)
	crand.Read(b)
	return fmt.Sprintf("%x", b)
}
