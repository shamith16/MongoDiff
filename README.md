# mongodiff

A CLI tool to diff and sync MongoDB databases. See what's different between two Mongo instances before your PR breaks staging.

## Install

```bash
go install github.com/shamith/mongodiff/cmd/mongodiff@latest
```

Or build from source:

```bash
git clone https://github.com/shamith/mongodiff.git
cd mongodiff
make build
```

## Usage

### Diff

```bash
# Basic usage
mongodiff diff \
  --source "mongodb://localhost:27017" \
  --target "mongodb://staging.example.com:27017" \
  --db myapp

# Filter collections
mongodiff diff \
  --source "mongodb://localhost:27017" \
  --target "mongodb://staging.example.com:27017" \
  --db myapp \
  --include users,products \
  --exclude sessions,logs

# JSON output
mongodiff diff --source ... --target ... --db myapp --output json

# Summary only
mongodiff diff --source ... --target ... --db myapp --summary-only

# Ignore specific fields
mongodiff diff --source ... --target ... --db myapp --ignore-fields updatedAt,__v

# Use environment variables
MONGODIFF_SOURCE="mongodb://localhost:27017" \
MONGODIFF_TARGET="mongodb://staging.example.com:27017" \
mongodiff diff --db myapp
```

### Sync

Apply diffs from source to target. Source is never modified.

```bash
# Dry run — show what would change
mongodiff sync \
  --source "mongodb://localhost:27017" \
  --target "mongodb://staging.example.com:27017" \
  --db myapp \
  --dry-run

# Apply changes (with confirmation prompt and automatic backup)
mongodiff sync \
  --source "mongodb://localhost:27017" \
  --target "mongodb://staging.example.com:27017" \
  --db myapp
```

Backups are saved to `.mongodiff/backups/` as JSON before any changes are applied.

### Web UI

```bash
# Start the web server (default port 8080)
mongodiff serve

# Custom port
mongodiff serve --port 3000
```

Open `http://localhost:8080` for a browser-based UI with connection form, diff viewer, and sync controls.

## Output

mongodiff produces color-coded terminal output showing:
- **Collections**: added (+, green), removed (-, red), modified (~, yellow), identical (gray)
- **Documents**: added, removed, or modified with field-level diffs
- **Fields**: dot-notation paths with old/new values, type changes detected

```
mongodiff — comparing localhost → staging (database: myapp)

━━━ Collections ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  + feature_flags              (new collection, 3 documents)
  ~ users                      (2 modified, 1 added)
    orders                     (identical)

━━━ Collection: users ━━━━━━━━━━━━━━━━━━━━━━━━━━━

  ~ _id: ObjectId("664b...a1b0")
    - preferences.theme: "light"
    + preferences.theme: "dark"

━━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  1 collection added, 1 collection modified, 1 identical
  3 documents added, 2 documents modified
```

## Flags

### diff

| Flag | Description | Default |
|------|-------------|---------|
| `--source` | Source MongoDB URI | (required, or `MONGODIFF_SOURCE` env) |
| `--target` | Target MongoDB URI | (required, or `MONGODIFF_TARGET` env) |
| `--db` | Database name | (required) |
| `--include` | Comma-separated collections to include | all |
| `--exclude` | Comma-separated collections to exclude | none |
| `--timeout` | Connection timeout in seconds | 30 |
| `--output` | Output format: `terminal` or `json` | terminal |
| `--summary-only` | Show only the summary, no field-level details | false |
| `--ignore-fields` | Comma-separated field paths to ignore in comparison | none |

### sync

| Flag | Description | Default |
|------|-------------|---------|
| `--source` | Source MongoDB URI | (required, or `MONGODIFF_SOURCE` env) |
| `--target` | Target MongoDB URI | (required, or `MONGODIFF_TARGET` env) |
| `--db` | Database name | (required) |
| `--include` | Comma-separated collections to include | all |
| `--exclude` | Comma-separated collections to exclude | none |
| `--timeout` | Connection timeout in seconds | 30 |
| `--dry-run` | Show sync plan without applying | false |
| `--ignore-fields` | Comma-separated field paths to ignore in comparison | none |

### serve

| Flag | Description | Default |
|------|-------------|---------|
| `--port` | Port to listen on | 8080 |

## Comparison Rules

- **Type-strict**: `int32(3)` ≠ `int64(3)` ≠ `double(3.0)` — catches real serialization bugs
- **Null vs absent**: `null`, absent, and `[]` are three different states
- **Key-order independent**: `{a:1, b:2}` equals `{b:2, a:1}`
- **Positional arrays**: compared index-by-index
- **All BSON types**: ObjectId, DateTime, Decimal128, Binary, Regex, and more

## License

MIT
