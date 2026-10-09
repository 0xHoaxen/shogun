# Shogun

A private, single-user web app that manages a professional life: job tracker, contacts and outreach, Gmail job-mail detection, AI-drafted messages and posts in the owner's voice, a learning log, GitHub-driven resume suggestions, job discovery, analytics, notifications and cost control.

**Hard product rule:** nothing leaves the system (email, DM, post) without the owner approving that exact draft version.

Ten Go services over gRPC behind one gateway (`torii`), one Postgres 17 database with a schema per service, events through a transactional outbox, and a Next.js web UI.

## Prerequisites

- Go 1.26.9 (see `.tool-versions`)
- Docker (integration tests use Postgres via testcontainers)
- Node 22 (web app, from Phase 5)

## Quickstart

```sh
make tools   # install pinned buf, protoc plugins, goose, sqlc and golangci-lint into ./bin
make lint    # golangci-lint on every module, buf lint
make test    # go test -race on every module
make help    # all targets
```

## Where to look

| Need | File |
| --- | --- |
| Rules and conventions for agents and humans | [AGENTS.md](AGENTS.md) |
| How Claude Code works in this repo | [CLAUDE.md](CLAUDE.md) |
| What to build next | [TODO.md](TODO.md) |
| Repo layout and where new code goes | [docs/folder-structure.md](docs/folder-structure.md) |

## Design docs

- Shogun LLD: TODO(owner): add link
- Shogun System Design: TODO(owner): add link
