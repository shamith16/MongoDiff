package diff

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	mongoclient "github.com/shamith/mongodiff/pkg/mongo"
)

// Options controls which collections are compared.
type Options struct {
	IncludeCollections []string
	ExcludeCollections []string
	IgnoreFields       []string // dot-notation field paths to ignore (e.g. "__v", "meta.modified")
}

// Differ performs the comparison between two MongoDB databases.
type Differ struct {
	source *mongoclient.Client
	target *mongoclient.Client
	opts   Options
}

// New creates a new Differ.
func New(source, target *mongoclient.Client, opts Options) *Differ {
	return &Differ{
		source: source,
		target: target,
		opts:   opts,
	}
}

// CollectionCallback is called with each collection diff as it completes during streaming.
type CollectionCallback func(coll CollectionDiff, stats DiffStats)

// StreamCallbacks holds callbacks for streaming diff operations.
type StreamCallbacks struct {
	// OnStart is called after collection lists are resolved with the total count.
	OnStart func(totalCollections int)
	// OnCollection is called as each collection diff completes.
	OnCollection CollectionCallback
}

// Diff performs the comparison and returns a structured result.
// It does not mutate either database.
func (d *Differ) Diff(ctx context.Context, database string) (*DiffResult, error) {
	result := &DiffResult{
		Database:  database,
		Timestamp: time.Now(),
	}

	err := d.diffInternal(ctx, database, StreamCallbacks{
		OnCollection: func(coll CollectionDiff, stats DiffStats) {
			result.Collections = append(result.Collections, coll)
			result.Stats = stats
		},
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// DiffStream performs the comparison and calls callbacks as collections are diffed.
func (d *Differ) DiffStream(ctx context.Context, database string, cbs StreamCallbacks) error {
	return d.diffInternal(ctx, database, cbs)
}

func (d *Differ) diffInternal(ctx context.Context, database string, cbs StreamCallbacks) error {
	var stats DiffStats

	// Pass 1: Collection comparison
	sourceColls, err := d.source.ListCollections(ctx, database)
	if err != nil {
		return err
	}
	targetColls, err := d.target.ListCollections(ctx, database)
	if err != nil {
		return err
	}

	sourceColls = d.filterCollections(sourceColls)
	targetColls = d.filterCollections(targetColls)

	sourceSet := toSet(sourceColls)
	targetSet := toSet(targetColls)

	added := setDiff(sourceSet, targetSet)
	removed := setDiff(targetSet, sourceSet)
	matched := setIntersect(sourceSet, targetSet)
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(matched)

	if cbs.OnStart != nil {
		cbs.OnStart(len(added) + len(removed) + len(matched))
	}

	// Added collections (in source only)
	for _, name := range added {
		collDiff, err := d.diffAddedCollection(ctx, database, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping collection %q: %v\n", name, err)
			cbs.OnCollection(CollectionDiff{Name: name, Error: err.Error()}, stats)
			continue
		}
		stats.CollectionsAdded++
		stats.DocumentsAdded += collDiff.Stats.DocumentsAdded
		cbs.OnCollection(collDiff, stats)
	}

	// Removed collections (in target only)
	for _, name := range removed {
		collDiff, err := d.diffRemovedCollection(ctx, database, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping collection %q: %v\n", name, err)
			cbs.OnCollection(CollectionDiff{Name: name, Error: err.Error()}, stats)
			continue
		}
		stats.CollectionsRemoved++
		stats.DocumentsRemoved += collDiff.Stats.DocumentsRemoved
		cbs.OnCollection(collDiff, stats)
	}

	// Matched collections (in both)
	for _, name := range matched {
		collDiff, err := d.diffMatchedCollection(ctx, database, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping collection %q: %v\n", name, err)
			cbs.OnCollection(CollectionDiff{Name: name, Error: err.Error()}, stats)
			continue
		}
		stats.CollectionsMatched++
		stats.DocumentsAdded += collDiff.Stats.DocumentsAdded
		stats.DocumentsRemoved += collDiff.Stats.DocumentsRemoved
		stats.DocumentsModified += collDiff.Stats.DocumentsModified
		stats.DocumentsIdentical += collDiff.Stats.DocumentsIdentical
		cbs.OnCollection(collDiff, stats)
	}

	return nil
}

// diffAddedCollection handles a collection that exists only in source.
func (d *Differ) diffAddedCollection(ctx context.Context, database, name string) (CollectionDiff, error) {
	docs, err := d.source.FetchAllDocuments(ctx, database, name)
	if err != nil {
		return CollectionDiff{}, err
	}

	var docDiffs []DocumentDiff
	for id, doc := range docs {
		docDiffs = append(docDiffs, DocumentDiff{
			ID:       id,
			DiffType: Added,
			Source:   doc,
		})
	}

	return CollectionDiff{
		Name:      name,
		DiffType:  Added,
		Documents: docDiffs,
		Stats: DiffStats{
			DocumentsAdded: len(docs),
		},
	}, nil
}

// diffRemovedCollection handles a collection that exists only in target.
func (d *Differ) diffRemovedCollection(ctx context.Context, database, name string) (CollectionDiff, error) {
	docs, err := d.target.FetchAllDocuments(ctx, database, name)
	if err != nil {
		return CollectionDiff{}, err
	}

	var docDiffs []DocumentDiff
	for id, doc := range docs {
		docDiffs = append(docDiffs, DocumentDiff{
			ID:       id,
			DiffType: Removed,
			Target:   doc,
		})
	}

	return CollectionDiff{
		Name:      name,
		DiffType:  Removed,
		Documents: docDiffs,
		Stats: DiffStats{
			DocumentsRemoved: len(docs),
		},
	}, nil
}

// diffMatchedCollection handles a collection that exists in both source and target.
// Pass 2: Document comparison by _id.
// Pass 3: Field comparison per matched document.
func (d *Differ) diffMatchedCollection(ctx context.Context, database, name string) (CollectionDiff, error) {
	// Pass 2: Fetch _id values from both sides
	sourceIDs, err := d.source.FetchIDs(ctx, database, name)
	if err != nil {
		return CollectionDiff{}, err
	}
	targetIDs, err := d.target.FetchIDs(ctx, database, name)
	if err != nil {
		return CollectionDiff{}, err
	}

	sourceIDSet := toIDMap(sourceIDs)
	targetIDSet := toIDMap(targetIDs)

	var stats DiffStats
	var docDiffs []DocumentDiff

	// Documents added (source only)
	var addedIDs []interface{}
	for _, id := range sourceIDs {
		key := idKey(id)
		if _, exists := targetIDSet[key]; !exists {
			addedIDs = append(addedIDs, id)
		}
	}
	if len(addedIDs) > 0 {
		addedDocs, err := d.source.FetchDocuments(ctx, database, name, addedIDs)
		if err != nil {
			return CollectionDiff{}, err
		}
		for _, id := range addedIDs {
			docDiffs = append(docDiffs, DocumentDiff{
				ID:       id,
				DiffType: Added,
				Source:   addedDocs[id],
			})
			stats.DocumentsAdded++
		}
	}

	// Documents removed (target only)
	var removedIDs []interface{}
	for _, id := range targetIDs {
		key := idKey(id)
		if _, exists := sourceIDSet[key]; !exists {
			removedIDs = append(removedIDs, id)
		}
	}
	if len(removedIDs) > 0 {
		removedDocs, err := d.target.FetchDocuments(ctx, database, name, removedIDs)
		if err != nil {
			return CollectionDiff{}, err
		}
		for _, id := range removedIDs {
			docDiffs = append(docDiffs, DocumentDiff{
				ID:       id,
				DiffType: Removed,
				Target:   removedDocs[id],
			})
			stats.DocumentsRemoved++
		}
	}

	// Documents matched (both sides) — need field-level comparison
	var matchedIDs []interface{}
	for _, id := range sourceIDs {
		key := idKey(id)
		if _, exists := targetIDSet[key]; exists {
			matchedIDs = append(matchedIDs, id)
		}
	}

	if len(matchedIDs) > 0 {
		sourceDocs, err := d.source.FetchDocuments(ctx, database, name, matchedIDs)
		if err != nil {
			return CollectionDiff{}, err
		}
		targetDocs, err := d.target.FetchDocuments(ctx, database, name, matchedIDs)
		if err != nil {
			return CollectionDiff{}, err
		}

		// Pass 3: Field comparison
		for _, id := range matchedIDs {
			sourceDoc := sourceDocs[id]
			targetDoc := targetDocs[id]

			fields := CompareDocumentsFiltered(sourceDoc, targetDoc, d.opts.IgnoreFields)
			if len(fields) == 0 {
				stats.DocumentsIdentical++
				continue
			}

			docDiffs = append(docDiffs, DocumentDiff{
				ID:       id,
				DiffType: Modified,
				Fields:   fields,
				Source:   sourceDoc,
				Target:   targetDoc,
			})
			stats.DocumentsModified++
		}
	}

	// Determine collection-level diff type
	diffType := Modified
	if stats.DocumentsAdded == 0 && stats.DocumentsRemoved == 0 && stats.DocumentsModified == 0 {
		// All documents identical — this is a matched but identical collection
		diffType = "" // empty means identical
	}

	return CollectionDiff{
		Name:      name,
		DiffType:  diffType,
		Documents: docDiffs,
		Stats:     stats,
	}, nil
}

// filterCollections applies include/exclude filters to a collection list.
func (d *Differ) filterCollections(names []string) []string {
	if len(d.opts.IncludeCollections) > 0 {
		includeSet := make(map[string]bool)
		for _, name := range d.opts.IncludeCollections {
			includeSet[name] = true
		}
		var filtered []string
		for _, name := range names {
			if includeSet[name] {
				filtered = append(filtered, name)
			}
		}
		return filtered
	}

	if len(d.opts.ExcludeCollections) > 0 {
		excludeSet := make(map[string]bool)
		for _, name := range d.opts.ExcludeCollections {
			excludeSet[name] = true
		}
		var filtered []string
		for _, name := range names {
			if !excludeSet[name] {
				filtered = append(filtered, name)
			}
		}
		return filtered
	}

	return names
}

// Set operations on string slices
func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

func setDiff(a, b map[string]bool) []string {
	var result []string
	for item := range a {
		if !b[item] {
			result = append(result, item)
		}
	}
	return result
}

func setIntersect(a, b map[string]bool) []string {
	var result []string
	for item := range a {
		if b[item] {
			result = append(result, item)
		}
	}
	return result
}

// idKey creates a comparable string key from an _id value.
// This handles ObjectID and other types that may not be directly comparable as map keys.
func idKey(id interface{}) string {
	switch v := id.(type) {
	case bson.ObjectID:
		return "oid:" + v.Hex()
	case string:
		return "str:" + v
	default:
		return fmt.Sprintf("other:%v", v)
	}
}

func toIDMap(ids []interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(ids))
	for _, id := range ids {
		m[idKey(id)] = id
	}
	return m
}
