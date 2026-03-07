# mongodiff — Technical Specification

> A CLI tool to diff and sync MongoDB databases. Born from the real problem of missing database changes when promoting code from local development to staging to production.

---

## 1. Problem Statement

When working on features that involve MongoDB schema or document changes, development happens against a local Dockerized Mongo instance. At PR time, the same changes must be manually replicated to a remote staging database. This process is error-prone: fields get missed, documents get forgotten, and when staging is promoted to production (where the developer has no write access), those gaps become blockers that require a tech lead to fix.

There is no `git diff` for MongoDB. **mongodiff** fills that gap.

---

## 2. Core Concept

mongodiff connects to two MongoDB instances (source and target), compares them at the collection and document level, and produces a human-readable diff showing exactly what's different. That's the product. Everything else — syncing, UI, history — is layered on top of this single capability.

---

## 3. Architecture

### 3.1 Layered Design

```
┌─────────────────────────────────────────────────┐
│                   Interfaces                     │
│                                                  │
│   ┌─────────┐   ┌──────────┐   ┌────────────┐   │
│   │   CLI   │   │  HTTP    │   │   Web UI   │   │
│   │         │   │  Server  │   │  (SPA)     │   │
│   └────┬────┘   └────┬─────┘   └─────┬──────┘   │
│        │             │               │           │
│        └─────────────┼───────────────┘           │
│                      │                           │
│               ┌──────▼──────┐                    │
│               │ Core Library│                    │
│               │ (pkg/diff)  │                    │
│               └──────┬──────┘                    │
│                      │                           │
│               ┌──────▼──────┐                    │
│               │  MongoDB    │                    │
│               │  Driver     │                    │
│               └─────────────┘                    │
└─────────────────────────────────────────────────┘
```

The architecture follows one rule: **the core library has no opinions about I/O**. It accepts connection parameters, performs the diff, and returns structured results. The CLI, HTTP server, and Web UI are thin wrappers that format those results for their respective mediums.

A secondary design principle: **the per-collection diff is the atomic unit of work**. The function `diffCollection(ctx, db, name) CollectionDiff` is self-contained — it connects to both sides, fetches documents, compares, and returns a result. This isolation is what makes concurrency (run N collection diffs as goroutines) and streaming (emit each collection result as it completes) possible later without rewriting the core logic. See Sections 5.4 and 5.5.

This is the same pattern used by tools like Terraform (core → CLI → Cloud UI) and Docker (engine → CLI → Desktop). It ensures logic is written once and tested once.

### 3.2 Project Structure

```
mongodiff/
├── cmd/
│   └── mongodiff/
│       └── main.go              # CLI entrypoint
├── pkg/
│   ├── diff/
│   │   ├── differ.go            # Core diff engine
│   │   ├── types.go             # DiffResult, CollectionDiff, DocumentDiff
│   │   ├── comparator.go        # Field-level deep comparison
│   │   └── differ_test.go
│   ├── mongo/
│   │   ├── client.go            # Connection management, wrapper around driver
│   │   └── client_test.go
│   └── output/
│       ├── terminal.go          # Color-coded terminal renderer
│       ├── json.go              # JSON renderer (future)
│       └── renderer.go          # Renderer interface
├── internal/
│   ├── cli/
│   │   ├── root.go              # Root command setup
│   │   ├── diff.go              # `mongodiff diff` command
│   │   ├── sync.go              # `mongodiff sync` command (v0.2)
│   │   └── serve.go             # `mongodiff serve` command (v0.3)
│   └── server/                  # HTTP server (v0.3)
│       ├── server.go
│       ├── handlers.go
│       └── routes.go
├── web/                          # Web UI static assets (v0.3)
│   └── dist/
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### 3.3 Core Library API Surface

```go
// pkg/diff/types.go

type DiffType string

const (
    Added    DiffType = "added"
    Removed  DiffType = "removed"
    Modified DiffType = "modified"
)

// Top-level result of a diff operation
type DiffResult struct {
    Source      string           // source connection identifier
    Target      string           // target connection identifier
    Database    string
    Timestamp   time.Time
    Collections []CollectionDiff
    Stats       DiffStats
}

type DiffStats struct {
    CollectionsAdded   int
    CollectionsRemoved int
    CollectionsMatched int
    DocumentsAdded     int
    DocumentsRemoved   int
    DocumentsModified  int
    DocumentsIdentical int
}

// Diff for a single collection
type CollectionDiff struct {
    Name      string
    DiffType  DiffType        // added (exists only in source), removed, modified
    Documents []DocumentDiff  // nil if collection-level add/remove
    Stats     DiffStats
}

// Diff for a single document
type DocumentDiff struct {
    ID        interface{}     // the _id value
    DiffType  DiffType
    Fields    []FieldDiff     // nil if document-level add/remove
    Source    bson.M          // full source doc (for added/modified)
    Target    bson.M          // full target doc (for removed/modified)
}

// Diff for a single field within a document
type FieldDiff struct {
    Path      string          // dot-notation path, e.g. "address.city"
    DiffType  DiffType
    OldValue  interface{}     // value in target (nil if added)
    NewValue  interface{}     // value in source (nil if removed)
}
```

```go
// pkg/diff/differ.go

type Options struct {
    IncludeCollections []string   // if set, only these collections are compared
    ExcludeCollections []string   // if set, these collections are skipped
}

type Differ struct {
    source *mongo.Client
    target *mongo.Client
    opts   Options
}

func New(source, target *mongo.Client, opts Options) *Differ

// Diff performs the comparison and returns a structured result.
// It does not mutate either database.
func (d *Differ) Diff(ctx context.Context, database string) (*DiffResult, error)
```

This is the entire public API for v0.1. One struct, one method. The CLI just calls `Diff()` and passes the result to a renderer.

#### Planned: Streaming API (post-MVP)

The synchronous `Diff()` method returns only after all comparisons are complete. For larger databases or better UX, a streaming variant will be introduced that emits results as they're computed:

```go
// pkg/diff/stream.go (planned — not in v0.1)

type EventType string

const (
    EventCollectionStart EventType = "collection_start"  // beginning to diff a collection
    EventDocumentDiff    EventType = "document_diff"      // single document result
    EventCollectionDone  EventType = "collection_done"    // finished a collection
    EventComplete        EventType = "complete"           // all done, final stats
)

type DiffEvent struct {
    Type       EventType
    Collection string          // which collection this event belongs to
    Data       interface{}     // CollectionDiff, DocumentDiff, or DiffStats depending on Type
}

// DiffStream performs the same comparison as Diff() but emits results
// incrementally through a channel. The channel is closed when the diff
// is complete or the context is cancelled.
func (d *Differ) DiffStream(ctx context.Context, database string) <-chan DiffEvent
```

The synchronous `Diff()` will remain as the simple path — internally it may eventually just drain `DiffStream()` into a `DiffResult` struct. Both interfaces call the same per-collection diff logic underneath.

This streaming API enables:
- **CLI:** Print collection summaries and document diffs as they arrive, no waiting for full completion.
- **HTTP Server:** Serve results via Server-Sent Events (SSE), so the Web UI updates in real-time.
- **Concurrency:** The channel-based design naturally composes with goroutine-per-collection parallelism (see Section 5.4).

---

## 4. CLI Interface Design

### 4.1 v0.1 — Diff

```bash
# Basic usage
mongodiff diff \
  --source "mongodb://localhost:27017" \
  --target "mongodb://staging.example.com:27017" \
  --db myapp

# With filters
mongodiff diff \
  --source "mongodb://localhost:27017" \
  --target "mongodb://staging.example.com:27017" \
  --db myapp \
  --include users,products \
  --exclude sessions,logs

# Environment variables also work
MONGODIFF_SOURCE="mongodb://localhost:27017" \
MONGODIFF_TARGET="mongodb://staging.example.com:27017" \
mongodiff diff --db myapp
```

### 4.2 Output Example

```
mongodiff — comparing local → staging (database: myapp)

━━━ Collections ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  + feature_flags              (new collection, 3 documents)
  ~ users                      (2 modified, 1 added)
  ~ products                   (1 modified)
    orders                     (identical)

━━━ Collection: feature_flags (added) ━━━━━━━━━━━

  + { _id: "dark_mode", enabled: true, rollout: 0.5 }
  + { _id: "new_checkout", enabled: false }
  + { _id: "beta_search", enabled: true, rollout: 0.1 }

━━━ Collection: users ━━━━━━━━━━━━━━━━━━━━━━━━━━━

  + _id: ObjectId("665a...f3c2")
    { name: "Test User", role: "admin", ... }

  ~ _id: ObjectId("664b...a1b0")
    - preferences.theme: "light"
    + preferences.theme: "dark"
    + preferences.notifications: { email: true, sms: false }

  ~ _id: ObjectId("664b...a1b1")
    - status: "active"
    + status: "suspended"
    + suspendedAt: 2024-06-01T00:00:00Z

━━━ Collection: products ━━━━━━━━━━━━━━━━━━━━━━━━

  ~ _id: ObjectId("663c...d4e5")
    + metadata.seoTitle: "Premium Widget | MyApp"
    + metadata.seoDescription: "The best widget..."

━━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  1 collection added, 2 collections modified, 1 identical
  4 documents added, 3 documents modified, 0 removed
```

Colors: `+` lines in green, `-` lines in red, `~` lines in yellow, identical in gray.

### 4.3 v0.2 — Sync

```bash
# Dry run first — shows what would be applied, changes nothing
mongodiff sync \
  --source "mongodb://localhost:27017" \
  --target "mongodb://staging.example.com:27017" \
  --db myapp \
  --dry-run

# Apply for real — creates backup, then applies
mongodiff sync \
  --source "mongodb://localhost:27017" \
  --target "mongodb://staging.example.com:27017" \
  --db myapp

# Output:
# ⚠ This will modify 4 documents across 3 collections in staging.
# Backup saved to: .mongodiff/backups/2024-06-01T12-00-00Z.bson
# Proceed? [y/N]
```

### 4.4 v0.3 — Serve

```bash
# Start the server with embedded Web UI
mongodiff serve --port 8080

# Opens browser at http://localhost:8080
# UI lets you enter connections, run diffs, view results visually
```

---

## 5. Diff Algorithm

The diff engine works in three passes:

**Pass 1 — Collection Comparison**
List all collection names in source and target. Classify each as `added` (source only), `removed` (target only), or `matched` (both). Apply include/exclude filters before comparison.

**Pass 2 — Document Comparison (per matched collection)**
For each matched collection, fetch only `_id` values from both sides using a projected query (`projection: {_id: 1}`). This avoids pulling full document content just to determine which documents exist where. Classify each document as `added` (source only), `removed` (target only), or `matched` (both). This is a set difference operation on `_id` values. Full documents are fetched only for `matched` pairs that need field-level comparison in Pass 3. For `added` documents, fetch full content from source only (for display). For `removed` documents, fetch full content from target only (for display).

**Pass 3 — Field Comparison (per matched document)**
For each matched document, do a recursive deep-diff of the BSON structure. Walk both documents field-by-field, tracking the dot-notation path. For each field, classify as `added` (source only), `removed` (target only), `modified` (different values), or `identical`. Nested documents and arrays are compared recursively. Array comparison is positional (index-based), not content-based — this is a deliberate simplification for v1. Type changes are explicitly detected and surfaced (see Rule 1 below).

### 5.1 Comparison Rules

These are the implementation-level rules for how two BSON values are compared. They govern Pass 3 entirely and are non-negotiable — the correctness of the diff depends on them.

**Fundamental principle: compare BSON values, not JSON representations.** The comparator works on the Go driver's deserialized types (`bson.M`, `primitive.ObjectID`, `primitive.DateTime`, etc.), never on JSON strings.

**Context:** The target codebase uses Mongoose (Node.js ODM). This means `_id` values are `ObjectId` type in BSON (not strings, despite appearing as hex strings in JSON). The `__v` field is Mongoose's internal version key and will frequently differ between environments — this is expected noise, not a real diff. If it becomes disruptive in practice, it's the strongest argument for a future `--ignore-fields` flag.

#### Rule 1 — Type Check First, Always

Before comparing values, compare BSON types. If the types differ, it's a modification with a type change, even if the values look numerically or semantically similar.

```
# Example output for type mismatch
~ settings.maxRetries: (int32 → double) 3 → 3.0
```

This catches real bugs — a field accidentally stored as a string instead of a number, or a date stored as a string instead of ISODate.

#### Rule 2 — Null vs Absent vs Empty

All three are distinct states:

| State | BSON representation | Example |
|---|---|---|
| Null | Field exists, value is BSON null | `lastName: null` |
| Absent | Field does not exist in the document | (no `lastName` key) |
| Empty (array) | Field exists, value is `[]` | `hobbies: []` |
| Empty (string) | Field exists, value is `""` | `name: ""` |

A field changing from `null` to absent is a removal. A field changing from `[]` to absent is a removal. These are not equivalent and the diff must distinguish them:

```
# null → absent
- lastName: null
+ lastName: (absent)

# absent → empty array
- hobbies: (absent)
+ hobbies: []
```

This matters for the codebase's data where explicit nulls are used (`lastName: null`, `caregiverId: null`, `journey: null`).

#### Rule 3 — Nested Documents: Recursive, Key-Order-Independent

For nested documents (`meta`, `name`, `participantDetails`), sort keys alphabetically before walking. `{a: 1, b: 2}` and `{b: 2, a: 1}` are identical.

Recursion tracks dot-notation paths:

```
~ participantDetails.preferredNotificationTime[0]: "10:00:00" → "12:00:00"
~ meta.modified.$date: "2026-03-05T..." → "2026-03-06T..."
```

Nesting can be arbitrarily deep. The comparator does not impose a depth limit.

#### Rule 4 — Arrays: Positional, Strict

Compare index by index. Length differences produce additions or removals at the tail.

```
# Source: ["medication-education-adherence", "recovery-symptom-tracking", "new-focus"]
# Target: ["medication-education-adherence", "recovery-symptom-tracking"]

~ focusAreas[2]:
  + "new-focus"
```

```
# Source: ["a", "b"]
# Target: ["b", "a"]

~ [0]: "b" → "a"
~ [1]: "a" → "b"
```

Arrays of nested documents are also compared positionally. Element at index 0 in source is compared to element at index 0 in target, recursively.

**Known limitation:** Insertions at the front of an array cause every subsequent element to appear modified due to index shifting. This is accepted for v0.1. If real-world usage produces excessive noise on arrays, LCS (longest common subsequence) diffing will be considered in a future version.

#### Rule 5 — Dates

MongoDB's `ISODate` is stored internally as UTC milliseconds (int64). Two dates are equal if and only if their millisecond values are equal. No timezone normalization is needed since BSON stores dates as UTC.

Display format in diff output: ISO 8601 (`2026-03-06T10:07:41.734Z`).

**Important:** String-formatted dates like `dob: "1976-03-17"` are strings, not BSON dates. The comparator treats them as strings. It does not attempt to parse or semantically compare date-like strings.

#### Rule 6 — ObjectId

Byte-level equality on the 12-byte value. Two ObjectIds are equal only if all 12 bytes match.

Display format: `ObjectId("665a1b2c3d4e5f6a7b8c9d0e")`

#### Rule 7 — Strings

Exact byte equality. No trimming, no case folding, no normalization.

`"demo"` ≠ `"Demo"` ≠ `" demo"` ≠ `"demo "`

#### Rule 8 — Numbers (Strict Type Comparison)

`int32`, `int64`, and `double` are distinct BSON types. A type mismatch is a modification even if the numeric value is equivalent.

| Source | Target | Result |
|---|---|---|
| `int32(3)` | `int32(3)` | Identical |
| `int32(3)` | `int64(3)` | Modified (type: int32 → int64) |
| `int32(3)` | `double(3.0)` | Modified (type: int32 → double) |
| `double(3.0)` | `double(3.0)` | Identical |
| `int64(0)` | `int32(0)` | Modified (type: int64 → int32) |

This is deliberately strict. In a Mongoose codebase where Node.js and Go drivers may serialize numbers differently, type mismatches are real bugs worth catching.

#### Rule 9 — Booleans

`true == true`, `false == false`. No truthy/falsy coercion.

#### Rule 10 — Remaining BSON Types

| BSON Type | Equality Rule | Display Format |
|---|---|---|
| Decimal128 | String representation equality (avoids float precision issues) | `Decimal128("123.456")` |
| Binary | Byte-for-byte comparison | Truncated base64 (first 32 chars + `...`) |
| Regex | Pattern AND flags must both match | `/pattern/flags` |
| Undefined | Type equality only (no value) | `undefined` |
| MinKey | Type equality only | `MinKey` |
| MaxKey | Type equality only | `MaxKey` |

#### Rule 11 — Unknown Types (Fallback)

If the comparator encounters a BSON type not covered above, it falls back to `reflect.DeepEqual` from Go's standard library and logs a warning to stderr:

```
⚠ Unknown BSON type encountered at path "data.custom": reflect.Type=<type>. Using reflect.DeepEqual fallback.
```

This ensures the tool never crashes on unexpected data, but also never silently miscompares.

### 5.2 Comparison Rules Summary Matrix

| BSON Type | Equality Rule | Type-Strict | Display Format |
|---|---|---|---|
| ObjectId | 12-byte equality | N/A | `ObjectId("665a...f3c2")` |
| String | Exact byte equality | N/A | `"value"` |
| Int32 | Numeric equality | Yes — int32 ≠ int64 ≠ double | `3` (tagged if mismatch) |
| Int64 | Numeric equality | Yes | `3` (tagged if mismatch) |
| Double | Numeric equality | Yes | `3.0` (tagged if mismatch) |
| Boolean | Direct equality | N/A | `true` / `false` |
| Null | Always equal to other nulls | N/A | `null` |
| Absent | Distinct from null | N/A | `(absent)` |
| DateTime | Millisecond UTC equality | N/A | `2026-03-06T10:07:41.734Z` |
| Decimal128 | String representation | N/A | `Decimal128("123.456")` |
| Document | Recursive, key-order-independent | N/A | Nested diff with dot-paths |
| Array | Positional, index-by-index | N/A | Indexed entries `[0]`, `[1]` |
| Binary | Byte-for-byte | N/A | Truncated base64 |
| Regex | Pattern + flags | N/A | `/pattern/flags` |
| Unknown | `reflect.DeepEqual` + warning | N/A | Go default formatting |

### 5.3 Performance Considerations (v0.1)

For "dozens of docs across several collections" (stated scale), the naive approach works fine: fetch all docs into memory, compare in-process. No streaming, no cursors, no pagination needed.

**v0.1 is deliberately sequential and synchronous.** This makes it trivial to debug, test, and reason about. The per-collection diff logic is already isolated into its own function, which is the only prerequisite for adding concurrency and streaming later — no rewrite, just a new calling pattern.

### 5.4 Concurrency Model (planned — post-MVP)

When the sequential approach becomes noticeably slow (likely at hundreds of collections or thousands of documents), collection-level parallelism is the first optimization to reach for.

**How it works:** Each collection diff is independent — it reads from source and target, compares, and produces a `CollectionDiff`. These can run as parallel goroutines with a bounded worker pool.

```go
// Conceptual — not final implementation

func (d *Differ) diffConcurrent(ctx context.Context, db string, collections []string) []CollectionDiff {
    results := make(chan CollectionDiff, len(collections))
    sem := make(chan struct{}, maxWorkers) // e.g. 5 concurrent collections

    for _, coll := range collections {
        sem <- struct{}{}
        go func(name string) {
            defer func() { <-sem }()
            diff := d.diffCollection(ctx, db, name) // same function used by sequential path
            results <- diff
        }(coll)
    }

    // collect results...
}
```

**Why collection-level and not document-level:** Document comparison within a single collection is fast (it's in-memory struct walking). The bottleneck at scale is the number of collections and the network round-trips to fetch documents from each. Parallelizing at the collection level gives the biggest speedup with the least complexity.

**Concurrency limits:** The worker pool is bounded (default 5, configurable via `--workers`) to avoid overwhelming either Mongo instance with too many concurrent read operations.

**When to build this:** When a `mongodiff diff` run takes more than 2–3 seconds and the bottleneck is clearly I/O-bound (waiting on Mongo reads), not CPU-bound (comparing docs). Profile first, parallelize second.

### 5.5 Progressive Streaming (planned — post-MVP)

Instead of waiting for the entire diff to complete before showing output, progressive streaming emits results as they're computed. This is a UX improvement, not a performance improvement — the total time is the same, but perceived latency drops significantly.

**The user experience changes from:**
```
[waiting... waiting... 3 seconds... waiting...]
[entire diff dumps to screen at once]
```

**To:**
```
mongodiff — comparing local → staging (database: myapp)

━━━ Collections (scanning...) ━━━━━━━━━━━━━━━━━━
  + feature_flags              (new collection)
  ~ users                      (comparing 142 documents...)
      ~ _id: ObjectId("664b...a1b0")        ← appears as computed
        - preferences.theme: "light"
        + preferences.theme: "dark"
      ~ _id: ObjectId("664b...a1b1")        ← appears next
        ...
  ~ products                   (queued)       ← hasn't started yet
```

**Three levels of progressiveness:**

1. **Collection-level streaming (low effort):** List all collections immediately (fast — just collection names), then diff each one and print results as each completes. This alone eliminates most perceived waiting.

2. **Document-level streaming (medium effort):** Within a collection, emit each document diff as it's computed. Requires the renderer to handle incremental output, but the diff logic stays the same.

3. **Full real-time streaming (higher effort):** Combines concurrency with streaming. Multiple collections diff in parallel, each emitting document-level events through the shared `DiffEvent` channel. The renderer interleaves output from multiple collections. This is the most complex but also the most satisfying UX — the screen fills up like a live build log.

**How it composes with each interface:**

| Interface   | Streaming mechanism            | UX result                                    |
|-------------|-------------------------------|----------------------------------------------|
| CLI         | Print to stdout as events arrive | Terminal scrolls with results in real-time  |
| HTTP Server | Server-Sent Events (SSE)      | `/api/diff/stream` endpoint pushes events    |
| Web UI      | EventSource consuming SSE     | Diff view populates progressively, collections expand as results arrive |

**Implementation path:** The `DiffStream()` method (defined in Section 3.3) returns a `<-chan DiffEvent`. The CLI renderer switches from "format full DiffResult" to "consume channel, print each event." The HTTP handler wraps the same channel in SSE framing. The core diff logic (per-collection, per-document functions) doesn't change — only the orchestration layer that calls them.

**When to build this:** When the diff takes long enough that staring at a blank terminal feels wrong — likely around the same time concurrency becomes warranted. Build them together since they share the same channel-based architecture.

These are NOT in v0.1. Don't build them until the naive approach is actually slow.

---

## 6. Release Phases

### Phase 1 — v0.1.0: Diff (MVP)

**Goal:** See what's different between two Mongo databases.

| Component           | Status   |
|---------------------|----------|
| Core diff engine    | Build    |
| Terminal renderer   | Build    |
| CLI `diff` command  | Build    |
| Connection via args | Build    |
| Include/exclude     | Build    |
| Unit tests          | Build    |
| README              | Build    |

**Ship criteria:** You can run `mongodiff diff --source ... --target ... --db myapp` and see a useful, correct diff before your Monday PR.

**What is explicitly NOT in v0.1:**
- No sync/apply
- No dry-run
- No backups
- No server
- No web UI
- No history
- No config file
- No JSON output

### Phase 2 — v0.2.0: Sync

**Goal:** Apply the diff from source to target safely.

| Component                    | Status  |
|------------------------------|---------|
| `sync` command               | Build   |
| Dry-run mode                 | Build   |
| Pre-sync backup (.bson dump) | Build   |
| Confirmation prompt          | Build   |
| Restore from backup command  | Build   |

**Ship criteria:** You can run `mongodiff sync --dry-run`, verify the plan, then run `mongodiff sync` and have staging match local.

### Phase 3 — v0.3.0: Server + Web UI

**Goal:** Make the tool accessible to the team without CLI installation.

| Component                  | Status  |
|----------------------------|---------|
| HTTP server                | Build   |
| REST API (`/api/diff`, `/api/sync`) | Build   |
| Web UI (embedded SPA)      | Build   |
| Side-by-side diff view     | Build   |
| `serve` command            | Build   |

**Ship criteria:** A teammate can open `http://localhost:8080`, paste two connection strings, and see the diff in a browser.

### Phase 4 — v0.4.0+: Future (Do Not Build Yet)

These are ideas, not commitments. Each one gets built only when there's a real pain point driving it.

- **Concurrent diffing** — Parallelize collection comparisons using a bounded goroutine worker pool. Build when sequential diffing takes >2–3 seconds. See Section 5.4 for design.
- **Progressive streaming** — Emit diff results to the terminal/UI as they're computed instead of waiting for full completion. Pairs with concurrency. See Section 5.5 for design and the streaming API in Section 3.3.
- **History/audit log** — Track what was synced, when, by whom. Store as local JSON or SQLite.
- **Snapshots** — Save a point-in-time state of a database, diff against snapshots later.
- **Environment profiles** — Named configs (local→staging, staging→prod) in a `.mongodiff.yaml` file.
- **PR integration** — GitHub Action that runs `mongodiff diff` and posts the output as a PR comment.
- **JSON output mode** — For piping into other tools or CI.
- **Index diffing** — Compare indexes between source and target (was mentioned in initial requirements but deferred).
- **Schema inference** — Detect structural patterns across documents in a collection and diff the "shape" of a collection, not just individual documents.

---

## 7. Technical Decisions

| Decision                  | Choice                          | Rationale                                                     |
|---------------------------|---------------------------------|---------------------------------------------------------------|
| Language                  | Go                              | Single binary, fast, good Mongo driver, no runtime deps       |
| Document matching         | `_id` only (ObjectId type)      | Covers the actual use case, avoids complexity of composite keys. Mongoose codebase uses ObjectId. |
| Field comparison          | Full document, no ignored fields| No surprises. If `updatedAt` or `__v` differs, you want to know. Future `--ignore-fields` flag if noisy. |
| Type comparison           | Strict (int32 ≠ int64 ≠ double) | Catches real bugs from driver serialization differences between Node.js and Go |
| Null vs absent            | Distinct states                 | `null`, absent, and empty are three different things. Matches Mongoose data patterns. |
| Key order in documents    | Ignored (semantic equality)     | `{a:1, b:2}` equals `{b:2, a:1}`. Prevents false diffs from insertion order variance. |
| Array diffing             | Positional (index-based)        | Simple, correct for config data. Content-based (LCS) is a future concern. |
| Database scope            | One database per run            | Matches the workflow: one feature, one database                |
| Config                    | CLI args + env vars             | No config file for v0.1. Reduces setup friction to zero.      |
| CLI framework             | cobra                           | Standard for Go CLIs (kubectl, docker, gh all use it)         |
| Output                    | Color-coded terminal text       | Familiar to anyone who uses `git diff`                        |
| Connection handling        | Short-lived per run             | Connect, diff, disconnect. No connection pooling needed.      |
| Execution model           | Sequential, synchronous (v0.1)  | Simplest to debug/test. Concurrency & streaming designed but deferred (see 5.4, 5.5) |

---

## 8. Error Handling

The tool should handle these scenarios gracefully:

- **Connection failure** — Clear message: "Could not connect to source: connection refused at localhost:27017". No stack traces.
- **Auth failure** — "Authentication failed for target. Check your connection string credentials."
- **Database doesn't exist** — "Database 'myapp' not found on source. Available databases: admin, config, local, myapp_v2"
- **Timeout** — Configurable via `--timeout` flag (default 30s). "Timed out after 30s reading collection 'users' from target."
- **Permission denied on collection** — Skip the collection, warn the user, continue diffing the rest.

The principle: **never crash silently, never dump a stack trace, always suggest what to do next.**

---

## 9. Testing Strategy

### Unit Tests (pkg/diff)
Test the diff engine against in-memory BSON structures. No real MongoDB needed. Cover: identical docs, added fields, removed fields, modified fields, nested objects, arrays, type changes, nil/missing values.

### Integration Tests (with Docker)
Spin up two Mongo containers, seed them with known data, run the differ, assert the output. These run in CI only. Use `testcontainers-go` for lifecycle management.

### Manual Testing
Use your actual local Docker Mongo and staging to validate real-world output during development. This is your v0.1 acceptance test.

---

## 10. Build & Distribution

```makefile
# Build for current platform
make build

# Build for all platforms (for future distribution)
make build-all
# Produces: bin/mongodiff-linux-amd64, bin/mongodiff-darwin-amd64, bin/mongodiff-darwin-arm64

# Run tests
make test

# Run integration tests (requires Docker)
make test-integration
```

For v0.1, `go build` is the distribution strategy. You build it, you put it in your PATH, done. Binary releases on GitHub come with open-sourcing in Phase 3 or 4.

---

## 11. Security Considerations

- Connection strings may contain credentials. The tool **never logs full connection strings** — it redacts passwords in all output.
- The tool treats source as read-only in v0.1 (diff only). In v0.2 (sync), the target is written to. Source is always read-only.
- No data is sent anywhere. The tool connects directly to the two Mongo instances and keeps everything local.
- For staging/production connections that require TLS or auth mechanisms beyond connection string, the standard Mongo connection string options (`?tls=true&authSource=admin` etc.) are supported natively by the Go driver.

---

## 12. Non-Goals

To be explicit about what this tool is **not**:

- **Not a migration tool.** It doesn't manage schema migrations over time like `migrate` or `goose`. It compares two live databases.
- **Not a replication tool.** It doesn't continuously sync. It's a point-in-time snapshot comparison.
- **Not a backup tool.** The pre-sync backup in v0.2 is a safety net, not a backup strategy.
- **Not an ORM or query tool.** It doesn't help you write queries or model data.

---

## 13. Success Criteria

The tool is successful when:

1. **v0.1:** You run it before a PR and it catches a document you forgot to add to staging. That's it. That's the win.
2. **v0.2:** You run sync instead of manually inserting documents into staging via Compass or mongosh.
3. **v0.3:** Your teammate uses the web UI to check their own changes without asking you for help.

If v0.1 doesn't save you from a missed change within the first two weeks, something is wrong with the tool, not the roadmap.
