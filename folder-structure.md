# Folder structure

One Git repo (`github.com/0xHoaxen/shogun`), one `go.work`, twelve Go modules: `gen/go`, `pkg`, and ten services. `tools/` is a separate module kept out of the workspace so tool versions never leak into service builds.

```text
shogun/
├── README.md                    # what it is, quickstart, links to the design docs
├── AGENTS.md                    # rules for any coding agent
├── CLAUDE.md                    # Claude Code specifics, imports AGENTS.md
├── TODO.md                      # phased work queue
├── go.work                      # use ./gen/go ./pkg ./services/*
├── .tool-versions               # go, golangci-lint, node, helm (asdf/mise; CI reads it)
├── Makefile                     # single entry point for all dev commands
├── buf.yaml                     # lint + breaking rules for proto/
├── buf.gen.yaml                 # codegen: Go and TypeScript
├── sqlc.yaml                    # generates internal/store/db in each service
├── .golangci.yml                # lint config, depguard rules for module boundaries
├── .editorconfig  .gitignore  .dockerignore  .env.example
├── release-please-config.json   # per-module versioning
├── .release-please-manifest.json
│
├── proto/shogun/
│   ├── events/v1/envelope.proto         # Envelope wrapped around every event
│   ├── api/v1/*.proto                   # torii's public API for the web app
│   └── <service>/v1/
│       ├── <service>.proto              # that service's gRPC API
│       └── events.proto                 # payloads of events it publishes
│
├── gen/go/                      # generated Go, committed; module .../gen/go
│
├── pkg/                         # shared plumbing, module .../pkg, versioned alone
│   ├── config/    logger/    version/    telemetry/
│   ├── server/                  # gRPC + health + graceful shutdown
│   ├── postgres/                # pool with search_path, goose migrate, InTx
│   ├── outbox/    inbox/    bus/         # events: write, dedupe, route
│   ├── grpcclient/    authz/             # service-to-service calls and identity
│   ├── hanko/                   # approval token sign/verify
│   └── llm/                     # the only way to call Claude; meters via soroban
│
├── services/                    # each directory is its own Go module
│   ├── torii/                   # gateway
│   ├── kagami/                  # jobs and contacts
│   ├── tsubame/                 # mail
│   ├── fude/                    # drafts and approvals
│   ├── taiko/                   # notifications
│   ├── dojo/                    # learning
│   ├── katana/                  # profile suggestions
│   ├── shinobi/                 # job discovery
│   ├── sensei/                  # insights
│   └── soroban/                 # cost control
│       ├── go.mod               # replace => ../../pkg and ../../gen/go
│       ├── cmd/<name>/main.go   # wiring only
│       ├── internal/
│       │   ├── app/             # use cases, transactions, outbox writes
│       │   ├── domain/          # entities, state machines, domain errors (no I/O)
│       │   ├── store/           # repositories; queries/*.sql + sqlc output in db/
│       │   ├── transport/grpc/  # handlers: proto <-> domain, errors -> status
│       │   ├── events/          # inbox handlers for events this service consumes
│       │   └── jobs/            # River workers and periodic jobs
│       └── migrations/          # goose SQL, embedded in the binary
│
├── tools/                       # separate module; `tool` directives pin buf, protoc plugins, goose, sqlc
│   └── go.mod
│
├── web/                         # Next.js UI; own package.json and release
│   ├── src/app/                 # routes
│   ├── src/gen/                 # generated Connect clients (do not edit)
│   └── src/components/
│
├── deploy/
│   ├── docker/Dockerfile        # one multi-stage file, ARG SERVICE
│   ├── compose/
│   │   ├── compose.yaml         # local stack: postgres, ten services, torii, web
│   │   ├── compose.obs.yaml     # profile: OTel collector, Jaeger, Prometheus, Grafana
│   │   └── postgres/init.sql    # extensions, schema and role per service
│   └── helm/
│       ├── service/             # one generic chart
│       └── values/<env>/<service>.yaml
│
├── scripts/
│   ├── new-service.sh           # scaffold from templates/service
│   ├── rename-service.sh        # rename folder, proto pkg, schema, role, values, tags
│   ├── changed-modules.sh       # prints the CI matrix for a diff
│   └── templates/service/       # skeleton used by new-service.sh
│
├── docs/
│   ├── folder-structure.md      # this file
│   ├── design/                  # exported LLD and System Design
│   └── adr/                     # architecture decision records, one file per decision
│
└── .github/
    ├── workflows/
    │   ├── ci.yml               # changed modules: lint, test, build
    │   ├── proto.yml            # buf lint, breaking, generate-and-diff
    │   ├── pr-title.yml         # Conventional Commit check
    │   ├── release.yml          # release-please, images to GHCR
    │   ├── deploy.yml           # bump image tags, staging then production
    │   └── codeql.yml
    ├── dependabot.yml
    └── pull_request_template.md
```

## Where does new code go?

| I am adding... | Put it in |
| --- | --- |
| A business rule or state transition | `services/<name>/internal/domain` |
| A new use case | `services/<name>/internal/app` |
| A SQL query | `services/<name>/internal/store/queries/*.sql`, then `make sqlc` |
| A table change | new file in `services/<name>/migrations/` |
| An RPC | `proto/shogun/<name>/v1/<name>.proto`, then `make proto` |
| An event I publish | `proto/shogun/<name>/v1/events.proto` and a route in `pkg/bus/routes.go` |
| An event I consume | `services/<name>/internal/events` |
| A background job | `services/<name>/internal/jobs` |
| Code two services need | `pkg/<topic>`, only if it has no business meaning |
| A screen | `web/src/app`, calling a `torii` API from `proto/shogun/api/v1` |
| A deploy setting | `deploy/helm/values/<env>/<service>.yaml` |

## Module boundaries

```text
services/*  ──imports──▶  pkg, gen/go
pkg         ──imports──▶  gen/go            (never a service)
gen/go      ──imports──▶  nothing of ours
services/A  ──✗──▶        services/B        (call it over gRPC instead)
```

## Naming

- Directory, proto package, schema, role and env prefix are the same word: `kagami` / `shogun.kagami.v1` / schema `kagami` / `KAGAMI_ADDR`.
- Go module path for a service: `github.com/0xHoaxen/shogun/services/kagami`.
- Image: `ghcr.io/0xhoaxen/shogun-kagami:<version>`. Tag: `services/kagami/v0.4.0`.
