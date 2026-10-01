# AGENTS.md

Guidance for any coding agent (and humans) working in this repo. Read this first. Deeper design lives in the Shogun LLD and System Design docs; the repo layout is in `docs/folder-structure.md`; the work queue is `TODO.md`.

## What Shogun is

A private, single-user web app that manages a professional life: job tracker, contacts and outreach, Gmail job-mail detection, AI-drafted messages and posts in the owner's voice, a learning log, GitHub-driven resume suggestions, job discovery, analytics, notifications and cost control.

**Hard product rule:** nothing leaves the system (email, DM, post) without the owner approving that exact draft version. Automation is allowed for internal events only.

## Architecture in one screen

Ten Go services over gRPC behind one gateway, one Postgres 17 database with a schema and a login role per service, events through a transactional outbox. Web UI is Next.js and talks only to `torii`.

| Service | Role | Postgres schema |
| --- | --- | --- |
| `torii` | gateway: ConnectRPC for the web app, sessions, Google OAuth login | `torii` |
| `kagami` | jobs, companies, contacts, timelines, follow-ups, CSV import | `kagami` |
| `tsubame` | Gmail sync and classification; the only service that sends email | `tsubame` |
| `fude` | AI drafts, versions, approval queue, mints the Hanko token | `fude` |
| `taiko` | notifications, live stream, daily digest | `taiko` |
| `dojo` | learning items and activities | `dojo` |
| `katana` | GitHub sync, resume and LinkedIn suggestions | `katana` |
| `shinobi` | job discovery from user-provided sources, scoring | `shinobi` |
| `sensei` | analytics projected from events | `sensei` |
| `soroban` | token metering, budgets, spend limits | `soroban` |

`pkg/hanko` is the approval-token library, not a service. Names are final; use them in folders, protos (`shogun.<name>.v1`), schemas, env vars (`<NAME>_ADDR`), images and tags. Event types use domain nouns (`job.added`, `draft.sent`), never service names.

## Commands

```sh
make tools        # install pinned buf, protoc plugins, goose, sqlc into ./bin
make proto        # buf lint + buf generate into gen/go and web/src/gen
make lint         # golangci-lint on every module, buf lint
make test         # go test -race ./... on every module (needs Docker for Postgres)
make build        # build every service binary into ./dist
make up / down    # docker compose local stack
make migrate      # apply all service migrations to the local database
make new-service NAME=<name>      # generate a service skeleton
make rename-service OLD=a NEW=b   # rename a service everywhere
```

Run a single module: `cd services/kagami && go test -race ./...`. The repo is a `go.work` workspace; each module also builds with `GOWORK=off`.

## Rules that must not be broken

1. **Service isolation.** A service imports only `pkg` and `gen/go`, never another service. Cross-service calls use generated gRPC clients through `pkg/grpcclient`. Enforced by `depguard`.
2. **Database isolation.** A service touches only its own schema. No foreign key crosses a schema; reference another service's row by plain `uuid` and resolve it through that service's API.
3. **Events via outbox.** Write the event with `outbox.Write` in the same transaction as the state change. Never publish from outside a transaction, never call another service to "notify" it. Consumers must be idempotent (dedupe on event id via `inbox`).
4. **No external send without Hanko.** Only `fude.Approve` signs a Hanko token; only `tsubame.Send` sends email and it verifies the token. Do not add another code path to either. Posts for LinkedIn and X are copy-only and never go through `tsubame`.
5. **All Claude calls go through `pkg/llm`.** It reserves and commits cost with `soroban`. Never import the Anthropic SDK directly in a service. Fail closed if `soroban` is unavailable.
6. **Generated code is never edited.** `gen/go` and `web/src/gen` come from `make proto`; commit regenerated output.
7. **Migrations are append-only.** Never edit a migration that has been merged; add a new one. SQL uses goose format in `services/<name>/migrations/`.
8. **Secrets never enter the repo.** Config comes from environment variables; only `.env.example` is committed. Never log message bodies, tokens or email addresses.
9. **Proto compatibility.** Inside `v1`, add fields freely, never renumber or remove. `buf breaking` runs in CI.

## Conventions

- **Go:** `gofumpt` and `goimports` (local prefix `github.com/sboy99/shogun`), `golangci-lint` clean. Wrap errors with `%w`. Pass `context.Context` first. No globals except in `main`.
- **Layers inside a service:** `cmd/<name>` wires; `internal/app` use cases; `internal/domain` pure types and state machines (no I/O); `internal/store` Postgres via sqlc; `internal/transport/grpc` handlers; `internal/events` inbox handlers; `internal/jobs` River workers. Dependencies point inward.
- **IDs and data:** UUIDv7 generated in Go; `timestamptz` UTC; money as `bigint` micro-dollars; enums as `text` with `CHECK`; every user-facing table has `owner_id`, `version`, `created_at`, `updated_at`, `archived_at`.
- **Errors:** gRPC status codes with a stable `ErrorInfo.reason` such as `JOB_STATUS_INVALID_TRANSITION`. Invalid state change is `FailedPrecondition`.
- **Updates:** `FieldMask` plus optimistic `version` check. Lists use `page_size` and `page_token`.
- **Tests:** table-driven unit tests for `domain` and `app`; integration tests for `store` use a real Postgres via testcontainers, never mocks of the database. Every new RPC gets a handler test; every new event gets a producer and a consumer test.
- **Logging:** `slog` JSON from `pkg/logger`; include `trace_id` and `request_id`.
- **Commits:** Conventional Commits with the module as scope, for example `feat(kagami): import contacts`. One logical change per commit. PR titles follow the same format. Release tags are per module (`services/kagami/v0.4.0`).

## Working on a task

1. Open `TODO.md`, pick the first unchecked task whose `Needs:` are all checked.
2. Read the files it names and the matching section of the design docs before editing.
3. Make the smallest change that satisfies `Done when:`; run exactly the commands listed there.
4. Tick the task in `TODO.md` in the same commit.
5. If a task is ambiguous or contradicts a rule above, stop and ask instead of guessing.

## Do not

- Add a dependency without saying why in the PR description.
- Add a service, table or event type that is not in the design docs without updating the docs first.
- Skip, disable or weaken a test or lint rule to get green.
- Store anything the owner has not asked to be stored; all data stays in the owner's own database.
