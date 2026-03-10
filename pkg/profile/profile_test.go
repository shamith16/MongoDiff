package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_MissingFile(t *testing.T) {
	profiles, err := Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("expected empty slice, got %d profiles", len(profiles))
	}
}

func TestSave_And_Load(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "profiles.json")

	want := []Profile{
		{
			Name:           "dev",
			Source:         "mongodb://localhost:27017",
			Target:         "mongodb://localhost:27018",
			Database:       "mydb",
			Timeout:        30,
			SourceToTarget: true,
			CollectionMode: "all",
		},
		{
			Name:                "staging",
			Source:               "mongodb://staging-src:27017",
			Target:               "mongodb://staging-tgt:27017",
			CollectionMode:       "selected",
			SelectedCollections:  []string{"users", "orders"},
			IgnoreFields:         []string{"updatedAt"},
		},
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected file mode 0600, got %04o", perm)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d profiles, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Errorf("profile[%d].Name = %q, want %q", i, got[i].Name, want[i].Name)
		}
		if got[i].Source != want[i].Source {
			t.Errorf("profile[%d].Source = %q, want %q", i, got[i].Source, want[i].Source)
		}
		if got[i].Target != want[i].Target {
			t.Errorf("profile[%d].Target = %q, want %q", i, got[i].Target, want[i].Target)
		}
		if got[i].Database != want[i].Database {
			t.Errorf("profile[%d].Database = %q, want %q", i, got[i].Database, want[i].Database)
		}
		if got[i].Timeout != want[i].Timeout {
			t.Errorf("profile[%d].Timeout = %d, want %d", i, got[i].Timeout, want[i].Timeout)
		}
		if got[i].SourceToTarget != want[i].SourceToTarget {
			t.Errorf("profile[%d].SourceToTarget = %v, want %v", i, got[i].SourceToTarget, want[i].SourceToTarget)
		}
		if got[i].CollectionMode != want[i].CollectionMode {
			t.Errorf("profile[%d].CollectionMode = %q, want %q", i, got[i].CollectionMode, want[i].CollectionMode)
		}
	}
}

func TestUpsert_Insert(t *testing.T) {
	profiles := []Profile{
		{Name: "dev", Source: "s1", Target: "t1", CollectionMode: "all"},
	}
	p := Profile{Name: "staging", Source: "s2", Target: "t2", CollectionMode: "all"}

	result := Upsert(profiles, p)
	if len(result) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(result))
	}
	if result[1].Name != "staging" {
		t.Errorf("expected appended profile name %q, got %q", "staging", result[1].Name)
	}
}

func TestUpsert_Update(t *testing.T) {
	profiles := []Profile{
		{Name: "dev", Source: "old-source", Target: "t1", CollectionMode: "all"},
		{Name: "staging", Source: "s2", Target: "t2", CollectionMode: "all"},
	}
	p := Profile{Name: "dev", Source: "new-source", Target: "t1", CollectionMode: "selected"}

	result := Upsert(profiles, p)
	if len(result) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(result))
	}
	if result[0].Source != "new-source" {
		t.Errorf("expected Source %q, got %q", "new-source", result[0].Source)
	}
	if result[0].CollectionMode != "selected" {
		t.Errorf("expected CollectionMode %q, got %q", "selected", result[0].CollectionMode)
	}
}

func TestDelete_Exists(t *testing.T) {
	profiles := []Profile{
		{Name: "dev", Source: "s1", Target: "t1", CollectionMode: "all"},
		{Name: "staging", Source: "s2", Target: "t2", CollectionMode: "all"},
	}

	result, found := Delete(profiles, "dev")
	if !found {
		t.Fatal("expected found=true")
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(result))
	}
	if result[0].Name != "staging" {
		t.Errorf("expected remaining profile %q, got %q", "staging", result[0].Name)
	}
}

func TestDelete_NotFound(t *testing.T) {
	profiles := []Profile{
		{Name: "dev", Source: "s1", Target: "t1", CollectionMode: "all"},
	}

	result, found := Delete(profiles, "nonexistent")
	if found {
		t.Fatal("expected found=false")
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(result))
	}
}

func TestDefaultPath(t *testing.T) {
	path := DefaultPath()
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.HasSuffix(path, "profiles.json") {
		t.Errorf("expected path to end with profiles.json, got %q", path)
	}
}
