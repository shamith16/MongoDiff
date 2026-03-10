package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHandleDiff_MissingFields(t *testing.T) {
	s := New(3000)

	tests := []struct {
		name string
		body diffRequest
		want string
	}{
		{
			name: "missing source",
			body: diffRequest{Target: "mongodb://t", Database: "db"},
			want: "source, target, and database are required",
		},
		{
			name: "missing target",
			body: diffRequest{Source: "mongodb://s", Database: "db"},
			want: "source, target, and database are required",
		},
		{
			name: "missing database",
			body: diffRequest{Source: "mongodb://s", Target: "mongodb://t"},
			want: "source, target, and database are required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest("POST", "/api/diff", bytes.NewReader(body))
			w := httptest.NewRecorder()

			s.handleDiff(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}

			var resp errorResponse
			json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Error != tc.want {
				t.Errorf("expected %q, got %q", tc.want, resp.Error)
			}
		})
	}
}

func TestHandleDiff_InvalidBody(t *testing.T) {
	s := New(3000)
	req := httptest.NewRequest("POST", "/api/diff", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	s.handleDiff(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSync_MissingFields(t *testing.T) {
	s := New(3000)
	body, _ := json.Marshal(diffRequest{Source: "mongodb://s"})
	req := httptest.NewRequest("POST", "/api/sync", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleSync(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSyncDryRun_MissingFields(t *testing.T) {
	s := New(3000)
	body, _ := json.Marshal(diffRequest{Database: "db"})
	req := httptest.NewRequest("POST", "/api/sync/dry-run", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleSyncDryRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTestConnection_InvalidBody(t *testing.T) {
	s := New(3000)
	req := httptest.NewRequest("POST", "/api/test-connection", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()

	s.handleTestConnection(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleDiffStream_MissingFields(t *testing.T) {
	s := New(3000)
	body, _ := json.Marshal(diffRequest{Source: "mongodb://s"})
	req := httptest.NewRequest("POST", "/api/diff/stream", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleDiffStream(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusTeapot, "I'm a teapot")

	if w.Code != http.StatusTeapot {
		t.Errorf("expected 418, got %d", w.Code)
	}

	var resp errorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "I'm a teapot" {
		t.Errorf("expected 'I'm a teapot', got %q", resp.Error)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
}

func TestRoutes_Exist(t *testing.T) {
	s := New(3000)

	// Verify the mux handles these routes without panic
	tests := []struct {
		method string
		path   string
	}{
		{"POST", "/api/diff"},
		{"POST", "/api/diff/stream"},
		{"POST", "/api/sync"},
		{"POST", "/api/sync/dry-run"},
		{"POST", "/api/test-connection"},
		{"POST", "/api/collections"},
		{"GET", "/api/profiles"},
		{"POST", "/api/profiles"},
		{"DELETE", "/api/profiles/test"},
		{"GET", "/"},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, req)
			// Just verify it doesn't 404 from the mux (405 is ok for wrong method).
			// A handler-level 404 (with application/json content-type) is acceptable.
			if w.Code == http.StatusNotFound && w.Header().Get("Content-Type") != "application/json" {
				t.Errorf("route %s %s returned 404", tc.method, tc.path)
			}
		})
	}
}

func TestHandleListCollections_InvalidBody(t *testing.T) {
	s := New(3000)
	req := httptest.NewRequest("POST", "/api/collections", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	s.handleListCollections(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleListCollections_MissingFields(t *testing.T) {
	s := New(3000)
	body, _ := json.Marshal(map[string]string{"uri": "mongodb://localhost:27017"})
	req := httptest.NewRequest("POST", "/api/collections", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleListCollections(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleGetProfiles(t *testing.T) {
	s := New(3000)
	s.profilePath = filepath.Join(t.TempDir(), "profiles.json")

	req := httptest.NewRequest("GET", "/api/profiles", nil)
	w := httptest.NewRecorder()

	s.handleGetProfiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string][]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp["profiles"]) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(resp["profiles"]))
	}
}

func TestHandleSaveProfile(t *testing.T) {
	s := New(3000)
	s.profilePath = filepath.Join(t.TempDir(), "profiles.json")

	// Save a profile
	body, _ := json.Marshal(map[string]string{"name": "test", "source": "mongodb://s", "target": "mongodb://t"})
	req := httptest.NewRequest("POST", "/api/profiles", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleSaveProfile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify via GET
	req = httptest.NewRequest("GET", "/api/profiles", nil)
	w = httptest.NewRecorder()

	s.handleGetProfiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string][]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp["profiles"]) != 1 {
		t.Errorf("expected 1 profile, got %d", len(resp["profiles"]))
	}
}

func TestHandleSaveProfile_MissingName(t *testing.T) {
	s := New(3000)
	s.profilePath = filepath.Join(t.TempDir(), "profiles.json")

	body, _ := json.Marshal(map[string]string{"source": "mongodb://s"})
	req := httptest.NewRequest("POST", "/api/profiles", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleSaveProfile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var resp errorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "profile name is required" {
		t.Errorf("expected 'profile name is required', got %q", resp.Error)
	}
}

func TestHandleDeleteProfile(t *testing.T) {
	s := New(3000)
	s.profilePath = filepath.Join(t.TempDir(), "profiles.json")

	// Save a profile first
	body, _ := json.Marshal(map[string]string{"name": "deleteme", "source": "mongodb://s", "target": "mongodb://t"})
	req := httptest.NewRequest("POST", "/api/profiles", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSaveProfile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("save failed: %d", w.Code)
	}

	// Delete it
	req = httptest.NewRequest("DELETE", "/api/profiles/deleteme", nil)
	req.SetPathValue("name", "deleteme")
	w = httptest.NewRecorder()
	s.handleDeleteProfile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify via GET — should be 0 profiles
	req = httptest.NewRequest("GET", "/api/profiles", nil)
	w = httptest.NewRecorder()
	s.handleGetProfiles(w, req)

	var resp map[string][]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp["profiles"]) != 0 {
		t.Errorf("expected 0 profiles after delete, got %d", len(resp["profiles"]))
	}
}
