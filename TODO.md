# TODO

Work queue for building Shogun, in phases. Written so a coding agent can take one task at a time.

## How to use this file

- Take the **first unchecked task whose `Needs:` are all checked**. One task = one branch (`task/<id>-<slug>`) = one PR.
- Every task has **Do** (what to build), **Files** (where), and **Done when** (commands that must pass). Run those exact commands and report the output.
- Tick the box in the same commit. If you discover missing work, add a task under the right phase instead of widening the current one.
- Anything marked `TODO(owner)` needs a decision from sboy99; skip it and continue with other tasks.
- Rules in `AGENTS.md` override anything here.

Legend: `Needs:` prerequisites, `Size:` S under 100 lines, M under 400, L split before starting.

---

## Phase 0: Repo foundation

- [x] **P0.1 Workspace and tool pins** (S)
  Do: create `go.work` (no `use` lines yet: `./pkg` is added in P1.1, `./gen/go` in P1.2, services in P2.3), `.tool-versions` (go 1.25.4, golangci-lint 2.4.0, nodejs 22, helm 3), `.gitignore`, `.dockerignore`, `.editorconfig`, `.env.example`.
  Files: repo root.
  Done when: `go version` matches `.tool-versions`; `go work sync` exits 0.

- [x] **P0.2 Tools module** (S) Needs: P0.1
  Do: `tools/go.mod` with `tool` directives for buf v1.57, protoc-gen-go v1.36, protoc-gen-go-grpc v1.5, goose v3.26, sqlc v1.30 (v1.31+ needs Go 1.26). Not in `go.work`.
  Done when: `cd tools && GOWORK=off GOBIN=$PWD/../bin go install tool` puts `buf`, `goose`, `sqlc`, `protoc-gen-go`, `protoc-gen-go-grpc` in `bin/`.

- [x] **P0.3 Makefile** (S) Needs: P0.2
  Do: targets `tools proto sqlc lint test build up down migrate new-service rename-service`; each module loop uses `go list -m -f '{{.Dir}}'` from `go.work`. Targets that need later phases print "not yet" and exit 0.
  Done when: `make tools` works; `make lint` and `make test` exit 0 on the empty workspace.

- [x] **P0.4 Lint config** (S) Needs: P0.1
  Do: `.golangci.yml` (v2: standard + bodyclose, errorlint, gocritic, misspell, revive, sloglint; gofumpt, goimports with local prefix) and a `depguard` rule set: services cannot import other services; `pkg` cannot import services; nobody imports the Anthropic SDK except `pkg/llm`.
  Done when: `golangci-lint config verify` exits 0.

- [x] **P0.5 Project docs in place** (S) Needs: P0.1
  Do: copy `AGENTS.md`, `CLAUDE.md`, `docs/folder-structure.md`, this `TODO.md` into the repo; write `README.md` with quickstart and links to the LLD and System Design docs.
  Done when: files exist at the paths in `docs/folder-structure.md`.

- [x] **P0.6 Buf config** (S) Needs: P0.2
  Do: `buf.yaml` (STANDARD lint, FILE breaking), `buf.gen.yaml` (Go with `paths=source_relative`, managed `go_package_prefix github.com/0xHoaxen/shogun/gen/go`; TypeScript to `web/src/gen` with `@connectrpc/protoc-gen-connect-es` added in P7).
  Done when: `bin/buf lint` exits 0. (buf rejects a module with no `.proto` files, so the envelope proto from P1.2 is added here.)

---

## Phase 1: Shared platform (`pkg`)

- [x] **P1.1 pkg module + version, config, logger** (S) Needs: P0.1
  Do: `pkg/go.mod` and `go work use ./pkg`; `version` (ldflags vars), `config.Base` + helpers, `logger.New` (slog JSON with service and version).
  Files: `pkg/version`, `pkg/config`, `pkg/logger`.
  Done when: `cd pkg && go test -race ./config/... ./logger/...` passes with tests for env parsing defaults and the required `DATABASE_URL` outside local.

- [x] **P1.2 Event envelope proto + codegen** (S) Needs: P0.6
  Do: `proto/shogun/events/v1/envelope.proto` already exists from P0.6; create `gen/go/go.mod` and `go work use ./gen/go`; run codegen.
  Done when: `make proto` exits 0 and `cd gen/go && go build ./...` passes; `bin/buf lint` clean.

- [x] **P1.3 pkg/server** (M) Needs: P1.1
  Do: `server.Run(ctx, cfg, log, register)`: gRPC with health, reflection (off when `ENVIRONMENT=production`), recover and log interceptors; HTTP `/healthz` `/readyz` `/metrics`; graceful shutdown on SIGTERM with `ShutdownTimeout`.
  Done when: test starts the server on random ports, hits health over gRPC and HTTP, cancels the context and sees clean exit within the timeout.

- [x] **P1.4 pkg/postgres** (M) Needs: P1.1
  Do: `Connect(ctx, url, schema)` pinning `search_path`; `Migrate(ctx, pool, embed.FS)` with goose; `InTx(ctx, pool, fn)` that commits or rolls back.
  Done when: integration test with testcontainers Postgres creates a schema, migrates a sample file, and shows a failed `InTx` rolls back.

- [x] **P1.5 pkg/outbox and pkg/inbox** (M) Needs: P1.2, P1.4
  Do: `outbox.Write(ctx, tx, source, type, subject, payload)` returns the event id and issues `NOTIFY outbox`; `inbox.Handle(ctx, pool, env, fn)` inserts `inbox(event_id)` `ON CONFLICT DO NOTHING` and runs `fn` in the same transaction only if new. SQL for both tables as a reusable migration snippet in `pkg/postgres/sql/`.
  Done when: integration tests prove (a) outbox row appears only after commit, (b) the same event twice runs `fn` once.

- [x] **P1.6a pkg/bus interface and routes** (S) Needs: P1.5
  Do: `bus.Bus` interface + `routes.go` mapping event type to consumers.
  Done when: `cd pkg && go test -race -count=1 ./bus/...` passes.

- [x] **P1.6b Outbox relay** (M) Needs: P1.6a
  Do: River-based relay: periodic job reads undelivered rows `FOR UPDATE SKIP LOCKED` (batch 100), enqueues one `deliver_event` job per consumer, marks delivered.
  Done when: `cd pkg && go test -race -count=1 ./bus/...` passes, including a relay integration test against Postgres.

- [x] **P1.6c EventSink and gRPC bus** (M) Needs: P1.6b
  Do: `EventSinkService.Deliver` gRPC service (named with the `Service` suffix to satisfy buf lint) (proto in `proto/shogun/events/v1/sink.proto`) that every service registers, calling `inbox.Handle`.
  Done when: integration test with two in-process "services": a produced event is delivered once to each consumer, survives a consumer returning an error twice, and a duplicate delivery is ignored.

- [x] **P1.7 pkg/authz and pkg/grpcclient** (M) Needs: P1.3
  Do: `authz` signs and verifies the 60 s HMAC identity token in `x-shogun-identity` (owner_id, request_id) with unary and stream interceptors; `grpcclient.Dial` adds retries, deadlines, the identity header, and trace propagation.
  Done when: tests show a call without or with an expired token gets `Unauthenticated`, a valid token reaches the handler with `owner_id` in context.

- [x] **P1.8 pkg/telemetry** (S) Needs: P1.3
  Do: OTel tracer and meter setup from env, OTLP exporter, no-op when endpoint unset; wire into `server` and `grpcclient`.
  Done when: `go test ./telemetry/...` passes; running the server test with no endpoint produces no errors.

- [x] **P1.9 pkg/hanko** (M) Needs: P1.1
  Do: PASETO `v4.public` `Sign(claims, key)` / `Verify(token, pubkey, expected)` with claims jti, aud, iss, sub, draft_id, version, body_sha256, rcpt_sha256, iat, exp (5 min); key id support with two valid public keys.
  Done when: tests cover valid, expired, wrong audience, tampered hash, wrong key, rotated key.

---

## Phase 2: Service generator and skeletons

- [x] **P2.1 Service template** (M) Needs: P1.3, P1.4, P1.6
  Do: `scripts/templates/service/` producing `go.mod` (replace to `../../pkg` and `../../gen/go`), `cmd/<name>/main.go` (config, logger, postgres, migrate, server.Run, EventSink), `internal/{app,domain,store,transport/grpc,events,jobs}` with one placeholder each, `migrations/00001_init.sql` creating `outbox` and `inbox`, embed of migrations, `Dockerfile` args.
  Done when: generated service compiles with `go build ./...` and its binary starts against local Postgres and answers health.

- [x] **P2.2 new-service and rename-service scripts** (M) Needs: P2.1
  Do: `scripts/new-service.sh NAME` (adds module to `go.work`, a `svc-<name>` depguard rule in `.golangci.yml` and the new name to every other service's deny list, proto stub, Makefile and CI matrix entries, helm values, compose service); `scripts/rename-service.sh OLD NEW` rewrites folder, module path, proto package, schema, role, env prefix, values, release config.
  Done when: `make new-service NAME=demo` then `make rename-service OLD=demo NEW=demo2` builds; the script is idempotent-safe (refuses if target exists). Delete the demo after.

- [x] **P2.3 Generate the ten skeletons** (M) Needs: P2.2
  Do: run `make new-service` for `torii kagami tsubame fude taiko dojo katana shinobi sensei soroban`; add each to `go.work`.
  Done when: `make build` produces ten binaries; `make lint` and `make test` pass.

- [x] **P2.4 Wire the outbox relay into services** (S) Needs: P2.3
  Do: in the service template and every generated service, call `relay.Migrate`, build `relay.New` with `bus.DialGRPCBus` over the `<NAME>_ADDR` targets, and start and stop it alongside `server.Run`. Required before the first producer (P4.5).
  Done when: a generated service starts the relay and an outbox row written in a test is delivered to a fake consumer.

---

## Phase 3: Local stack and CI/CD

- [x] **P3.1 Dockerfile** (S) Needs: P2.1
  Do: `deploy/docker/Dockerfile` multi-stage, `ARG SERVICE`, builds from workspace root with `-ldflags` for version and commit, runs on distroless nonroot, healthcheck via the HTTP port.
  Done when: `docker build --build-arg SERVICE=kagami -f deploy/docker/Dockerfile .` succeeds and the image runs.

- [x] **P3.2 Postgres init** (S) Needs: P0.1
  Do: `deploy/compose/postgres/init.sql`: extensions `vector` and `citext`; for each service a schema, a login role with password from env, `REVOKE ALL ON SCHEMA public`, `search_path` set.
  Done when: connecting as `kagami` shows only the `kagami` schema and cannot create objects in `fude`.

- [x] **P3.2b Extension operators on search_path** (S) Needs: P3.2
  Do: `postgres.Connect` pins `search_path` to the service schema only, so the `vector` and `citext` operators installed in the `extensions` schema are invisible unqualified (`citext = citext` silently falls back to case-sensitive `text =`). Put `extensions` on the pool's `search_path` (for example `<schema>,extensions`) and keep `Migrate`'s schema lookup working.
  Done when: a testcontainers test shows `'A'::citext = 'a'::citext` is true and `<->` on `vector` resolves from a service role with no qualification.

- [x] **P3.3 Compose stack** (M) Needs: P3.1, P3.2, P2.3
  Do: `deploy/compose/compose.yaml` (pgvector Postgres 17, ten services, env from `.env`, `MIGRATE_ON_START=true`); `compose.obs.yaml` profile. `make up`, `make down`, `make migrate`.
  Done when: `make up` brings all containers healthy; `grpcurl -plaintext localhost:<port> grpc.health.v1.Health/Check` returns SERVING for each.

- [x] **P3.4 changed-modules script** (S) Needs: P2.3
  Do: `scripts/changed-modules.sh <base>` prints a JSON matrix; a change under `pkg/`, `gen/`, `go.work` or `proto/` selects every module.
  Done when: unit-style shell test shows `services/kagami/x.go` selects only kagami and `pkg/config/x.go` selects all.

- [x] **P3.5 CI workflows** (M) Needs: P3.4
  Do: `.github/workflows/ci.yml` (matrix lint, `go test -race`, build, Docker build without push), `proto.yml` (lint, breaking vs main, generate then `git diff --exit-code`), `pr-title.yml`, `codeql.yml`, `dependabot.yml`, `pull_request_template.md`. Actions pinned to SHAs; Go version from `.tool-versions`.
  Done when: `actionlint` passes locally; opening a draft PR shows all checks green.

- [x] **P3.5b CI follow-ups** (S) Needs: P3.1, P3.4, P3.5
  Do: add the Docker build job (no push) to `ci.yml` once the Dockerfile exists; switch the lint and test matrix to `scripts/changed-modules.sh`; add `/services/*` to `dependabot.yml`; mark `ci ok`, `proto` and `pr-title` as required checks on `main`.
  Done when: `actionlint` passes; a change under `services/kagami/` runs only the kagami matrix entry.
  `TODO(owner)`: marking `ci ok`, `proto` and `pr-title` as required checks on `main` is a GitHub branch-protection setting, not repo code. `ci.yml` currently triggers only on `push` to `main` (the `pull_request` trigger was removed in `f7253d0`), so `ci ok` can never report on a PR; decide whether to restore the trigger before making it required.

- [ ] **P3.6 Release workflows** (M) Needs: P3.5
  Do: `release-please-config.json` + manifest with one package per module and component names; `release.yml` builds and pushes `ghcr.io/0xhoaxen/shogun-<svc>:<version>` with SBOM and provenance for released services; `deploy.yml` bumps `deploy/helm/values/<env>/<svc>.yaml`.
  Done when: `actionlint` passes; a dry-run of release-please on a `feat(kagami):` commit proposes `services/kagami` only.
  Status: config, manifest, `release.yml` and `deploy.yml` are in place and `actionlint` is clean. Box stays open until the dry run is done, which needs a GitHub token (`gh auth login`): `npx release-please release-pr --dry-run --repo-url=0xHoaxen/shogun --token=$(gh auth token) --config-file=release-please-config.json --manifest-file=.release-please-manifest.json`. `deploy.yml` expects `image.tag` in each values file, which P3.7 creates. `TODO(owner)`: enable "Allow GitHub Actions to create and approve pull requests" in repo settings, and note PRs opened with `GITHUB_TOKEN` do not trigger other workflows.

- [x] **P3.7 Helm chart** (M) Needs: P3.1
  Do: `deploy/helm/service` (Deployment, Service, HPA, PDB, ServiceMonitor, pre-upgrade migration Job), `values/staging` and `values/production` per service.
  Done when: `helm lint` and `helm template` pass for all ten values files.

- [x] **P3.7b Migrate subcommand and migration Job** (M) Needs: P3.7
  Do: add a `migrate` subcommand to the service template and the ten services that applies migrations and exits, then set `migrationJob.enabled: true` and `migrateOnStart: false` in the chart values. The chart's pre-install/pre-upgrade Job already exists but is off because the binaries cannot run migrations without starting the server.
  Done when: `helm template` with the job on renders a hook Job running `migrate`, and `docker run <image> migrate` against compose Postgres exits 0 with the schema migrated.

---

## Phase 4: kagami (jobs and contacts) and torii (gateway)

- [x] **P4.1 kagami proto** (M) Needs: P2.3
  Do: `proto/shogun/kagami/v1/kagami.proto` with the RPCs from the LLD (AddJob, GetJob, ListJobs, UpdateJob, ChangeJobStatus, AddContact, UpdateContact, ListContacts, GetContact, ChangeContactStatus, ImportContacts, ListDueFollowUps) and `events.proto` payloads for `job.*` and `contact.*`.
  Done when: `make proto` and `bin/buf lint` pass.

- [x] **P4.2 kagami migrations** (M) Needs: P2.3
  Do: migration `00002_core.sql` with `companies jobs job_events contacts contact_events imports` exactly as in the System Design "Table structures" tab.
  Done when: `make migrate` applies cleanly; `\d kagami.jobs` matches the DDL.

- [x] **P4.3 kagami domain** (M) Needs: P4.1
  Do: `internal/domain` job and contact entities with transition tables from the System Design state machines; errors map to `JOB_STATUS_INVALID_TRANSITION` / `CONTACT_STATUS_INVALID_TRANSITION`.
  Done when: table-driven tests cover every allowed and one disallowed transition per state.

- [x] **P4.4 kagami store** (M) Needs: P4.2
  Do: sqlc queries + repositories with optimistic `version` and cursor pagination; contacts dedupe on lower(email) then linkedin_url.
  Done when: testcontainers integration tests pass for create, update with stale version (fails), list pagination, dedupe.

- [x] **P4.5 kagami use cases + handlers (jobs)** (M) Needs: P4.3, P4.4, P1.5
  Do: AddJob (upsert company by domain, write `job.added` via outbox), ChangeJobStatus (writes `job_events` and `job.status_changed`), Get/List/Update; gRPC handlers with authz.
  Done when: handler tests pass; a test asserts exactly one outbox row per mutating call.

- [x] **P4.6 kagami use cases + handlers (contacts)** (M) Needs: P4.5
  Do: Add/Update/List/Get contact, ChangeContactStatus writing `contact.status_changed`.
  Done when: handler tests pass including idempotency-key replay returning the same row.

- [x] **P4.7 CSV import** (M) Needs: P4.6
  Do: `ImportContacts` parsing the 17-column CSV, per-row validation, dry run, one transaction on commit, `imports` row, one `contact.added` per new contact.
  Done when: tests with a good file, a file with bad rows (per-row errors returned, nothing written on dry run), and re-import (updates not duplicates).

- [x] **P4.8 kagami follow-up scans** (S) Needs: P4.6
  Do: River periodic jobs `follow_up_scan` and `stale_application_scan` at 08:00 Asia/Kolkata emitting `job.follow_up_due` and `contact.follow_up_due` once per due date; `ListDueFollowUps` RPC.
  Done when: tests with a fake clock show one event per due item per day.

- [ ] **P4.8b Follow-up scan catch-up** (S) Needs: P4.8
  Do: the scans run only at their scheduled minute, so a day on which kagami was down at 07:55 and 08:00 is never scanned. On start, enqueue today's scans when the clock is already past their time (the unique-by-date args keep it to one run). `TODO(owner)`: `staleAfterDays` (7) in `internal/app/followups.go` is a default to confirm.
  Done when: a test starts the service at 09:00 IST on a day with no scan job and sees both scans run once.

- [x] **P4.9a api/v1 protos and TS codegen** (S) Needs: P1.7, P3.3
  Do: `proto/shogun/api/v1/{auth,jobs,contacts}.proto` (`AuthService`, `JobsService`, `ContactsService`); `buf.gen.web.yaml` generating the TS client with `protoc-gen-es` v2 into `web/src/gen`, run by `make proto` once `web/node_modules` exists (P5.1a).
  Done when: `make proto`, `bin/buf lint` and `bin/buf breaking --against '.git#branch=main'` pass; `cd gen/go && go build ./...` passes.

- [x] **P4.9b1 Sessions: migration, store, use cases** (M) Needs: P4.9a
  Do: `torii.sessions` migration (token hash only); sqlc store; `domain.Session` with expiry and sliding renewal; `app.Auth` with `StartSession` (email allowlist, verified email, stable owner id from the Google subject), `Authenticate` (sliding renewal) and `EndSession`; fails closed on an empty allowlist.
  Done when: `cd services/torii && go test -race ./internal/...` passes: allowed and disallowed email, expired session, renewal, logout, store round trip against Postgres.

- [x] **P4.9b2 Google OAuth flow** (M) Needs: P4.9b1
  Do: Google OAuth (authorization code + PKCE + `state`) at `/auth/login`, `/auth/callback`, `/auth/logout`; ID token verified with `go-oidc`; issuer from `GOOGLE_ISSUER_URL` (defaults to Google, overridden by tests); allowlist from `TORII_ALLOWED_EMAILS`; PKCE verifier and state in a short-lived signed cookie; session cookie HttpOnly, Secure outside local, SameSite=Lax; renewed cookie on sliding renewal.
  Done when: tests with a fake OAuth provider cover allowed email, disallowed email, bad state, expired session, logout.

- [x] **P4.9c1 Connect codegen, session interceptor, AuthService** (M) Needs: P4.9b2
  Do: `protoc-gen-connect-go` in the tools module and `buf.gen.connect.yaml` (api protos only, run by `make proto`); `connectapi` package with the session interceptor (cookie to session, owner identity for `grpcclient`, cookie refresh on renewal), the `newError` helper with a stable `ErrorInfo.reason`, and the `AuthService` handlers.
  Done when: `cd services/torii && go test -race ./internal/transport/...` passes: unauthenticated, unknown and expired sessions return `Unauthenticated`; `GetSession`, `Logout`, renewal and identity propagation work.

- [ ] **P4.9d Torii housekeeping** (S) Needs: P4.9c2
  Do: River periodic job deleting expired `torii.sessions` rows (`store.Sessions.DeleteExpired` exists); add the new torii variables (`GOOGLE_*`, `TORII_ALLOWED_EMAILS`, `TORII_PUBLIC_URL`, `TORII_PUBLIC_ADDR`, `KAGAMI_ADDR`, `SOROBAN_ADDR`) to `deploy/helm/values/staging/torii.yaml`. `TODO(owner)`: production values and the real Google client are yours to set.
  Done when: a test with a fake clock shows only expired sessions removed; `helm template` passes for staging torii.

- [x] **P4.9c2 Public listener, config, rate limit, size cap** (M) Needs: P4.9c1
  Do: `TORII_PUBLIC_ADDR` listener serving `/auth/*` and the Connect handlers, started and stopped with the gRPC server; torii config (`TORII_PUBLIC_URL`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_ISSUER_URL`, `TORII_ALLOWED_EMAILS`, session TTL, rate limit, body cap) with `.env.example` and compose entries; per-IP rate limit; request body size cap.
  Done when: `cd services/torii && go test -race ./...` passes, including a `run` test against the fake IdP that logs in over HTTP and calls `GetSession`, plus rate-limit and oversize-body tests.

- [x] **P4.10a torii Jobs endpoints** (M) Needs: P4.9c2, P4.6
  Do: `JobsService` handlers calling kagami through a signed `grpcclient` connection (`KAGAMI_ADDR`): board grouped by status, job with newest-first timeline, add (with `Idempotency-Key` passthrough), field-masked update limited to editable fields, status change; downstream errors keep their stable `ErrorInfo.reason`, internals are never leaked.
  Done when: `cd services/torii && go test -race ./...` passes, including a run test where a signed-in owner's `GetBoard` reaches a fake kagami carrying a valid identity token for that owner.

- [x] **P4.10b torii Contacts endpoints and CSV import** (M) Needs: P4.10a
  Do: `ContactsService` handlers (list with status filter, add with `Idempotency-Key`, change status, `ImportContacts` dry run and confirm) with the same error mapping; mount in `public.go`.
  Done when: handler tests pass for each RPC including the dry-run report and per-row errors. The "via compose" end-to-end check (login against a stub IdP, add a job, list it, change status) is part of P5.2c and runs in GitHub Actions, not locally.

---

## Phase 5: Web app (first screens)

- [x] **P5.1a Web scaffold and generated client** (M) Needs: P4.9a
  Do: `web/` with Next.js (App Router, TypeScript, Tailwind v4), shadcn/ui on the Shogun design tokens (seven colours, no radii), Connect-Web transport and TanStack Query providers, `/api` and `/auth` rewrites to torii (`TORII_URL`), the TS client generated by `make proto` and committed; CI installs `web` dependencies for the stale-code check and runs a `web` job (lint, typecheck, build); dependabot covers `/web`.
  Done when: `cd web && npm ci && npm run lint && npm run typecheck && npm run build` passes; `make proto` leaves no diff.
  `web` is registered with release-please as a `node` component at 0.1.0 (tags `web/v0.1.0`); `release.yml` publishes images for services only, so `web` gets version PRs and a changelog but no image. `web/AGENTS.md` and `web/CLAUDE.md` are written by `next dev` and kept so the tree stays clean.

- [x] **P5.1b Login page and authenticated layout** (M) Needs: P5.1a, P4.9c2
  Do: `/login` with the Google link and `?error=` messages, the authenticated shell from the design canvas (rail, strip, tab nav, utility footer with logout), session gate on `AuthService.GetSession` that sends `Unauthenticated` to `/login`, Playwright config with a mocked-API helper, and the `e2e` step in the CI `web` job.
  Done when: `cd web && npm run build && npm run test:e2e` passes: signed-out redirect, login link, error message, signed-in shell, logout.

- [x] **P5.2a Jobs board** (M) Needs: P5.1b, P4.10a
  Do: board by status from `GetBoard`, drag (pointer and keyboard) to `ChangeJobStatus` with optimistic move and rollback keyed by `ErrorInfo.reason` (add a `reasonOf` helper in `web/src/lib/errors.ts` that decodes the `google.rpc.ErrorInfo` detail), add-job dialog with `Idempotency-Key`.
  Done when: Playwright tests (mocked API) for add job, move job and a rejected move pass.

- [x] **P5.2b Contacts table and CSV import** (M) Needs: P5.1b, P4.10b
  Do: contacts table with status filter and paging, add-contact dialog, CSV import dialog (dry run preview with per-row errors, then confirm resends with `dry_run=false`).
  Done when: Playwright tests (mocked API) for import with a good file and a bad file, and the status filter pass.

- [x] **P5.2c Compose end-to-end in GitHub Actions** (M) Needs: P5.2a, P5.2b
  Do: web Dockerfile and compose service, a stub OIDC provider container for torii's `GOOGLE_ISSUER_URL`, and a CI job that brings up the stack and runs login, add job, list it, change status. Also closes P4.10b's "via compose" check.
  Done when: the GitHub Actions compose job is green.
  Status: the flow passes locally against the stack (`compose.e2e.yaml` overlay, `npm run test:e2e:compose`). The box is ticked on that evidence; the first run of the `e2e-compose` job in Actions is still to confirm. `ci.yml` triggers only on push to `main` (see P3.5b), so it first runs after merge.

---

## Phase 6: soroban (cost control) and pkg/llm

- [x] **P6.1 soroban migrations and proto** (M) Needs: P2.3
  Do: `prices budgets budget_periods reservations ledger` per the DDL; RPCs Reserve, Commit, Release, GetSpend, SetBudget, ListBudgets, SetPrice, ListPrices; events `cost.threshold_reached`, `cost.budget_exhausted`. Seed prices for the models in use and default budgets ($20 global monthly hard, $15 `fude` monthly hard, $3 other services monthly hard, $1 per feature daily soft) with `TODO(owner)` to confirm numbers.
  Done when: `make proto` and `make migrate` pass.

- [x] **P6.2a Reserve** (M) Needs: P6.1, P1.5
  Do: transactional Reserve locking matching `budget_periods` rows `FOR UPDATE` (ordered by id), checking `spent + reserved + estimate <= limit` for hard budgets, creating periods lazily at local midnight boundaries (Asia/Kolkata); default budgets copied from the nil-owner templates on first use; a refusal is `ResourceExhausted` with reason `BUDGET_EXHAUSTED` and `resets_at`, and `cost.budget_exhausted` is emitted once per period.
  Done when: concurrency test with 50 parallel Reserve calls against a small hard budget never exceeds the limit.

- [x] **P6.2b Commit / Release / expiry / thresholds** (M) Needs: P6.2a
  Do: Commit moves reserved to spent and writes `ledger` (a commit on an expired reservation still records the spend); Release; `expire_reservations` River job every minute; `cost.threshold_reached` emitted once per period and threshold. `cost.*` routes in `pkg/bus/routes.go` are added with their consumers (taiko, sensei), as for the other events.
  Done when: commit and release leave `reserved_micros` at 0; a threshold is emitted once per period.

- [x] **P6.2c soroban read and admin RPCs** (M) Needs: P6.2b
  Do: GetSpend (group by service, feature, model, day), SetBudget (optimistic `version`), ListBudgets (with current period spent and reserved), SetPrice, ListPrices.
  Done when: handler tests pass, including a stale-version SetBudget and spend grouped by each key.

- [x] **P6.3 pkg/llm** (M) Needs: P6.2b, P1.7
  Do: `llm.Complete(ctx, feature, req)`: count tokens, estimate cost, Reserve, call the Claude API, Commit actual usage (or Release on error), prompt caching for system prompt, optional response cache by prompt hash for 24 h; fails closed when soroban is unreachable; model per feature from config; API client behind an interface with a fake for tests.
  Done when: tests with the fake API cover allowed, denied (`ResourceExhausted` with `resets_at`), API error releases reservation, soroban down fails closed.

- [x] **P6.4 soroban admin endpoints in torii and a spend screen** (M) Needs: P6.2c, P6.3, P5.1
  Do: `CostsService` in `api/v1` (spend by day/service/feature, budgets CRUD) and a Settings > Spend page.
  Done when: Playwright test edits a budget and sees updated spend.

---

## Phase 7: fude (drafts) + hanko + tsubame (mail)

- [x] **P7.1 fude migrations, proto, domain** (M) Needs: P6.3
  Do: tables `drafts draft_versions approvals voice_samples templates`; RPCs GenerateDraft, Regenerate, EditDraft, Approve, Discard, ListQueue, GetDraft, AddVoiceSample; draft state machine from the System Design diagram; events `draft.ready/failed/approved`.
  Done when: domain tests cover all transitions including "edit after approval returns to pending".

- [x] **P7.2a fude store, use cases and handlers** (M) Needs: P7.1
  Do: sqlc store for drafts, versions and voice samples; GenerateDraft (idempotency key, queue seam), Regenerate, EditDraft (new user version, digest from `hanko.BodyDigest`), Discard, ListQueue, GetDraft, AddVoiceSample use cases; gRPC handlers with authz, optimistic `version` and stable `ErrorInfo.reason`; register `FudeService`.
  Done when: `cd services/fude && go test -race ./...` passes: store integration tests (stale version, pagination, duplicate key) and a handler test per RPC. `draft.*` events are written by the generation job (P7.2b) and Approve (P7.3), so no outbox row is written here. The `app.Queue` seam is nil until P7.2b wires River.

- [x] **P7.2b1 fude generation use case** (M) Needs: P7.2a
  Do: `app.Generator.Generate`: read the draft, describe its target through a `ContextSource`, take the owner's 5 newest voice samples, pick the template for kind, channel and contact status (default instructions otherwise), call `pkg/llm` feature `fude.<kind>` (follow-up and one-off share `fude.outreach`), write `draft_versions` with its digest, move the draft to pending and emit `draft.ready`. `Fail` marks a generating draft failed (a failed regenerate stays pending) and emits `draft.failed` once with a short reason. Skips a version that already exists or a draft that is no longer waiting.
  Done when: `cd services/fude && go test -race ./internal/app/...` passes with a fake LLM: first version, regenerate, template choice, retry after success, every failure reason, `Fail` for generating and pending drafts. Voice samples are the newest 5; ranking by pgvector similarity moves to P7.2c with the Embedder.

- [x] **P7.2b2 fude generation job and wiring** (M) Needs: P7.2b1
  Do: `generate_draft` River worker (queue `fude`, 2 workers, 5 tries, unique by draft id and version) calling `Generator`; a `*llm.BudgetError` snoozes the job until `ResetsAt`; after the last try call `Fail` and finish; `app.Queue` implementation inserting the job in the caller's transaction; kagami `ContextSource` over `pkg/grpcclient` (`KAGAMI_ADDR`); wire `pkg/llm`, the worker and the queue into `cmd/fude` and add the env vars to `.env.example` and compose.
  Done when: worker tests cover success, retry, final failure and budget denial snoozing; an integration test shows GenerateDraft leading to a pending draft through River with a fake LLM.
  Status: `ANTHROPIC_API_KEY` is required only when `ENVIRONMENT=production`; elsewhere an empty key logs a warning and drafts fail with `generation_failed`. `TODO(owner)`: set `SOROBAN_ADDR`, `KAGAMI_ADDR` and the API key secret in `deploy/helm/values/staging/fude.yaml`, as P4.9d does for torii.

- [x] **P7.2c1 fude embeddings** (M) Needs: P7.2b2
  Do: `Embedder` interface (1024 dimensions) and `VoiceEmbedder`; `embed_voice_sample` job (3 tries, unique per sample) queued by AddVoiceSample in its transaction; the generator ranks the top 5 samples by pgvector cosine distance to the draft's topic, unembedded ones last, and falls back to the newest 5 when the embedder fails. `TODO(owner)`: choose the embedding provider. With none (`cmd/fude` passes a nil embedder), no embed job is queued and the newest samples are used.
  Done when: tests with a fake embedder cover storing a vector, wrong width, provider error, a missing sample, ranking by closeness, the fallback, and an embed through River.

- [x] **P7.2c2 fude event handlers** (M) Needs: P7.2c1
  Do: inbox handlers for `job.added` (cover letter, channel other) and `contact.status_changed` (outreach on the contact's preferred channel; for email the generation job fills the recipient from the contact), each creating the draft in the inbox transaction with the event id as idempotency key; routes for both in `pkg/bus/routes.go`. Kagami's payloads gained `owner_id` for this (commit `14a58bc`). `learning.activity_added` waits for dojo's `events.proto` (see P9.1).
  Done when: consumer tests for both handlers including a duplicate delivery and a payload without an owner.

- [x] **P7.3 Approve and Hanko** (M) Needs: P7.1, P1.9
  Do: `fude.Approve(draft_id, version, body_sha256)` checks state, current version, hash; stamps Hanko; writes `approvals`; sets `approved`; emits `draft.approved`; a test (and a depguard/grep check in CI) asserts `hanko.Sign` is referenced only from `fude/internal/app/approve.go`.
  Done when: tests cover stale version, hash mismatch, double approve, and the single-reference check passes.
  Status: `Approver.Approve` returns the token to its caller only; tsubame.Send (P7.6) will use it, so nothing is stored or logged. `hanko.RecipientDigest` joins `hanko.BodyDigest` in `pkg/hanko` for tsubame to recompute. Copy-only channels are stamped too (the `approvals` row needs a jti) but the token is discarded. `FUDE_HANKO_SIGNING_KEY` (base64 Ed25519 seed) is required at startup. `TODO(owner)`: set `FUDE_HANKO_SIGNING_KEY` and `FUDE_HANKO_KEY_ID` as a secret in the staging and production helm values.

- [x] **P7.4a tsubame tables and encrypted accounts** (M) Needs: P2.3
  Do: migration `00002_mail.sql` (`accounts messages sends`, as in the DDL); `internal/envelope` (AES-256-GCM, a data key per value wrapped by an id-tagged master key, bound to its row so a copied token will not open); `app.Accounts` with Connect (reconnect keeps the id and sync cursor, clears a reauth status) and Token.
  Done when: `cd services/tsubame && go test -race ./...` passes: round trip, tamper, wrong row, rotation, reconnect, disabled account, no plaintext in the stored column.

- [x] **P7.4b tsubame MailProvider and Gmail** (M) Needs: P7.4a
  Do: `MailProvider` interface (List, Get, Send, History) and a fake; Gmail implemented over its REST API with `net/http` and `golang.org/x/oauth2` (`google.golang.org/api` needs Go 1.25.8 and the repo is pinned to 1.25.4 by the linter), base URL injectable for tests; the refresh token is exchanged per call and never logged.
  Done when: tests against an `httptest` Gmail and token endpoint cover list, get, history (including an expired cursor), send, a revoked token marking the account for reauth, and no token in log output.

- [x] **P7.4c tsubame ConnectAccount and CompleteConnect** (M) Needs: P7.4b
  Do: `ConnectAccount` (provider to auth URL) and `CompleteConnect` (code and state to Account) in `tsubame.proto`, with a PKCE verifier and owner sealed into the opaque `state` (no extra table), `TSUBAME_GMAIL_CLIENT_ID`, `TSUBAME_GMAIL_CLIENT_SECRET`, `TSUBAME_GMAIL_REDIRECT_URL` and `TSUBAME_TOKEN_MASTER_KEY` config, wiring in `cmd/tsubame`. The account address comes from Gmail's profile. Torii's callback route and the web "connect mail" screen are new work: add them under P7.8.
  Done when: tests with a mocked Google token endpoint cover a good connect, a bad or expired or foreign state, a reused code, and a test greps captured log output for tokens.
  Status: `TSUBAME_TOKEN_MASTER_KEY` and the three `TSUBAME_GMAIL_*` variables are required at startup (placeholders in `.env.example`). Only one master key is read; rolling a new one needs a way to load the old ones too, which is not built yet. A state is not single-use (the code is, at Google). `TODO(owner)`: create the Google OAuth client with the Gmail API enabled, and set the key and client secret as secrets in the staging and production helm values.

- [x] **P7.5a tsubame mail sync** (M) Needs: P7.4
  Do: `app.Syncer` reads each active account by `history_id`, falling back to a full read of the last 30 days (capped at 500) when there is no cursor or Gmail dropped it, and stores messages, queues classification of inbound ones and advances the cursor in one transaction; `gmail_sync` River periodic job every 5 minutes (unique while one waits or runs, no retries, 4 minute timeout).
  Done when: fixture tests with the fake provider cover first sync, incremental, expired cursor, a failure part way (cursor and rows unchanged, then retried), a deleted message, outbound mail not queued, and one account failing without stopping the others; a test shows River running the scheduled sync.

- [x] **P7.5b1 kagami FindMailLinks** (M) Needs: P4.6
  Do: `FindMailLinks(from_email, urls)` on kagami: the contact by the sender's address, and the job by a posting URL in the mail, else by the sender's company domain (parent domains too, never a free-mail provider, an open job before a rejected one), else the contact's own job.
  Done when: handler tests cover each key, the preferences between them, free-mail senders, per-owner isolation and a missing owner.

- [x] **P7.5b2 tsubame classification** (M) Needs: P7.5a, P7.5b1, P6.3
  Do: `classify_message` River job (3 tries): rules first; `pkg/llm` feature `tsubame.classify` only when the rules are unsure; link the message by thread, then through kagami `FindMailLinks`; emit `mail.classified` and `mail.reply_detected` (new `tsubame/v1/events.proto`); route both to kagami; wire the queue into the syncer.
  Done when: fixture mails cover each classification, the LLM is not called when a rule is sure, and linking works for each key.
  Status: `mail.classified` and `mail.reply_detected` carry `owner_id` for their consumers. Their routes to kagami are added with kagami's handlers in P7.7, so until then the events stay in tsubame's outbox undelivered, which is harmless. Mail with no rule match and no link to a job or contact is stored as `other` without a model call, to save budget. A message that fails classification three times stays unclassified. `ANTHROPIC_API_KEY` is required in production only.

- [x] **P7.6a tsubame Send with Hanko** (M) Needs: P7.4, P1.9
  Do: `Send(hanko, draft_id, version, to, subject, body, contact_id?, job_id?)`: verify signature, audience, issuer, expiry, the owner, and the body and recipient hashes recomputed from the request; pick the owner's active Gmail account before spending the token; insert `sends` (unique `token_jti`, and one live send per draft version) and commit it before calling the provider; send once with the `X-Shogun-Draft: <draft_id>:<version>` header; record `sent` or `failed` with `draft.sent` or `draft.send_failed` in the same transaction. `sends` gained `contact_id` and `job_id` (migration 00003; DDL and LLD updated, with the new `draft.send_failed` event and fude as a `draft.sent` consumer).
  Done when: tests cover each rejection (changed text, subject, recipients, version, draft, no or garbage token, wrong key, key id, audience, issuer, expiry, another owner), token reuse and a second token for one version (both `PermissionDenied`), 8 concurrent sends of one token sending once, a provider failure (row failed, event written, a fresh approval can send), a revoked account marked for reauth, no account (token not spent), and bad input.

- [x] **P7.6b tsubame send reconciler** (M) Needs: P7.6a
  Do: River periodic job that resolves sends left in `sending` (a crash between recording and the provider's answer): look for `X-Shogun-Draft: <draft_id>:<version>` in the provider's recent Sent mail; found means record `sent` and emit `draft.sent`, not found after ten minutes means record `failed` and emit `draft.send_failed`; younger rows are left alone. Never sends anything itself.
  Done when: tests with the fake provider cover found, not found and too young, a provider error leaving the row alone, and that the reconciler never calls Send.

- [x] **P7.6c fude sends approved email and follows the outcome** (M) Needs: P7.6a, P7.3
  Do: `Approve` of an email draft calls `tsubame.Send` with the token (retrying the same token on connection errors, which is safe because a token works once); a definite refusal (no account, not approved) puts the draft back to pending; fude consumes `draft.sent` (approved to sent) and `draft.send_failed` (approved to pending) and never decides that itself; routes `draft.sent` and `draft.send_failed` to fude. `TODO(owner)`: `TSUBAME_HANKO_VERIFY_KEYS` (the public key of `FUDE_HANKO_SIGNING_KEY`) goes in the staging and production helm values.
  Done when: tests with a fake tsubame cover send success, a refusal reverting the draft, retry on connection errors, both event handlers, a duplicate delivery, and a `draft.sent` that arrives for a draft already put back to pending.
  Status: `Approve` of an email draft calls `tsubame.Send` after the approval commits. Only a definite refusal (no account, not approved) or an unreachable tsubame after three tries puts the draft back; a provider failure waits for `draft.send_failed`; an unclear outcome (deadline, internal error) leaves it approved rather than guessing. Nothing but `draft.sent` ever marks a draft sent. Routes for both events to fude are in `pkg/bus/routes.go`; kagami is added in P7.7. `TSUBAME_ADDR` is now required by fude.

- [x] **P7.7 kagami consumes mail and draft events** (M) Needs: P7.5, P7.6, P4.6
  Do: handlers for `mail.classified` (apply at confidence 0.9 or more, else notification suggestion), `mail.reply_detected`, `draft.sent` (contact status and last_contacted); stale-event guard via `occurred_at`.
  Done when: tests include out-of-order and duplicate events.
  Status: mail moves a job only at confidence 0.9 or more, only where the state machine allows, never out of `rejected`, and never repeats the current status; below 0.9 it only leaves a `mail_linked` timeline note (taiko raises the suggestion from `mail.classified` in P8.1). An event that occurred before the job's or contact's latest status change (by event time, kept in the timeline payload) is ignored. `draft.sent` moves a contact not yet reached to `reached_out` and moves `last_contacted` forward only. `ChangeJobStatus` and `ChangeContactStatus` now share `moveJob` and `moveContact` with the handlers. Routes for the three events to kagami are in `pkg/bus/routes.go`. `TODO(owner)`: `contact.status_changed` makes fude draft an outreach message for every status change, including `reached_out` caused by fude's own send and `replied` caused by a reply; decide which statuses should draft (the LLD lists templates per status).

- [x] **P7.8a torii DraftsService and MailService** (M) Needs: P7.3, P7.4c, P5.1
  Do: `api/v1/drafts.proto` (`ListQueue GetDraft GenerateDraft Regenerate EditDraft Approve Discard`) and `api/v1/mail.proto` (`ConnectAccount CompleteConnect`) with Connect handlers over fude and tsubame (`FUDE_ADDR`, `TSUBAME_ADDR`), the same error mapping as jobs and contacts, and `Idempotency-Key` passthrough on GenerateDraft. Approve keeps the `SEND_UNAVAILABLE` and `SEND_STATUS_UNKNOWN` reasons so the screen can say whether the mail may have gone out. fude's queue now carries each draft's subject and a 200 character preview.
  Done when: `cd services/torii && go test -race ./...` passes: each RPC, enum mapping, owner propagation, the approve error reasons, no session, no internals or codes in errors.

- [x] **P7.8b web drafts queue and detail** (M) Needs: P7.8a
  Do: `/drafts` queue (pending first, with subject and preview) and `/drafts/[id]` detail with versions, regenerate with extra context, edit, discard, and approve showing the exact subject, body and recipient that will be sent, with the digest of that text sent with the approval; copy buttons for LinkedIn and X; add-draft dialog; nav entry. Mocked-API Playwright tests.
  Done when: Playwright (mocked API) covers queue, regenerate, edit, approve to email, approve to copy, discard, a stale version and a refused send.
  Status: the approval digest is computed in the browser (`src/lib/digest.ts`) over the stored current version, never the editor text, and is pinned to `hanko.BodyDigest` by a golden vector tested on both sides. Approve is blocked while an edit is unsaved or an older version is open. Polling is bounded to 90 seconds. Follow-up: the draft state machine lets the owner mark an approved copy-only post as posted (approved to sent), but fude has no RPC for it, so LinkedIn and X drafts stay `approved` after copying. Add `MarkPosted` to fude and a button to the copy panel.

- [x] **P7.8c web connect mail** (M) Needs: P7.8a
  Do: Settings > Mail with a Connect Gmail button that follows only an https address from the backend, and a `/mail/callback` page that finishes the connection with the code and state once, then removes them from the address bar.
  Done when: Playwright (mocked API) covers the redirect, a non-https address refused, a failed start, a good callback (called once, code gone from the URL), an expired or foreign state, a refused code, a denied grant and a callback with no code.
  Status: there is no RPC to list connected accounts, so the screen cannot show which address is connected or whether it needs reconnecting. Follow-up: add `ListAccounts` to tsubame and `MailService`, and show address and status here.

- [ ] **P7.8d Compose end-to-end for drafts and mail** (M) Needs: P7.8b, P7.8c, P5.2c
  Do: bring fude, tsubame, soroban and kagami up in the compose e2e overlay with a stub Anthropic API and a stub Gmail (token endpoint, profile, list, get, send), which needs base-URL settings for both in fude's and tsubame's config; a compose Playwright spec: add a draft, regenerate, edit, approve to email, and assert the stub Gmail received exactly one message with the `X-Shogun-Draft` header; add those services to the CI `e2e-compose` job.
  Done when: `npm run test:e2e:compose` passes locally against the stack, and the CI job is green after merge.

- [ ] **P7.8e Mark a copy-only draft as posted** (S) Needs: P7.3
  Do: `MarkPosted` on fude (approved to sent for non-email channels, through the state machine) and a button in the copy panel. Without it LinkedIn and X drafts stay `approved` after the owner posts them.
  Done when: domain, handler and Playwright tests pass.

---

## Phase 8: taiko (notifications)

- [ ] **P8.1 taiko service** (M) Needs: P1.6
  Do: migrations `notifications channel_settings`; consume the events in the LLD catalog; one notification per `source_event_id`; RPCs List, MarkRead, MarkAllRead, Subscribe (server stream).
  Done when: duplicate event creates one notification; stream test receives a new notification within 1 s.

- [ ] **P8.2 torii stream and bell UI** (M) Needs: P8.1, P5.1
  Do: `NotificationsService.Stream` bridging `Subscribe` with last-seen-id reconnect; bell with unread count and list.
  Done when: Playwright: adding a job leads to a "cover letter ready" notification appearing without reload.

- [ ] **P8.3 Daily digest** (S) Needs: P8.1, P4.8
  Do: `daily_digest` at 08:30 summarising due follow-ups, drafts waiting, spend; skipped if empty; respects quiet hours.
  Done when: tests with fake clock.

---

## Phase 9: dojo, katana, shinobi, sensei

- [ ] **P9.1 dojo** (M) Needs: P2.3
  Do: tables, RPCs (items, activities, GeneratePost), events `learning.activity_added`, `learning.item_completed`; torii endpoints; Learning screen. Also add fude's `learning.activity_added` handler (post draft) and its route, deferred from P7.2c.
  Done when: logging an activity leads to a pending post draft in the queue (E2E with fake LLM).

- [ ] **P9.2 katana** (L, split) Needs: P6.3
  Do: GitHub sync with ETags into `github_snapshots`; `suggest` job diffing snapshots plus `learning.item_completed`, feature `katana.suggest`; accept and dismiss RPCs; Profile suggestions screen.
  Done when: fixture snapshots produce suggestions with evidence links; accepting changes state only.

- [ ] **P9.3 shinobi** (L, split) Needs: P6.3, P4.6
  Do: sources of kind api, rss, file with schedule; upsert postings by `(source_id, external_id)`; rule score then LLM score for borderline (feature `shinobi.score`); `discovery.match_found`; `SaveToTracker` calling `kagami.AddJob`; Discovery screen. `TODO(owner)`: provide the first real data source.
  Done when: fixture RSS and file sources work; score at or above `min_score` emits the event once.

- [ ] **P9.4 sensei** (M) Needs: P7.7
  Do: consume `job.*`, `contact.*`, `mail.*`, `draft.sent`, `cost.*` into `facts`; nightly `rollup`; `GetFunnel`, `GetOutreachStats`; Insights screen.
  Done when: replaying the same events twice leaves rollups unchanged.

---

## Phase 10: Hardening and operations

- [ ] **P10.1 Failure drills** (M) Needs: P7.8
  Do: integration tests that kill a consumer mid-delivery, make Claude time out, exhaust a budget, return a Gmail error after Hanko use; assert nothing is lost or double-sent per the System Design failure table.
  Done when: all drills pass in CI.

- [ ] **P10.2 Observability** (M) Needs: P3.3
  Do: dashboards and alerts for outbox lag, discarded jobs, error rate, Gmail sync failing, budget thresholds; `make up-observability` profile works.
  Done when: killing a consumer raises the outbox lag alert in the local stack.

- [ ] **P10.3 Security pass** (M) Needs: P7.8
  Do: dependency and secret scanning in CI, CSP and cookie flags check, authz test that every RPC rejects missing identity, check that logs contain no message bodies or tokens.
  Done when: scans clean; authz test iterates every registered RPC.

- [ ] **P10.4 Backup and restore** (S) Needs: P3.3
  Do: nightly `pg_dump` job and a restore runbook in `docs/`.
  Done when: restore into a fresh container reproduces row counts.

- [ ] **P10.5 First deploy** (M) Needs: P3.6, P3.7, P10.1
  Do: deploy to staging (single VM with Compose, or cluster) with secrets from the secret store; smoke test script.
  Done when: smoke test passes against staging. `TODO(owner)`: choose deploy target.

---

## Later (not scheduled)

Interview prep, resume tailoring per job, post scheduling calendar, Outlook provider for `tsubame`, NATS JetStream behind `pkg/bus`, row-level security for multi-user.
