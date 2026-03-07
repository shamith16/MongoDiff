# mongodiff

A CLI tool to diff MongoDB databases. See what's different between two Mongo instances before your PR breaks staging.

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

# Use environment variables
MONGODIFF_SOURCE="mongodb://localhost:27017" \
MONGODIFF_TARGET="mongodb://staging.example.com:27017" \
mongodiff diff --db myapp
```

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

| Flag | Description | Default |
|------|-------------|---------|
| `--source` | Source MongoDB URI | (required, or `MONGODIFF_SOURCE` env) |
| `--target` | Target MongoDB URI | (required, or `MONGODIFF_TARGET` env) |
| `--db` | Database name | (required) |
| `--include` | Comma-separated collections to include | all |
| `--exclude` | Comma-separated collections to exclude | none |
| `--timeout` | Connection timeout in seconds | 30 |

## Comparison Rules

- **Type-strict**: `int32(3)` ≠ `int64(3)` ≠ `double(3.0)` — catches real serialization bugs
- **Null vs absent**: `null`, absent, and `[]` are three different states
- **Key-order independent**: `{a:1, b:2}` equals `{b:2, a:1}`
- **Positional arrays**: compared index-by-index
- **All BSON types**: ObjectId, DateTime, Decimal128, Binary, Regex, and more

## v0.1.0

This is the MVP. It does one thing: shows you what's different. No sync, no server, no web UI.

## License

MIT
