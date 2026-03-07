package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/shamith/mongodiff/pkg/diff"
	mongoclient "github.com/shamith/mongodiff/pkg/mongo"
	"github.com/shamith/mongodiff/pkg/output"
	syncer "github.com/shamith/mongodiff/pkg/sync"
)

type diffRequest struct {
	Source       string   `json:"source"`
	Target       string   `json:"target"`
	Database     string   `json:"database"`
	Include      []string `json:"include,omitempty"`
	Exclude      []string `json:"exclude,omitempty"`
	IgnoreFields []string `json:"ignoreFields,omitempty"`
	Timeout      int      `json:"timeout,omitempty"`
}

type connectionTestRequest struct {
	URI     string `json:"uri"`
	Timeout int    `json:"timeout,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	var req diffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Source == "" || req.Target == "" || req.Database == "" {
		writeError(w, http.StatusBadRequest, "source, target, and database are required")
		return
	}

	result, cleanup, err := runDiff(req)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result.Source = mongoclient.RedactURI(req.Source)
	result.Target = mongoclient.RedactURI(req.Target)

	// Render as JSON
	jr := output.NewJSONRenderer()
	var buf bytes.Buffer
	if err := jr.Render(&buf, result); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to render result")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(buf.Bytes())
}

func (s *Server) handleSyncDryRun(w http.ResponseWriter, r *http.Request) {
	var req diffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Source == "" || req.Target == "" || req.Database == "" {
		writeError(w, http.StatusBadRequest, "source, target, and database are required")
		return
	}

	result, cleanup, err := runDiff(req)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	syn := syncer.New(nil, nil)
	plan := syn.Plan(result)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(plan)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	var req diffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Source == "" || req.Target == "" || req.Database == "" {
		writeError(w, http.StatusBadRequest, "source, target, and database are required")
		return
	}

	timeout := time.Duration(req.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx := context.Background()

	connectCtx, connectCancel := context.WithTimeout(ctx, timeout)
	defer connectCancel()

	source, err := mongoclient.Connect(connectCtx, req.Source, timeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "source: "+err.Error())
		return
	}
	defer source.Disconnect(context.Background())

	target, err := mongoclient.Connect(connectCtx, req.Target, timeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "target: "+err.Error())
		return
	}
	defer target.Disconnect(context.Background())

	opts := diff.Options{
		IncludeCollections: req.Include,
		ExcludeCollections: req.Exclude,
		IgnoreFields:       req.IgnoreFields,
	}

	differ := diff.New(source, target, opts)
	result, err := differ.Diff(ctx, req.Database)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "diff failed: "+err.Error())
		return
	}
	result.Source = mongoclient.RedactURI(req.Source)
	result.Target = mongoclient.RedactURI(req.Target)

	syn := syncer.New(source, target)

	backupPath, err := syn.Backup(ctx, result)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup failed: "+err.Error())
		return
	}

	syncResult, err := syn.Apply(ctx, result)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sync failed: "+err.Error())
		return
	}
	syncResult.BackupPath = backupPath

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(syncResult)
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var req connectionTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	timeout := time.Duration(req.Timeout) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, err := mongoclient.Connect(ctx, req.URI, timeout)
	if err != nil {
		writeError(w, http.StatusOK, err.Error()) // 200 with error in body — it's a test
		return
	}
	defer client.Disconnect(context.Background())

	databases, err := client.ListDatabases(ctx)
	if err != nil {
		writeError(w, http.StatusOK, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"databases": databases,
	})
}

func runDiff(req diffRequest) (*diff.DiffResult, func(), error) {
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx := context.Background()

	connectCtx, connectCancel := context.WithTimeout(ctx, timeout)
	defer connectCancel()

	source, err := mongoclient.Connect(connectCtx, req.Source, timeout)
	if err != nil {
		return nil, nil, err
	}

	target, err := mongoclient.Connect(connectCtx, req.Target, timeout)
	if err != nil {
		source.Disconnect(context.Background())
		return nil, nil, err
	}

	cleanup := func() {
		source.Disconnect(context.Background())
		target.Disconnect(context.Background())
	}

	opts := diff.Options{
		IncludeCollections: req.Include,
		ExcludeCollections: req.Exclude,
		IgnoreFields:       req.IgnoreFields,
	}

	differ := diff.New(source, target, opts)
	result, err := differ.Diff(ctx, req.Database)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	return result, cleanup, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse{Error: message})
}
