# CLAUDE.md

@AGENTS.md

The rules, commands and architecture in `AGENTS.md` apply in full. This file adds how Claude Code should work in this repo.

## How to work

- **Start every session** by reading `TODO.md` and picking the next unchecked task whose `Needs:` are done. Say which task you are taking before editing.
- **Plan first for anything touching more than three files** (new service, new migration plus handlers, anything in `pkg/`). Use plan mode, list the files, then implement.
- **One task, one branch, one PR.** Branch name `task/<id>-<slug>` (for example `task/P6.3-kagami-jobs-store`). Never commit to `main`.
- **Verify before claiming done.** Run the exact commands in the task's `Done when:`. Report real output; if something fails, say so and fix it rather than loosening the check.
- **Keep diffs small.** If a task grows past about 400 changed lines excluding generated code, split it and add the new tasks to `TODO.md`.
- **Tick the box** for the task in the same commit and add any follow-up tasks you discovered under the right phase.
- **Use subagents** for independent, read-heavy work (for example surveying how three services implement the same pattern). Do edits in the main session so changes stay coherent.

## Where things are

| Need | Look at |
| --- | --- |
| What to do next | `TODO.md` |
| Repo layout and what goes where | `docs/folder-structure.md` |
| Rules and conventions | `AGENTS.md` |
| Table definitions | `services/<name>/migrations/` (source of truth once written) |
| RPC and event contracts | `proto/shogun/<name>/v1/` |
| Design intent | Shogun LLD and Shogun System Design docs (linked in `README.md`) |

## Generated and protected files

Do not hand-edit: `gen/go/**`, `web/src/gen/**`, `go.work.sum`, any `*_sqlc.go` / `internal/store/db/*.go`. Regenerate with `make proto` or `make sqlc`.

Never touch without being asked: `.github/workflows/release.yml`, `deploy/helm/values/production/**`, `.release-please-manifest.json`.

## Skeleton rules for new code

- New service: `make new-service NAME=<name>`, never copy-paste another service.
- New RPC: edit the proto, run `make proto`, implement in `internal/transport/grpc`, call into `internal/app`, add a handler test.
- New table: new goose migration in the owning service, a sqlc query file, a store method, a store integration test.
- New event: payload message in the producer's `events.proto`, route in `pkg/bus/routes.go`, producer test, consumer handler in `internal/events` with an idempotency test.
- New Claude call: add a feature name to `pkg/llm` config and a default budget row in `soroban`'s seed migration. Do not call the SDK directly.

## Style for answers and PRs

- Lead with what changed and what you ran to check it. Keep it short.
- PR body: **Before** / **After** / **How** paragraphs, then the task id. Note any rule from `AGENTS.md` that needed a judgement call.
- When blocked on a decision that is the owner's (budget numbers, which job sources, prompts and voice), leave a clearly marked `TODO(owner):` and move on to the next task.

## Commit trailer

Follow the attribution lines supplied by the environment for commits and PRs; do not invent your own.
