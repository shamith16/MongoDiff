package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/shamith/mongodiff/pkg/diff"
	"github.com/shamith/mongodiff/pkg/history"
	"github.com/shamith/mongodiff/pkg/profile"
	mongoclient "github.com/shamith/mongodiff/pkg/mongo"
	"github.com/shamith/mongodiff/pkg/output"
	syncer "github.com/shamith/mongodiff/pkg/sync"
)

type diffRequest struct {
	Source         string   `json:"source"`
	Target         string   `json:"target"`
	Database       string   `json:"database"`
	Include        []string `json:"include,omitempty"`
	Exclude        []string `json:"exclude,omitempty"`
	IgnoreFields   []string `json:"ignoreFields,omitempty"`
	Timeout        int      `json:"timeout,omitempty"`
	SourceToTarget bool     `json:"sourceToTarget,omitempty"`
}

type connectionTestRequest struct {
	URI     string `json:"uri"`
	Timeout int    `json:"timeout,omitempty"`
}

type listCollectionsRequest struct {
	URI      string `json:"uri"`
	Database string `json:"database"`
	Timeout  int    `json:"timeout,omitempty"`
}

type syncRequest struct {
	diffRequest
	Operations []syncer.SyncOperation `json:"operations,omitempty"`
}

type historyRequest struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type historyExportRequest struct {
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	EntryIDs []string `json:"entryIds"`
	Format   string   `json:"format"`
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

func (s *Server) handleDiffStream(w http.ResponseWriter, r *http.Request) {
	var req diffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Source == "" || req.Target == "" || req.Database == "" {
		writeError(w, http.StatusBadRequest, "source, target, and database are required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	timeout := time.Duration(req.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx := r.Context()

	connectCtx, connectCancel := context.WithTimeout(ctx, timeout)
	defer connectCancel()

	source, err := mongoclient.Connect(connectCtx, req.Source, timeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer source.Disconnect(context.Background())

	target, err := mongoclient.Connect(connectCtx, req.Target, timeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer target.Disconnect(context.Background())

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	opts := diff.Options{
		IncludeCollections: req.Include,
		ExcludeCollections: req.Exclude,
		IgnoreFields:       req.IgnoreFields,
		SourceToTarget:     req.SourceToTarget,
	}

	differ := diff.New(source, target, opts)
	err = differ.DiffStream(ctx, req.Database, diff.StreamCallbacks{
		OnStart: func(total int) {
			startEvent := map[string]interface{}{
				"type":  "start",
				"total": total,
			}
			data, _ := json.Marshal(startEvent)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		OnCollection: func(coll diff.CollectionDiff, stats diff.DiffStats) {
			event := map[string]interface{}{
				"type":       "collection",
				"collection": output.CollectionToJSON(coll),
				"stats":      stats,
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
	})

	if err != nil {
		errEvent := map[string]interface{}{
			"type":  "error",
			"error": err.Error(),
		}
		data, _ := json.Marshal(errEvent)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	// Send done event
	doneEvent := map[string]interface{}{
		"type":   "done",
		"source": mongoclient.RedactURI(req.Source),
		"target": mongoclient.RedactURI(req.Target),
	}
	data, _ := json.Marshal(doneEvent)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
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
	var req syncRequest
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
		SourceToTarget:     req.SourceToTarget,
	}

	// Scope diff to only the collections referenced in selected operations
	if len(req.Operations) > 0 {
		collSet := make(map[string]bool)
		for _, op := range req.Operations {
			collSet[op.Collection] = true
		}
		opts.IncludeCollections = make([]string, 0, len(collSet))
		for c := range collSet {
			opts.IncludeCollections = append(opts.IncludeCollections, c)
		}
	}

	differ := diff.New(source, target, opts)
	result, err := differ.Diff(ctx, req.Database)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "diff failed: "+err.Error())
		return
	}
	result.Source = mongoclient.RedactURI(req.Source)
	result.Target = mongoclient.RedactURI(req.Target)

	// Filter to selected operations if provided
	if len(req.Operations) > 0 {
		result = syncer.FilterResult(result, req.Operations)
	}

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

	// Log the sync to history
	entry := history.Entry{
		ID:        history.NewID(),
		Timestamp: time.Now().UTC(),
		Source:    mongoclient.RedactURI(req.Source),
		Target:    mongoclient.RedactURI(req.Target),
		Database:  req.Database,
		Summary: history.Summary{
			Inserted: syncResult.DocumentsInserted,
			Replaced: syncResult.DocumentsReplaced,
			Deleted:  syncResult.DocumentsDeleted,
		},
		BackupPath: backupPath,
	}
	for _, coll := range result.Collections {
		for _, doc := range coll.Documents {
			var opType string
			switch doc.DiffType {
			case diff.Added:
				opType = "insert"
			case diff.Modified:
				opType = "replace"
			case diff.Removed:
				opType = "delete"
			}
			if opType != "" {
				entry.Operations = append(entry.Operations, history.Operation{
					Collection: coll.Name,
					DocID:      doc.ID,
					Type:       opType,
				})
			}
		}
	}
	// Only log to history if something was actually applied
	if syncResult.DocumentsInserted > 0 || syncResult.DocumentsReplaced > 0 || syncResult.DocumentsDeleted > 0 ||
		syncResult.CollectionsCreated > 0 || syncResult.CollectionsDropped > 0 {
		if err := history.Append(s.historyDir, entry.Source, entry.Target, entry); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write history: %v\n", err)
		}
	}

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
		SourceToTarget:     req.SourceToTarget,
	}

	differ := diff.New(source, target, opts)
	result, err := differ.Diff(ctx, req.Database)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	return result, cleanup, nil
}

func (s *Server) handleGetProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := profile.Load(s.profilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load profiles: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"profiles": profiles})
}

func (s *Server) handleSaveProfile(w http.ResponseWriter, r *http.Request) {
	var p profile.Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" {
		writeError(w, http.StatusBadRequest, "profile name is required")
		return
	}
	profiles, err := profile.Load(s.profilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load profiles: "+err.Error())
		return
	}
	profiles = profile.Upsert(profiles, p)
	if err := profile.Save(s.profilePath, profiles); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save profiles: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (s *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "profile name is required")
		return
	}
	profiles, err := profile.Load(s.profilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load profiles: "+err.Error())
		return
	}
	profiles, found := profile.Delete(profiles, name)
	if !found {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	if err := profile.Save(s.profilePath, profiles); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save profiles: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (s *Server) handleListCollections(w http.ResponseWriter, r *http.Request) {
	var req listCollectionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URI == "" || req.Database == "" {
		writeError(w, http.StatusBadRequest, "uri and database are required")
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer client.Disconnect(context.Background())
	collections, err := client.ListCollections(ctx, req.Database)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"collections": collections})
}

func (s *Server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	var req historyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Source == "" || req.Target == "" {
		writeError(w, http.StatusBadRequest, "source and target are required")
		return
	}

	allEntries, err := history.Load(s.historyDir, mongoclient.RedactURI(req.Source), mongoclient.RedactURI(req.Target))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load history: "+err.Error())
		return
	}

	// Filter out empty entries (no actual changes applied)
	var entries []history.Entry
	for _, e := range allEntries {
		if e.Summary.Inserted > 0 || e.Summary.Replaced > 0 || e.Summary.Deleted > 0 {
			entries = append(entries, e)
		}
	}
	if entries == nil {
		entries = []history.Entry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"entries": entries})
}

func (s *Server) handleExportHistory(w http.ResponseWriter, r *http.Request) {
	var req historyExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Source == "" || req.Target == "" {
		writeError(w, http.StatusBadRequest, "source and target are required")
		return
	}
	if req.Format != "markdown" && req.Format != "mongosh" {
		writeError(w, http.StatusBadRequest, "format must be 'markdown' or 'mongosh'")
		return
	}

	entries, err := history.Load(s.historyDir, mongoclient.RedactURI(req.Source), mongoclient.RedactURI(req.Target))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load history: "+err.Error())
		return
	}

	if len(req.EntryIDs) > 0 {
		idSet := make(map[string]bool, len(req.EntryIDs))
		for _, id := range req.EntryIDs {
			idSet[id] = true
		}
		var filtered []history.Entry
		for _, e := range entries {
			if idSet[e.ID] {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	var text string
	switch req.Format {
	case "markdown":
		text = history.ExportMarkdown(entries)
	case "mongosh":
		text = history.ExportMongosh(entries)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"text": text})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse{Error: message})
}
