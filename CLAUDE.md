# mongodiff - Project Guidelines

## Source of Truth
- `techspec.md` is the authoritative source for all implementation decisions
- If context is ever lost or the session restarts, read `docs/plans/TRACKER.md` first to understand where things left off

## Commit Style
- Small, focused commits — one logical change per commit
- Use conventional commit prefixes: `feat:`, `fix:`, `test:`, `docs:`, `chore:`, `refactor:`
- Never bundle unrelated changes

## Architecture
- Go project using cobra for CLI
- Core library in `pkg/diff/` has no I/O opinions
- CLI in `internal/cli/` is a thin wrapper
- Output renderers in `pkg/output/`
- MongoDB client wrapper in `pkg/mongo/`

## v0.1.0 Scope
- Only the `diff` command — no sync, no server, no web UI, no history
- Sequential, synchronous execution
- Color-coded terminal output
