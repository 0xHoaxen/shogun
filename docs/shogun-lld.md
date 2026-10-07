# Shogun LLD

Oct 1, 2026 · @sboy99

## Scope and conventions

This LLD turns the Shogun HLD into buildable detail: module layout, tables, RPCs, event payloads and the release pipeline. Services use the Japanese names below everywhere: folders, Go modules, proto packages, Postgres schemas and roles, env vars, images and release tags.

| Service | Was | Meaning | Job |
| --- | --- | --- | --- |
| `torii` | gateway | the shrine gate | the one door the web app talks to |
| `kagami` | tracker | the mirror | jobs, contacts, follow-ups |
| `tsubame` | mail | the swallow, a swift messenger | Gmail sync, classification, the only sender |
| `fude` | drafter | the calligraphy brush | AI drafts and the approval queue |
| `taiko` | notifications | the war drum | notifications and digests |
| `dojo` | learning | the training hall | learning items and activities |
| `katana` | profile | your sharpened edge | GitHub sync, resume and LinkedIn suggestions |
| `shinobi` | discovery | the scout | job postings and scoring |
| `sensei` | insights | the teacher who reviews | analytics |
| `hanko` (library) | approval token | the personal seal | `pkg/hanko`: nothing leaves without your stamp |
| soroban | (new) | the abacus | token metering, budgets, spend limits |

Event types keep domain nouns (`job.added`, `mail.classified`, `draft.sent`), not service names, because they describe what happened; a future rename never breaks a consumer.

| Concern | Convention |
| --- | --- |
| IDs | UUIDv7 everywhere (time-sortable, generated in Go, never by Postgres). Stored as `uuid`. |
| Time | `timestamptz` in UTC; proto uses `google.protobuf.Timestamp`; dates without time (follow-ups) use `date` and a `YYYY-MM-DD` string in proto. |
| Enums | Postgres `text` with a `CHECK` constraint (cheap to extend); proto enums with `_UNSPECIFIED = 0`. |
| Errors | gRPC status codes only: `InvalidArgument`, `NotFound`, `AlreadyExists`, `FailedPrecondition` (bad state transition), `PermissionDenied` (bad approval token), `Unavailable` (downstream). Details via `google.rpc.ErrorInfo` with a stable `reason` like `JOB_STATUS_INVALID_TRANSITION`. |
| Pagination | Cursor based: `page_size` (default 50, max 200) + opaque `page_token` (base64 of the last row's `(created_at, id)`). |
| Soft delete | `archived_at timestamptz` on user-facing rows; nothing is hard deleted except by a purge job. |
| Updates | `FieldMask` on every `Update*` RPC; `version int` column for optimistic locking, compared on write. |
| Proto packages | `shogun.<service>.v1` (for example `shogun.kagami.v1`), one `<service>.proto` per service plus `events.proto` for that service's event payloads. |
| Go modules | `github.com/sboy99/shogun/<path>`; one module per service, plus `pkg` and `gen/go`. |
| Single tenant | One user today, but every table carries `owner_id uuid` so multi-user is a migration, not a redesign. |

## Repository layout

One Git repo, one `go.work`, twelve Go modules: each service is its own module so it versions, builds and deploys alone, while `go.work` lets you edit everything together locally.

```text
shogun/
├── go.work                  # uses gen/go, pkg, services/*
├── .tool-versions           # go, golangci-lint, node, helm (asdf/mise; CI reads it too)
├── Makefile                 # tools, proto, lint, test, build, up, migrate, new-service, rename-service
├── buf.yaml / buf.gen.yaml  # proto lint, breaking-change check, codegen
├── proto/shogun/
│   ├── events/v1/envelope.proto
│   ├── api/v1/              # torii's public API for the web app
│   └── <service>/v1/{<service>.proto, events.proto}
├── gen/go/                  # generated code, committed; module github.com/sboy99/shogun/gen/go
├── pkg/                     # shared libs (config, logger, server, postgres, outbox, inbox, bus, authz, hanko, version)
├── services/
│   ├── torii/               # gateway: ConnectRPC for the web app, gRPC clients to the rest
│   ├── kagami/              # jobs and contacts
│   ├── tsubame/             # mail
│   ├── fude/                # drafts and approvals
│   ├── taiko/               # notifications
│   ├── dojo/                # learning
│   ├── katana/              # profile suggestions
│   ├── shinobi/             # job discovery
│   ├── sensei/              # insights
│   └── soroban/             # cost metering and budgets
│       ├── go.mod           # (every service) replace => ../../pkg, ../../gen/go so GOWORK=off builds too
│       ├── cmd/<service>/main.go
│       ├── internal/{app,domain,store,transport/grpc,events,jobs}
│       └── migrations/      # goose SQL, embedded in the binary
├── tools/go.mod             # pinned buf, protoc-gen-go(-grpc), goose, sqlc via `tool` directives
├── web/                     # Next.js UI (own package.json, own release)
├── deploy/
│   ├── docker/Dockerfile    # one multi-stage file, ARG SERVICE
│   ├── compose/             # local stack + postgres init (schema and role per service)
│   └── helm/service/        # one generic chart; values/<env>/<service>.yaml per service
├── scripts/                 # new-service.sh, rename-service.sh, changed-modules.sh
├── docs/adr/                # architecture decisions
└── .github/                 # workflows, dependabot, PR template
```

**Module rules.** Services may import `pkg` and `gen/go`, never each other. `pkg` never imports a service. Cross-service calls go through generated gRPC clients only. A `depguard` lint rule enforces this.

**Renaming.** A service's name lives in its folder, proto package, schema, role, chart values and release config. `scripts/rename-service.sh <old> <new>` moves and rewrites all of them in one commit, so a later rename stays cheap.

## Inside a service

Every service has the same five layers, so moving between services is free and a new one comes from `make new-service NAME=x`. Dependencies point inward: transport and store depend on domain, never the reverse.

| Package | Holds | Depends on |
| --- | --- | --- |
| `cmd/<svc>` | `main.go`: load config, build logger, open DB, run migrations, wire `app`, call `server.Run` | `internal/app`, `pkg/*` |
| `internal/app` | Use cases (`AddJob`, `ApproveDraft`): validate, open a transaction, call store, write outbox events | `domain`, store interfaces |
| `internal/domain` | Entities, value types, state machines (job status, contact status, draft state), domain errors. Pure Go, no I/O | nothing |
| `internal/store` | Postgres repositories with sqlc-generated queries; implements the interfaces `app` declares | `pgx`, `domain` |
| `internal/transport/grpc` | Handlers: proto to domain mapping, error to status code mapping | `app`, `gen/go` |
| `internal/events` | Inbox handlers for events this service consumes (one func per event type) | `app` |
| `internal/jobs` | River workers and periodic jobs (AI generation, mail sync, scraping) | `app` |

**Request path (write).**

1. gRPC handler maps the request to a command and calls the use case.
2. Use case begins a `pgx.Tx`, loads the aggregate, applies the domain transition (which may reject with `FailedPrecondition`).
3. Store writes the row (checking `version`), then `outbox.Write` adds the event in the same transaction.
4. Commit. The handler returns; no other service was called synchronously.

**Queries** go through the use-case layer too, but read-only use cases may call the store directly without a transaction.

**sqlc** generates typed Go from `internal/store/queries/*.sql`, so there is no ORM and every query is reviewed as SQL.

## Shared libraries in pkg

`pkg` holds only cross-cutting plumbing; anything with business meaning stays in its service. It is versioned on its own (`pkg/v0.x.y`) so services can lag a version when needed.

| Package | Does | Key API |
| --- | --- | --- |
| `config` | Env-based config; every service embeds `config.Base` (name, env, gRPC and HTTP addr, DB URL, log level, shutdown timeout) | `LoadBase(name)`, `String/Bool/Duration(key, def)` |
| `logger` | JSON `slog` tagged with service, version, trace id | `New(service, level)` |
| `server` | gRPC server with health, reflection, recovery, logging, OTel interceptors; HTTP `/healthz`, `/readyz`, `/metrics`; graceful shutdown on SIGTERM | `Run(ctx, cfg, log, register)` |
| `postgres` | pgx pool with `search_path` pinned to the service schema; goose migrations from `embed.FS` | `Connect`, `Migrate`, `InTx(ctx, pool, fn)` |
| `outbox` | Writes events in the caller's tx; the relay delivers them | `Write(ctx, tx, type, subject, payload)`, `NewRelay(pool, bus)` |
| `inbox` | Dedupes consumed events by id, then runs the handler in one tx | `Handle(ctx, pool, env, fn)` |
| `bus` | Interface over the event transport; v1 implementation is Postgres + River | `Publish(ctx, env)`, `Subscribe(types, handler)` |
| `grpcclient` | Dials another service with retries, timeouts, OTel, and the internal auth header | `Dial(ctx, target)` |
| `authz` | Verifies the torii-signed internal identity on every RPC | `UnaryInterceptor(key)`, `FromContext(ctx)` |
| `hanko` | Signs and verifies approval tokens (see Hanko: the approval token) | `Sign`, `Verify` |
| `telemetry` | OTel tracer and meter setup, OTLP exporter | `Init(ctx, service)` |
| `version` | Build metadata via `-ldflags` | `Version`, `Commit` |
| llm | The only way to call Claude: reserves with Soroban, calls the API, commits real usage; prompt caching and response cache | Complete(ctx, feature, req) |

## Data model

One Postgres database, one schema and one login role per service; a role can only touch its own schema, so the "no reading other tables" rule is enforced by Postgres, not by discipline. Every table also has `id uuid pk`, `owner_id uuid`, `created_at`, `updated_at`; user-facing rows add `version int` and `archived_at`.

**In every schema**

| Table | Columns | Notes |
| --- | --- | --- |
| `outbox` | `id`, `type`, `subject`, `body bytea` (Envelope proto), `created_at`, `delivered_at` | Partial index on `delivered_at IS NULL`; rows older than 7 days and delivered are purged nightly |
| `inbox` | `event_id pk`, `type`, `processed_at` | Dedupe for consumed events; purged after 30 days |
| `river_*` | River's own job tables | Created by River's migrator inside the service schema |

**Per service**

| Schema | Table | Key columns |
| --- | --- | --- |
| `torii` | `sessions` | `token_hash`, `user_email`, `expires_at`, `last_seen_at`, `user_agent` |
| `kagami` | `companies` | `name`, `domain` (unique per owner), `notes` |
| `kagami` | `jobs` | `company_id`, `title`, `url` (unique per owner), `source`, `status` (saved, applied, shortlisted, interview, offer, rejected), `applied_on date`, `next_follow_up date`, `salary_text`, `location`, `description` |
| `kagami` | `job_events` | `job_id`, `kind` (status\_changed, note, mail\_linked, follow\_up\_set), `from_status`, `to_status`, `payload jsonb`, `occurred_at` |
| `kagami` | `contacts` | the 17 spreadsheet fields: `full_name`, `company_id`, `role`, `email`, `linkedin_url`, `x_handle`, `phone`, `relationship`, `how_we_met`, `status` (not\_reached, reached\_out, conversation\_started, replied, referral\_asked), `preferred_channel`, `last_contacted date`, `next_follow_up date`, `target_role`, `job_id` (from `job_link`), `tags text[]`, `notes`. Unique on lower(email) and on linkedin\_url when present (dedupe for import) |
| `kagami` | `contact_events` | `contact_id`, `kind` (status\_changed, message\_sent, reply\_received, note), `channel`, `payload jsonb`, `occurred_at` |
| `kagami` | `imports` | `filename`, `rows_total`, `rows_created`, `rows_updated`, `rows_failed`, `errors jsonb` |
| `tsubame` | `accounts` | `provider` (gmail), `address`, `token_ciphertext bytea`, `token_key_id`, `history_id` (Gmail sync cursor), `status` |
| `tsubame` | `messages` | `account_id`, `provider_message_id` (unique), `thread_id`, `from_addr`, `to_addrs text[]`, `subject`, `snippet`, `received_at`, `classification` (application\_confirmation, interview\_invite, rejection, offer, recruiter\_outreach, reply, other), `confidence real`, `linked_job_id`, `linked_contact_id` |
| `tsubame` | `sends` | `draft_id`, `draft_version`, `token_jti` (unique, single use), `provider_message_id`, `sent_at`, `status` |
| `fude` | `drafts` | `kind` (cover\_letter, outreach, follow\_up, post, one\_off), `target_type`, `target_id`, `channel` (email, linkedin, x, other), `state` (generating, pending, approved, sent, discarded, failed), `current_version` |
| `fude` | `draft_versions` | `draft_id`, `version`, `subject`, `body`, `body_sha256`, `prompt_context jsonb`, `model`, `input_tokens`, `output_tokens`, `created_by` (ai, user) |
| `fude` | `voice_samples` | `channel`, `text`, `embedding vector(1024)` (pgvector) |
| `fude` | `templates` | `kind`, `contact_status`, `instructions` (the per-status prompt) |
| `taiko` | `notifications` | `type`, `title`, `body`, `link`, `source_event_id` (unique), `read_at` |
| `taiko` | `channel_settings` | `channel` (in\_app, email\_digest), `enabled`, `quiet_hours` |
| `dojo` | `items` | `title`, `kind` (course, book, project, skill), `url`, `status` (planned, in\_progress, done), `completed_on` |
| `dojo` | `activities` | `item_id`, `summary`, `minutes`, `occurred_on date`, `tags text[]` |
| `katana` | `github_snapshots` | `taken_at`, `repos jsonb`, `contributions jsonb` |
| `katana` | `suggestions` | `target` (resume, linkedin), `section`, `before`, `after`, `reason`, `evidence jsonb`, `state` (open, accepted, dismissed) |
| `shinobi` | `sources` | `kind`, `config jsonb`, `schedule` (cron), `last_run_at` |
| `shinobi` | `postings` | `source_id`, `external_id` (unique per source), `title`, `company`, `url`, `location`, `posted_at`, `raw jsonb` |
| `shinobi` | `preferences` | `roles text[]`, `locations text[]`, `must_have text[]`, `nice_to_have text[]`, `min_score` |
| `shinobi` | `scores` | `posting_id`, `score real`, `reasons jsonb` |
| `sensei` | `facts` | `event_id` (unique), `type`, `dimension jsonb`, `occurred_at` (append-only, projected from events) |
| `sensei` | `daily_rollups` | `day`, `metric`, `dimension`, `value` (rebuilt nightly from `facts`) |
| `soroban` | `prices` | model, effective\_from, input\_usd\_per\_mtok, output\_usd\_per\_mtok, cache\_read\_usd\_per\_mtok, cache\_write\_usd\_per\_mtok |
| `soroban` | `budgets` | scope\_type (global, service, feature), scope\_value, period (daily, monthly), limit\_micros bigint, mode (hard, soft), thresholds int\[\] |
| `soroban` | `budget_periods` | budget\_id, period\_start, spent\_micros, reserved\_micros, notified\_thresholds int\[\] (the row Reserve locks) |
| `soroban` | `reservations` | service, feature, model, est\_micros, budget\_period\_ids uuid\[\], status (open, committed, released, expired), expires\_at |
| `soroban` | `ledger` | reservation\_id, service, feature, model, input\_tokens, output\_tokens, cache\_read\_tokens, cache\_write\_tokens, cost\_micros, request\_id, occurred\_at (append-only) |

## gRPC APIs

Each service exposes one `<Name>Service` in `shogun.<name>.v1` (for example `kagami.v1.KagamiService`). "Caller" says who may call it; the `authz` interceptor rejects anyone else. List RPCs all take `page_size`, `page_token` and return `next_page_token`.

| Service | RPC | Request (key fields) | Returns | Caller |
| --- | --- | --- | --- | --- |
| kagami | `AddJob` | title, company\_name, url, source, status | Job | torii, shinobi |
| kagami | `GetJob` / `ListJobs` | id / status filter, company\_id, due\_before | Job / Jobs | torii |
| kagami | `UpdateJob` | job, update\_mask, version | Job | torii |
| kagami | `ChangeJobStatus` | id, to\_status, note, version | Job | torii |
| kagami | `AddContact` / `UpdateContact` | contact fields, update\_mask | Contact | torii |
| kagami | `ListContacts` / `GetContact` | status, tag, company\_id, query | Contacts / Contact with timeline | torii |
| kagami | `ChangeContactStatus` | id, to\_status, version | Contact | torii |
| kagami | `ImportContacts` | csv bytes, dry\_run | ImportReport (per-row errors) | torii |
| kagami | `ListDueFollowUps` | on\_or\_before date | jobs and contacts due | torii, taiko |
| tsubame | `ConnectAccount` / `CompleteConnect` | provider / oauth code, state | auth URL / Account | torii |
| tsubame | `ListMessages` | classification, linked\_job\_id | Messages | torii |
| tsubame | `SyncNow` | account\_id | SyncResult | torii |
| tsubame | `Send` | hanko token, draft\_id, version, to, subject, body, contact\_id?, job\_id? | SendResult | fude only |
| fude | `GenerateDraft` | kind, target\_type, target\_id, channel, extra\_context | Draft (state generating) | torii, internal events |
| fude | `Regenerate` | draft\_id, extra\_context | Draft (new version) | torii |
| fude | `EditDraft` | draft\_id, subject, body | Draft (new version, created\_by user) | torii |
| fude | `Approve` | draft\_id, version, body\_sha256 | ApproveResult (sent or copy-ready) | torii only |
| fude | `Discard` / `ListQueue` / `GetDraft` | draft\_id / state filter | Draft(s) | torii |
| fude | `AddVoiceSample` | channel, text | VoiceSample | torii |
| taiko | `List` / `MarkRead` / `MarkAllRead` | unread\_only / ids | Notifications | torii |
| taiko | `Subscribe` | none (server stream) | stream of Notification | torii |
| dojo | `AddItem` / `UpdateItem` / `ListItems` | item fields | Item(s) | torii |
| dojo | `GetItem` / `ChangeItemStatus` | id / id, to\_status, version | Item | torii, fude (`GetItem`) |
| dojo | `LogActivity` / `ListActivities` | item\_id, summary, minutes, occurred\_on | Activity | torii |
| dojo | `GetActivity` | id | Activity with its item | torii, fude |
| dojo | `GeneratePost` | activity\_ids, channel | Draft id | torii |
| katana | `SyncGitHub` | none | Snapshot | torii, scheduler |
| katana | `ListSuggestions` / `AcceptSuggestion` / `DismissSuggestion` | target, state / id | Suggestion(s) | torii |
| shinobi | `UpsertSource` / `ListSources` / `RunSource` | kind, config, schedule | Source / RunResult | torii |
| shinobi | `SetPreferences` / `GetPreferences` | roles, locations, keywords, min\_score | Preferences | torii |
| shinobi | `ListPostings` / `SaveToTracker` | min\_score / posting\_id | Postings / Job | torii |
| sensei | `GetFunnel` | from, to, group\_by (source, month) | stage counts and rates | torii |
| sensei | `GetOutreachStats` | from, to, group\_by (channel, status) | sent, replied, reply\_rate | torii |
| soroban | Reserve | service, feature, model, est\_input\_tokens, est\_output\_tokens | reservation\_id, expires\_at, or ResourceExhausted | pkg/llm in any service |
| soroban | Commit / Release | reservation\_id, actual token counts / reservation\_id | LedgerEntry / empty | pkg/llm in any service |
| soroban | GetSpend | from, to, group\_by (service, feature, model, day) | cost and token totals | torii, sensei |
| soroban | SetBudget / ListBudgets | scope, period, limit\_micros, mode, thresholds | Budget(s) with current spend | torii |
| soroban | SetPrice / ListPrices | model, rates, effective\_from | Price(s) | torii |

Every service also serves `grpc.health.v1.Health` and server reflection (reflection off in production). Proto changes are checked by `buf breaking` against `main`, so a field can be added freely but never renumbered or removed inside `v1`.

## Events: catalog and delivery

Events are delivered at least once, pushed from the producer's outbox to each consumer's `EventSink.Deliver` RPC; consumers dedupe on event id. No shared tables and no broker in v1, and the transport sits behind `pkg/bus` so moving to NATS JetStream later changes no producer or consumer code.

**Catalog** (payload messages live in `proto/shogun/<producer>/v1/events.proto`; routes live in `pkg/bus/routes.go`)

| Event | Producer | Payload | Consumers |
| --- | --- | --- | --- |
| `job.added` | kagami | job\_id, title, company, url, source | fude (cover letter), sensei |
| `job.status_changed` | kagami | job\_id, from, to, at | sensei, taiko (offer, interview) |
| `job.follow_up_due` | kagami | job\_id, due\_on | fude (follow-up), taiko |
| `contact.added` | kagami | contact\_id, status | sensei |
| `contact.status_changed` | kagami | contact\_id, from, to, channel | fude (outreach), sensei |
| `contact.follow_up_due` | kagami | contact\_id, due\_on | fude (nudge), taiko |
| `mail.classified` | tsubame | message\_id, classification, confidence, job\_id?, contact\_id? | kagami (job status), taiko, sensei |
| `mail.reply_detected` | tsubame | message\_id, contact\_id | kagami (status to replied), taiko, sensei |
| `draft.ready` | fude | draft\_id, kind, target, version | taiko |
| `draft.failed` | fude | draft\_id, reason | taiko |
| `draft.approved` | fude | draft\_id, version, channel | sensei |
| `draft.sent` | tsubame | draft\_id, version, contact\_id?, job\_id?, sent\_at | fude (draft to sent), kagami (contact status, last\_contacted), sensei |
| `draft.send_failed` | tsubame | draft\_id, version, reason | fude (draft back to pending), taiko |
| `learning.activity_added` | dojo | activity\_id, item\_id, summary | fude (post draft) |
| `learning.item_completed` | dojo | item\_id, title, kind | katana, fude (post draft) |
| `profile.suggestion_ready` | katana | suggestion\_id, target | taiko |
| `discovery.match_found` | shinobi | posting\_id, score, title, company | taiko |
| cost.threshold\_reached | soroban | budget\_id, scope, period, percent, spent\_micros, limit\_micros | taiko, sensei |
| cost.budget\_exhausted | soroban | budget\_id, scope, period, resets\_at | taiko |

**Delivery path**

1. Producer use case writes the row and the outbox row in one transaction.
2. The producer's relay (a River periodic job every 1 s, plus a `NOTIFY` wake-up) reads up to 100 undelivered rows with `FOR UPDATE SKIP LOCKED`, so several replicas can share the work.
3. For each route, it enqueues a River `deliver` job (one per event and consumer), then marks the outbox row delivered.
4. The `deliver` job calls the consumer's `EventSink.Deliver(Envelope)`. River retries with exponential backoff for up to 24 hours; then the job is discarded and alerts fire.
5. The consumer's `Deliver` inserts into `inbox` (`ON CONFLICT DO NOTHING`) and enqueues its own River handler job in the same transaction, then returns OK. A duplicate returns OK without doing anything.
6. The handler job runs the use case. Failures retry inside the consumer and never block the producer.

**Ordering.** Not guaranteed across events. Handlers that care (status changes) compare `occurred_at` with the row's last applied event and ignore stale ones.

**Versioning.** Event types never change meaning. A breaking payload change ships as a new type (`job.added.v2`) published alongside the old one until every consumer moves.

## Hanko: the approval token

A hanko is a PASETO `v4.public` token (Ed25519): `fude` holds the only private key and `tsubame` holds only the public key, so even a compromised `tsubame` cannot mint one. The code lives in `pkg/hanko`.

| Claim | Value |
| --- | --- |
| `jti` | UUIDv7, single use |
| `aud` | `tsubame` |
| `iss` | `fude` |
| `sub` | owner id of the approving user |
| `draft_id`, `version` | the exact draft version approved |
| `body_sha256` | hash of subject + body as approved |
| `rcpt_sha256` | hash of the sorted recipient list |
| `iat`, `exp` | issued now, expires after 5 minutes |

**Stamp** (in `fude.Approve` only): torii passes the user's session identity, `fude` checks the draft is `pending`, the version is current and `body_sha256` matches what the user saw, signs, sets the draft to `approved`, then calls `tsubame.Send`.

**Verify** (in `tsubame.Send`): signature, `aud`, `exp`, recompute both hashes from the request, then `INSERT INTO sends (token_jti …)`. The unique `token_jti` makes the hanko single use even across replicas. Any failure returns `PermissionDenied` and sends nothing. The `sends` row is committed before the provider is called, so a crash can never lead to a second send: a row left in `sending` is resolved by a reconciler that looks for the `X-Shogun-Draft: <draft_id>:<version>` header in the provider's Sent mail, then records `sent` and emits `draft.sent`, or after ten minutes records `failed` and emits `draft.send_failed`. fude moves the draft from `approved` to `sent` on `draft.sent` and back to `pending` on `draft.send_failed`; it never decides that for itself.

**Keys** live in the secret store (`FUDE_HANKO_SIGNING_KEY`, `TSUBAME_HANKO_VERIFY_KEY`) and carry a key id, so rotation keeps two public keys valid for one day. No River worker, schedule or AI step has a code path to `Approve`; a test asserts the signer is referenced from exactly one package.

## Soroban: cost control

Soroban (the abacus) is the tenth service: every paid call (Claude tokens today; Gmail, GitHub or scraping APIs later if they cost money) is reserved against a budget before it runs and recorded after, so spend can never pass a hard limit, even with many jobs running at once.

**How a metered call works**

1. A service calls Claude only through `pkg/llm`, never the SDK directly (a `depguard` rule enforces it).
2. `pkg/llm` estimates the cost from the prompt's input tokens plus `max_tokens`, and calls `soroban.Reserve(service, feature, model, est_input, est_output)`.
3. Soroban locks the matching budget rows, checks `spent + reserved + estimate <= limit` for every budget in scope (global, service, feature), and either returns a `reservation_id` or `ResourceExhausted` naming the budget that blocked it.
4. On success the call runs. Afterwards `pkg/llm` calls `Commit(reservation_id, input, output, cache_read, cache_write)` with the real usage from the API response; Soroban prices it, writes a ledger row and moves the amount from reserved to spent.
5. If the call fails, `pkg/llm` calls `Release`. A sweeper releases reservations nobody committed within 10 minutes (crashed workers).
6. A blocked River job is snoozed until the budget's period resets, and the user gets one notification, not one per job.

**Budgets**

| Field | Values |
| --- | --- |
| Scope | global, a service (`fude`), or a feature (`fude.cover_letter`, `fude.outreach`, `fude.post`, `tsubame.classify`, `katana.suggest`, `shinobi.score`) |
| Period | daily or monthly, reset at local midnight (Asia/Kolkata assumed) |
| Limit | in USD, stored as micro-dollars (`int64`) so there is no float rounding |
| Mode | `hard` blocks new calls; `soft` only notifies |
| Thresholds | notify at 50%, 80% and 100% by default, once each per period |

Starting defaults, all editable in the app: global $20 per month hard, `fude` $15 per month hard, every other service $3 per month hard, and each feature $1 per day soft.

**Pricing.** A `prices` table holds per-model rates (input, output, cache read, cache write per million tokens) with an `effective_from` date, so old ledger rows keep the price that applied then. Prices are seeded by migration and changed with `SetPrice`.

**Cheaper by design.** `pkg/llm` also applies prompt caching for the shared system prompt and voice samples, picks a smaller model where the feature's config allows (classification, scoring), and caches identical requests by prompt hash for 24 hours.

**Failure mode.** If Soroban is down, calls fail closed: jobs retry with backoff rather than spend unmetered. Reserve is a single-row update on a small table, so it adds a few milliseconds to a call that takes seconds.

## Torii: gateway, auth and the web contract

Torii is a backend-for-frontend: the web app speaks ConnectRPC to it, and it speaks gRPC to the services. It owns no business data, only sessions.

- **Public API.** A separate proto package `shogun.api.v1` shaped for screens (`DashboardService.GetToday`, `JobsService`, `ContactsService`, `DraftsService`, `InboxService`, `MailService` for connecting an account, `NotificationsService.Stream`). Internal protos can change without breaking the UI, and one screen is one call. `CostsService` backs the spend and budgets screen.
- **Web client.** `buf generate` also emits TypeScript with `@connectrpc/connect-web` into `web/src/gen`, so the UI is typed end to end. Connect speaks plain HTTP/1.1 JSON, so it is debuggable with `curl`.
- **Login.** Google OAuth (the same Google account Gmail uses), with an allowlist of one email. On success torii stores a session in `torii.sessions` and sets an `HttpOnly`, `Secure`, `SameSite=Lax` cookie valid for 30 days with sliding renewal.
- **CSRF.** Connect requires `Content-Type: application/json` or `application/proto` plus a `Connect-Protocol-Version` header, which a cross-site form cannot send; the cookie is also `SameSite`.
- **Internal identity.** For each downstream call torii signs a 60-second token (HMAC, `owner_id`, `request_id`) into `x-shogun-identity`; `pkg/authz` verifies it on every RPC. In production, service-to-service traffic also runs over mTLS from the mesh or cluster.
- **Live notifications.** `NotificationsService.Stream` is a Connect server stream that torii bridges from `taiko.Subscribe`; it reconnects with the last seen id.
- **Limits.** Per-session rate limit (token bucket, 20 rps burst 50), 10 s deadline on unary calls, request body cap 5 MB (CSV import).

## Background jobs and schedules

All background work runs on River inside the owning service, so jobs are transactional with that service's data and scale with its replicas. Times are in your local zone (Asia/Kolkata assumed).

| Service | Job | Trigger | Retry and limits |
| --- | --- | --- | --- |
| every service | `outbox_relay` | every 1 s + `NOTIFY` | n/a, idempotent |
| every service | `deliver_event` | per event per consumer | backoff up to 24 h |
| every service | `purge` | daily 03:00 | delivered outbox > 7 d, inbox > 30 d |
| kagami | `follow_up_scan` | daily 08:00 | emits `job.follow_up_due` and `contact.follow_up_due` once per due date |
| kagami | `stale_application_scan` | daily 08:00 | applications with no event in 14 days |
| tsubame | `gmail_sync` | every 5 min per account, plus Gmail push (Pub/Sub) later | incremental by `history_id`; full resync if the cursor expires |
| tsubame | `classify_message` | per new message | rules first, Claude API only when rules are unsure; 3 tries |
| fude | `generate_draft` | per GenerateDraft or event | concurrency 2, 5 tries, then `draft.failed`; token budget per day |
| fude | `embed_voice_sample` | per new sample | 3 tries |
| taiko | `daily_digest` | daily 08:30 | one digest, skipped if empty |
| katana | `github_sync` | daily 02:00 | GitHub API with ETag caching |
| katana | `suggest` | after sync or `learning.item_completed` | 3 tries |
| shinobi | `run_source` | each source's cron | per-source rate limit, robots.txt respected |
| shinobi | `score_posting` | per new posting | 3 tries |
| sensei | `rollup` | nightly 01:00 | rebuilds `daily_rollups` from `facts` |
| soroban | expire\_reservations | every 1 min | releases reservations past expires\_at |

All Claude API calls go through `fude` (and `tsubame` for classification) through pkg/llm, which meters every call against Soroban budgets (see Soroban: cost control); the model name per feature lives in config.

## Config, secrets, observability

**Config** is environment variables only (12-factor). Shared keys: `ENVIRONMENT`, `GRPC_ADDR` (:9090), `HTTP_ADDR` (:8080), `DATABASE_URL`, `MIGRATE_ON_START`, `LOG_LEVEL`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `INTERNAL_IDENTITY_KEY`, and `<SERVICE>_ADDR` for each dependency (`KAGAMI_ADDR`, `FUDE_ADDR`, …). Service-specific keys are prefixed with the service name, for example `FUDE_MODEL`, `FUDE_DAILY_TOKEN_BUDGET`, `TSUBAME_GMAIL_CLIENT_ID`.

**Secrets** never live in the repo. Locally they come from an untracked `.env`; in a cluster from External Secrets Operator syncing a cloud secret manager into Kubernetes secrets. Gmail and GitHub OAuth tokens are encrypted at rest with envelope encryption (a data key per row, wrapped by a master key from the secret store).

**Observability**

| Signal | How | Where |
| --- | --- | --- |
| Traces | OpenTelemetry interceptors on every gRPC client and server; `traceparent` carried inside event envelopes so a trace spans the outbox hop | OTLP to an OTel Collector, then Tempo or Jaeger |
| Metrics | RED metrics per RPC, River queue depth and job latency, outbox lag (age of the oldest undelivered row), Claude tokens used | Prometheus scrape of `/metrics` |
| Logs | JSON `slog` with `trace_id`, `request_id`, `owner_id`; never logs message bodies or tokens | stdout, collected by Loki |
| Alerts | outbox lag > 5 min, discarded jobs > 0, error rate > 2% for 10 min, Gmail sync failing for 1 h | Alertmanager to email |

Local compose runs the collector, Jaeger and Prometheus behind a `make up-observability` profile so the default stack stays light.

## Versioning, CI/CD and deploy

Each module releases on its own semantic version, driven by Conventional Commits and release-please in manifest mode; CI only builds and tests the modules a change touches.

**Version management**

| What | Pinned by |
| --- | --- |
| Go, golangci-lint, Node, Helm | `.tool-versions` (asdf or mise); CI reads the same file |
| buf, protoc-gen-go, protoc-gen-go-grpc, goose, sqlc | `tools/go.mod` `tool` directives; `make tools` installs them into `./bin` |
| Go dependencies | each module's `go.mod`; Dependabot opens one grouped PR per week per ecosystem |
| GitHub Actions | pinned to commit SHAs, bumped by Dependabot |
| Base images | `golang:<ver>` builder, `gcr.io/distroless/static-debian12:nonroot` runtime, digests pinned |

**Release versions.** Tags follow Go's submodule convention: `services/kagami/v0.4.0`, `services/torii/v0.2.0`, `pkg/v0.2.1`, `gen/go/v0.3.0`, `web/v0.1.0`. A `feat:` bumps minor, `fix:` bumps patch, `feat!:` bumps major (minor while below 1.0). Commit scopes name the module, for example `feat(kagami): import contacts`, and a PR title lint enforces the format.

**Workflows** (`.github/workflows/`)

| File | On | Does |
| --- | --- | --- |
| `ci.yml` | pull request, push to main | `changed-modules.sh` builds the matrix (a change in `pkg`, `gen` or `go.work` means every module); per module: golangci-lint, `go test -race`, build; Docker build without push for changed services |
| `proto.yml` | changes under `proto/` | `buf lint`, `buf breaking` against main, `buf generate` then fail if `gen/` differs |
| `pr-title.yml` | pull request | Conventional Commit title check |
| `release.yml` | push to main | release-please opens or updates one release PR per module; when merged it tags, writes CHANGELOGs and outputs the released paths, then builds and pushes `ghcr.io/sboy99/shogun-<svc>:<version>` (for example `shogun-kagami:0.4.0`) with SBOM and provenance |
| `deploy.yml` | release published, or manual | bumps the image tag in `deploy/helm/values/<env>/<svc>.yaml`; staging automatically, production behind a GitHub environment approval |
| `codeql.yml` | weekly, pull request | static security analysis |

**Deploy shape.** One generic Helm chart (`deploy/helm/service`) renders Deployment, Service, HPA, PodDisruptionBudget, ServiceMonitor and a migration Job hook; each service supplies only a values file. GitOps (Argo CD watching `deploy/helm/values`) is the target; the workflow writes the new tag and Argo rolls it out. Until a cluster exists, the same images run with `docker compose` on a single VM.

## How it scales, and open questions

Every piece can grow on its own without a rewrite: services are stateless, state is partitioned by schema, and the two seams most likely to change (event transport, database placement) sit behind one interface each.

| Pressure | First move | Next move |
| --- | --- | --- |
| More load on one service | HPA adds replicas; outbox relay and River use `SKIP LOCKED`, so replicas share work safely | Split its River queues onto a worker deployment |
| Event volume or more consumers | Raise relay batch size | Swap `pkg/bus` to NATS JetStream; routes become subjects, producers and consumers unchanged |
| One schema outgrows the shared database | Its role already sees only its schema | Move that schema to its own Postgres; only its `DATABASE_URL` changes |
| Read-heavy screens | Torii caches per-session reads for seconds | Read models fed by events (as `sensei` already is) |
| Multiple users | `owner_id` is on every row and in every identity token | Row-level security keyed on `owner_id` |
| AI cost | Soroban hard budgets per service and feature | Cache drafts by prompt hash, smaller model for classification |

