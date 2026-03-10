package profile

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Profile holds a saved set of connection and diff parameters.
type Profile struct {
	Name                string   `json:"name"`
	Source              string   `json:"source"`
	Target              string   `json:"target"`
	Database            string   `json:"database,omitempty"`
	Timeout             int      `json:"timeout,omitempty"`
	SourceToTarget      bool     `json:"sourceToTarget"`
	CollectionMode      string   `json:"collectionMode"`
	SelectedCollections []string `json:"selectedCollections,omitempty"`
	IgnoreFields        []string `json:"ignoreFields,omitempty"`
}

// profileFile is the on-disk JSON wrapper.
type profileFile struct {
	Profiles []Profile `json:"profiles"`
}

// Load reads profiles from a JSON file at path.
// If the file does not exist, it returns an empty slice and no error.
func Load(path string) ([]Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Profile{}, nil
		}
		return nil, err
	}

	var pf profileFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	if pf.Profiles == nil {
		return []Profile{}, nil
	}
	return pf.Profiles, nil
}

// Save writes profiles to a JSON file at path.
// Parent directories are created with mode 0700; the file is written with mode 0600.
func Save(path string, profiles []Profile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	pf := profileFile{Profiles: profiles}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Upsert adds or replaces a profile by name. If a profile with the same name
// already exists it is replaced in place; otherwise the new profile is appended.
func Upsert(profiles []Profile, p Profile) []Profile {
	for i, existing := range profiles {
		if existing.Name == p.Name {
			profiles[i] = p
			return profiles
		}
	}
	return append(profiles, p)
}

// Delete removes the profile with the given name. It returns the resulting
// slice and a boolean indicating whether a profile was found and removed.
func Delete(profiles []Profile, name string) ([]Profile, bool) {
	for i, p := range profiles {
		if p.Name == name {
			return append(profiles[:i], profiles[i+1:]...), true
		}
	}
	return profiles, false
}

// DefaultPath returns the default location for the profiles file:
// ~/.mongodiff/profiles.json
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to $HOME; if that is also empty the caller will get
		// a relative path, which is acceptable as a last resort.
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".mongodiff", "profiles.json")
}
