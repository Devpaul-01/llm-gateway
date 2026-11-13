# Contributing to llm-gateway

Thanks for considering a contribution. This project is MIT-licensed and open
to pull requests, issues, and discussion.

## Before you start

For anything beyond a small fix (a new feature, a change to the admission
control model, a new provider adapter), please open an issue first to agree
on the approach before investing time in a PR. For typos, doc fixes, and
small bug fixes, a PR on its own is fine.

## Project layout

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full package-by-package
breakdown. In short:

- `internal/` — the Go backend (gateway, providers, admission control, admin
  API, metrics)
- `web/` — the Gateway Console, a Vite + React + TypeScript admin UI
- `internal/db/migrations/` — versioned SQL migrations (Postgres)

## Local setup

### Backend (Go)

Requires Go (version pinned in `go.mod`), Postgres, and Redis. The fastest
way to get all three running together is:

```bash
docker compose up --build
```

See the [Quickstart](README.md#quickstart) in the README for the full env
var list (`DATABASE_URL`, `REDIS_URL`, `ENCRYPTION_KEY`, `ADMIN_TOKEN`, at
minimum).

### Frontend (web/)

```bash
cd web
npm install
npm run dev
```

## Running checks before you open a PR

These are the exact checks CI runs on every push and PR — running them
locally first saves a round trip:

```bash
go build ./...
go vet ./...
gofmt -l .        # must print nothing
go test ./...
go test -race ./...
```

For the frontend:

```bash
cd web
npm run lint
npm test
npm run build
```

## Commit and PR conventions

- Keep PRs focused and reviewable — one logical change per PR rather than
  several unrelated changes bundled together.
- Write commit messages that describe *why*, not just *what*, when the
  reasoning isn't obvious from the diff alone.
- If your change affects behavior described in README.md,
  ARCHITECTURE.md, PRODUCT_OVERVIEW.md, or SECURITY.md, please update the
  relevant doc in the same PR. Keeping docs and code in sync is something
  this project takes seriously.
- Add or update tests for any behavior change. New admission-control logic,
  new provider adapters, and new failover paths should all be covered by
  tests, not just manually verified.

## Reporting bugs and requesting features

Please use the GitHub issue templates (Bug report / Feature request) so the
basic context (environment, steps to reproduce, expected vs. actual
behavior) is captured up front.

## Reporting security issues

Do **not** open a public issue for a security vulnerability. See
[SECURITY.md](SECURITY.md) for how to report one privately.

## Code of conduct

Participation in this project is governed by the
[Code of Conduct](CODE_OF_CONDUCT.md).
