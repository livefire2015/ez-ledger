# Repository Guidelines

## Project Structure & Module Organization
Source lives in `src`: domain structs in `src/models`, orchestration and persistence logic in `src/services`. SQL schema files live in `migrations/` (apply `001_create_ledger_tables.sql` first). Usage snippets are in `examples/`, technical write-ups in `docs/`, and AI-collaboration rules in `CLAUDE.md`. Unit tests sit under `tests/unit`, matching the package name they exercise.

## Build, Test, and Development Commands
Run `go mod download` to sync deps. Execute `go test ./...` for the full suite or `go test ./tests/unit -run TestPaymentLifecycle` for a focused loop. `go vet ./...` is required before PRs; format changes with `gofmt -w` or `goimports`. Prepare PostgreSQL by running `psql ezledger < migrations/001_create_ledger_tables.sql` so service code can persist ledger entries.

## Coding Style & Naming Conventions
Use standard Go style: tabs, `gofmt`, and concise lowercase package names. Exported APIs are PascalCase; helpers stay lowerCamelCase. Keep DTOs in `src/models`, orchestration + persistence in `src/services`, and maintain strict separation between statement and points logic. Functions that hit external systems should accept `context.Context` and wrap errors with `%w`.

## Testing Guidelines
Unit tests use Go’s `testing` package with table-driven cases (`tests/unit/*.go`). Name files `*_test.go` and mirror the exported symbol in each `Test...`. Assert both statement and points balances for every scenario, seed timestamps explicitly, and avoid randomness or network calls. Run `go test ./...` locally before every push.

## Commit & Pull Request Guidelines
Follow the repo pattern: `type(scope): summary` such as `feat(models): add billing cycle model`. Accepted types include `feat`, `fix`, `docs`, `test`, and `chore`. Pull requests must summarize behavior changes, mention DB migrations, link issues, and attach logs or screenshots when UX/output shifts. Confirm `go test ./...` and `go vet ./...` succeed before assigning reviewers.

## Security & Configuration Tips
Store secrets in environment variables (e.g., `DATABASE_URL`) and keep `.env*` files local. Run migrations against disposable databases when prototyping, and prefer UUID tenant IDs via `github.com/google/uuid`. Never mutate ledger rows—append corrective entries to preserve immutability and auditability.
