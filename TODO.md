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

- [ ] **P3.2b Extension operators on search_path** (S) Needs: P3.2
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

- [ ] **P3.7 Helm chart** (M) Needs: P3.1
  Do: `deploy/helm/service` (Deployment, Service, HPA, PDB, ServiceMonitor, pre-upgrade migration Job), `values/staging` and `values/production` per service.
  Done when: `helm lint` and `helm template` pass for all ten values files.

---

## Phase 4: kagami (jobs and contacts) and torii (gateway)

- [ ] **P4.1 kagami proto** (M) Needs: P2.3
  Do: `proto/shogun/kagami/v1/kagami.proto` with the RPCs from the LLD (AddJob, GetJob, ListJobs, UpdateJob, ChangeJobStatus, AddContact, UpdateContact, ListContacts, GetContact, ChangeContactStatus, ImportContacts, ListDueFollowUps) and `events.proto` payloads for `job.*` and `contact.*`.
  Done when: `make proto` and `bin/buf lint` pass.

- [ ] **P4.2 kagami migrations** (M) Needs: P2.3
  Do: migration `00002_core.sql` with `companies jobs job_events contacts contact_events imports` exactly as in the System Design "Table structures" tab.
  Done when: `make migrate` applies cleanly; `\d kagami.jobs` matches the DDL.

- [ ] **P4.3 kagami domain** (M) Needs: P4.1
  Do: `internal/domain` job and contact entities with transition tables from the System Design state machines; errors map to `JOB_STATUS_INVALID_TRANSITION` / `CONTACT_STATUS_INVALID_TRANSITION`.
  Done when: table-driven tests cover every allowed and one disallowed transition per state.

- [ ] **P4.4 kagami store** (M) Needs: P4.2
  Do: sqlc queries + repositories with optimistic `version` and cursor pagination; contacts dedupe on lower(email) then linkedin_url.
  Done when: testcontainers integration tests pass for create, update with stale version (fails), list pagination, dedupe.

- [ ] **P4.5 kagami use cases + handlers (jobs)** (M) Needs: P4.3, P4.4, P1.5
  Do: AddJob (upsert company by domain, write `job.added` via outbox), ChangeJobStatus (writes `job_events` and `job.status_changed`), Get/List/Update; gRPC handlers with authz.
  Done when: handler tests pass; a test asserts exactly one outbox row per mutating call.

- [ ] **P4.6 kagami use cases + handlers (contacts)** (M) Needs: P4.5
  Do: Add/Update/List/Get contact, ChangeContactStatus writing `contact.status_changed`.
  Done when: handler tests pass including idempotency-key replay returning the same row.

- [ ] **P4.7 CSV import** (M) Needs: P4.6
  Do: `ImportContacts` parsing the 17-column CSV, per-row validation, dry run, one transaction on commit, `imports` row, one `contact.added` per new contact.
  Done when: tests with a good file, a file with bad rows (per-row errors returned, nothing written on dry run), and re-import (updates not duplicates).

- [ ] **P4.8 kagami follow-up scans** (S) Needs: P4.6
  Do: River periodic jobs `follow_up_scan` and `stale_application_scan` at 08:00 Asia/Kolkata emitting `job.follow_up_due` and `contact.follow_up_due` once per due date; `ListDueFollowUps` RPC.
  Done when: tests with a fake clock show one event per due item per day.

- [ ] **P4.9 torii proto + login** (L, split) Needs: P1.7, P3.3
  Do (a): `proto/shogun/api/v1` Jobs and Contacts services. Do (b): Google OAuth with PKCE, email allowlist from env, session in `torii.sessions` (token hash only), cookie HttpOnly Secure SameSite=Lax, sliding renewal. Do (c): ConnectRPC server, session middleware, internal identity signing, rate limit, request size cap.
  Done when: tests with a fake OAuth provider cover allowed email, disallowed email, expired session, logout; an unauthenticated API call returns `Unauthenticated`.

- [ ] **P4.10 torii jobs and contacts endpoints** (M) Needs: P4.9, P4.6
  Do: screen-shaped endpoints calling kagami; `Idempotency-Key` passthrough.
  Done when: end-to-end test via compose: login stub, add a job, list it, change status.

---

## Phase 5: Web app (first screens)

- [ ] **P5.1 Next.js app + generated client** (M) Needs: P4.9
  Do: `web/` with Next.js, TypeScript, Connect-Web client generated by `make proto`, login page, authenticated layout.
  Done when: `cd web && npm run build` passes; login flow works against compose.

- [ ] **P5.2 Jobs board and contacts table** (L, split) Needs: P5.1, P4.10
  Do: job board by status with drag to change status, add-job form; contacts table with status filter, add-contact form, CSV import dialog (dry run result then confirm).
  Done when: Playwright tests for add job, move job, import CSV (good and bad file) pass.

---

## Phase 6: soroban (cost control) and pkg/llm

- [ ] **P6.1 soroban migrations and proto** (M) Needs: P2.3
  Do: `prices budgets budget_periods reservations ledger` per the DDL; RPCs Reserve, Commit, Release, GetSpend, SetBudget, ListBudgets, SetPrice, ListPrices; events `cost.threshold_reached`, `cost.budget_exhausted`. Seed prices for the models in use and default budgets ($20 global monthly hard, $15 `fude` monthly hard, $3 other services monthly hard, $1 per feature daily soft) with `TODO(owner)` to confirm numbers.
  Done when: `make proto` and `make migrate` pass.

- [ ] **P6.2 Reserve / Commit / Release** (L, split) Needs: P6.1, P1.5
  Do: transactional Reserve locking matching `budget_periods` rows `FOR UPDATE`, checking `spent + reserved + estimate <= limit` for hard budgets, creating periods lazily at local midnight boundaries; Commit moves reserved to spent and writes `ledger`; Release; `expire_reservations` job every minute; threshold events emitted once per period.
  Done when: concurrency test with 50 parallel Reserve calls against a small hard budget never exceeds the limit; commit and release leave `reserved_micros` at 0.

- [ ] **P6.3 pkg/llm** (M) Needs: P6.2, P1.7
  Do: `llm.Complete(ctx, feature, req)`: count tokens, estimate cost, Reserve, call the Claude API, Commit actual usage (or Release on error), prompt caching for system prompt, optional response cache by prompt hash for 24 h; fails closed when soroban is unreachable; model per feature from config; API client behind an interface with a fake for tests.
  Done when: tests with the fake API cover allowed, denied (`ResourceExhausted` with `resets_at`), API error releases reservation, soroban down fails closed.

- [ ] **P6.4 soroban admin endpoints in torii and a spend screen** (M) Needs: P6.3, P5.1
  Do: `CostsService` in `api/v1` (spend by day/service/feature, budgets CRUD) and a Settings > Spend page.
  Done when: Playwright test edits a budget and sees updated spend.

---

## Phase 7: fude (drafts) + hanko + tsubame (mail)

- [ ] **P7.1 fude migrations, proto, domain** (M) Needs: P6.3
  Do: tables `drafts draft_versions approvals voice_samples templates`; RPCs GenerateDraft, Regenerate, EditDraft, Approve, Discard, ListQueue, GetDraft, AddVoiceSample; draft state machine from the System Design diagram; events `draft.ready/failed/approved`.
  Done when: domain tests cover all transitions including "edit after approval returns to pending".

- [ ] **P7.2 fude generation pipeline** (L, split) Needs: P7.1
  Do: `generate_draft` River job (keyed by draft id and version): gather context, top 5 voice samples by pgvector similarity, template for kind and contact status, call `pkg/llm` feature `fude.<kind>`, write `draft_versions`, emit `draft.ready`; on 5 failures set `failed` and emit `draft.failed`. Event handlers: `job.added` (cover letter), `contact.status_changed` (outreach), `learning.activity_added` (post).
  Done when: tests with fake LLM cover success, retries, final failure, budget denial snoozing the job.

- [ ] **P7.3 Approve and Hanko** (M) Needs: P7.1, P1.9
  Do: `fude.Approve(draft_id, version, body_sha256)` checks state, current version, hash; stamps Hanko; writes `approvals`; sets `approved`; emits `draft.approved`; a test (and a depguard/grep check in CI) asserts `hanko.Sign` is referenced only from `fude/internal/app/approve.go`.
  Done when: tests cover stale version, hash mismatch, double approve, and the single-reference check passes.

- [ ] **P7.4 tsubame accounts, Gmail OAuth, provider interface** (M) Needs: P2.3
  Do: migrations `accounts messages sends`; `MailProvider` interface (List, Get, Send, History) with a Gmail implementation and a fake; envelope-encrypted token storage; ConnectAccount / CompleteConnect RPCs.
  Done when: tests with the fake provider and a mocked Google token endpoint pass; tokens are never logged (test greps log output).

- [ ] **P7.5 tsubame sync and classification** (L, split) Needs: P7.4, P6.3
  Do: `gmail_sync` every 5 min using `history_id`; `classify_message` rules first, `pkg/llm` feature `tsubame.classify` only when unsure; link to job or contact by domain, URL, thread, email; emit `mail.classified` and `mail.reply_detected`.
  Done when: fixture mails cover each classification; the cursor advances only on success.

- [ ] **P7.6 tsubame Send with Hanko** (M) Needs: P7.4, P1.9
  Do: `Send(hanko, to, subject, body)` verifies signature, audience, expiry, recomputed hashes, inserts `sends` with unique `token_jti`, sends once, adds the `X-Shogun-Draft` header, emits `draft.sent`; reconciler checks Sent mail for the header before any retry.
  Done when: tests cover each rejection, token reuse (`PermissionDenied`), provider failure (row marked failed, draft back to pending via event), and no code path sends without verification.

- [ ] **P7.7 kagami consumes mail and draft events** (M) Needs: P7.5, P7.6, P4.6
  Do: handlers for `mail.classified` (apply at confidence 0.9 or more, else notification suggestion), `mail.reply_detected`, `draft.sent` (contact status and last_contacted); stale-event guard via `occurred_at`.
  Done when: tests include out-of-order and duplicate events.

- [ ] **P7.8 Draft queue UI** (L, split) Needs: P7.3, P5.1
  Do: torii `DraftsService`; web screens for queue, draft detail with versions, regenerate with extra context, edit, approve (shows exactly what will be sent), copy button for LinkedIn and X.
  Done when: Playwright: generate, regenerate, edit, approve to email via fake provider, verify one send.

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
  Do: tables, RPCs (items, activities, GeneratePost), events `learning.activity_added`, `learning.item_completed`; torii endpoints; Learning screen.
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
