## Phase 13: v2

The v2 redesign and the backend it needs. Plan, decisions and data model: `docs/v2/PLAN.md`. Design: `docs/v2/design/` (read `docs/v2/DESIGN-GUIDE.md` first). Same rules as every phase: first unchecked task whose `Needs:` are done, one task = one branch = one PR, run `Done when` exactly, tick in the same commit.

v2 rules on top of `AGENTS.md`:
- The decisions in `docs/v2/PLAN.md` are final; where the mock disagrees, the decision wins.
- The mock is a spec, not code. Rebuild it with our Next.js, Tailwind and ConnectRPC stack. Mock data is placeholder; every value comes from an RPC or is empty-state copy.
- Screen tasks compare against `docs/v2/design/screenshots/` at 1440px and 390px and attach Playwright screenshots to the PR.
- Web `Done when` is always: `cd web && npm run typecheck && npm run lint && npx playwright test`.

### V0. Setup and docs

- [x] **V0.0 Wire in the v2 handoff** (S)
  Do: append this file to `TODO.md` as Phase 13; add a "v2 work" row to the table in `CLAUDE.md` pointing at `docs/v2/`.
  Files: `TODO.md`, `CLAUDE.md`.
  Done when: `git diff --stat` shows only those two files plus `docs/v2/**`.

- [x] **V0.1 Design docs for v2** (S) Needs: V0.0
  Do: update `docs/shogun-hld.md`, `docs/shogun-lld.md`, `docs/shogun-ddl.md`, `docs/shogun-state-machines.md` for: kagami `status_proposals` and `contact.note_added`; katana `profiles`, `profile_roles`, `skills`, `job_skills`, `profile.updated`, consuming `job.added`; tsubame held sends (`sends.status` gains `held`, `send_after`, `CancelSend`) with the send state machine `held -> sending -> sent|failed`, `held -> cancelled`; torii `approval_rules`. AGENTS.md requires this before new tables or events.
  Files: `docs/*.md`.
  Done when: docs reviewed in the PR; `make lint`.

- [ ] **V0.2 Settings tabs for Discovery, Spend, Users and GitHub** (M) Needs: V1.3
  Do: decision D10. The design now has them: see `Settings.dc.html` (tabs `discovery`, `spend`, `users`, and the Accounts list) and `desktop-Settings-tab-*.png`. Rebuild today's discovery sources and preferences (`web/src/components/discovery`), spend and budgets (`components/spend`), admin users (`components/admin`, admins only) and GitHub (`components/github`, a row in Connected accounts) in that look, on the same APIs as today.
  Files: `web/src/components/settings/**`, the four component folders above.
  Done when: web checks.

### V1. Shell and design system

- [x] **V1.1 Design tokens** (M) Needs: V0.1
  Do: replace the spec v1 light tokens in `globals.css` with the v2 dark set from `DESIGN-GUIDE.md` (bg, panel, line, line2, text, muted, cream, accent), the four accent schemes Ember `#E8693C`, Moss `#9DAE6C`, Tide `#6FA8BE`, Plum `#C08AB0` as `--accent` set per page, fonts Inter Tight, IBM Plex Mono, DM Serif Display and Noto Serif JP (self-hosted woff2 like today's fonts, with their OFL files), density `--rowpad` 12px comfy / 7px compact. Keep the shadcn semantic mapping. Check every text/background pair for 4.5:1 and list the results in the PR.
  Files: `web/src/app/globals.css`, `web/src/app/fonts/**`, `web/src/app/layout.tsx`.
  Done when: web checks.

- [x] **V1.2 App shell and routes** (M) Needs: V1.1
  Do: replace `Rail` and `AppShell` with the v2 shell: top bar (logo mark, Next hint, status text), collapsible sidebar with 8 numbered sections and the Inbox count, footer strip ("Page scheme", "Nothing leaves without your approval"). Below 860px the sidebar becomes a top menu. New routes `/dashboard`, `/inbox`, `/pipeline`, `/people`, `/writing`, `/growth`, `/insights`; `/settings` keeps its path. Redirects: `/jobs -> /pipeline`, `/contacts -> /people`, `/drafts -> /inbox`, `/drafts/[id] -> /writing?draft=id`, `/learning` and `/profile -> /growth`, `/notifications -> /inbox`, `/discovery -> /settings?tab=discovery`. Old screens render inside the new shell until their own task replaces them. Login lands on `/dashboard` (`TODO(owner)` if you want `/inbox` instead).
  Files: `web/src/components/shell/**`, `web/src/app/(app)/**`, `web/src/app/page.tsx`, `web/src/content/sections.ts`.
  Done when: web checks; existing specs updated for the new URLs.

- [x] **V1.3 Shared v2 components** (M) Needs: V1.1
  Do: `SidePanel` (slides in from the right, scrim, Esc closes, focus trap, returns focus), `Toast` with Undo and a 10 second timer, `FilterChips` with counts and `aria-pressed`, `Tag` (default, ok, warn), `Toggle` (`role=switch`), `Segmented`, `StepHeader` (1/2/3 with done and current), `ProgressLines` (the "AI reads it" list with done, now, pending). Each with a Playwright component spec or a page spec.
  Files: `web/src/components/ui/**`.
  Done when: web checks.

- [x] **V1.4 Appearance in local storage** (S) Needs: V1.1
  Do: decision D6. One `useAppearance` hook for page schemes, density, motion, sidebar collapsed, Dashboard layout, stored in `localStorage` under one versioned key, wrapped in try/catch with defaults when storage is empty or blocked. Motion off also follows `prefers-reduced-motion`.
  Files: `web/src/lib/appearance.ts`, shell.
  Done when: web checks.

### V2. AI provider (Settings)

- [x] **V2.1 Several models per user: soroban** (M)
  Dropped: one AI provider per user for now (D11). The existing `providers` row, `SetProvider`, `GetProvider` and `DeleteProvider` stay as they are.

- [x] **V2.2a Meter a test of unsaved details** (S) Needs: V2.0
  Do: found while planning V2.2: `Reserve` returns the saved provider and fails for a user with none, so a first-time user could not be tested. Add `ReserveRequest.unsaved_model` (add-only), allowed for `fude.test_connection` only, which prices and holds the call by model name without opening a provider (a saved provider's token cap still applies); add `pkg/llm` `Client.CompleteWith(ctx, feature, access, req)`. The key never reaches soroban.
  Files: `proto/shogun/soroban/v1/soroban.proto`, `services/soroban/internal/app/reserve.go`, `pkg/llm/llm.go`.
  Done when: `make proto && cd services/soroban && go test -race ./...`, `cd pkg && go test -race ./llm/...`, `make lint`.

- [ ] **V2.2 Test unsaved details** (M) Needs: V2.2a
  Do: a test that takes base URL, model and API key (not stored), makes one short call through `pkg/llm` with `netguard`, and returns ok, failure kind, latency and the model that answered. API key required; private and local addresses refused as `NOT_PUBLIC` (decision D2). Add failure kinds for a base URL ending in `/chat/completions`. Counted against the caller's budget like today's test.
  Files: `services/fude/internal/app/provider.go`, `proto/shogun/fude/v1/fude.proto`.
  Done when: `make proto && cd services/fude && go test -race ./...`, `make lint`.

- [ ] **V2.3 Test and save a provider in torii** (M) Needs: V2.2
  Do: `ProviderService.TestProvider` takes unsaved base URL, model and API key and returns the test result of V2.2. `SetProvider` runs the same test server side and refuses a failing provider, so Save can never store one. Key stays write-only and scrubbed from errors as today.
  Files: `proto/shogun/api/v1/providers.proto`, `services/torii/internal/transport/connectapi/providers.go`.
  Done when: `make proto && cd services/torii && go test -race ./...`.

- [ ] **V2.4 AI provider tab** (M) Needs: V2.3, V1.3
  Do: Settings > AI as in `Settings.dc.html` and `flow-settings-ai-models-*.png`, for one provider (D11): base URL, model name, API key with Show/Hide, Test connection, a result line that says why it failed, Save disabled until a passing test of the exact current values. No model list, "In use" tag, Use this model or Remove-with-Undo; Delete provider stays as today. The design already leaves out a hosted default and local models. With no provider: "Set up an AI provider to use AI features" here and on every AI action (decision D1).
  Files: `web/src/components/provider/**`, `web/src/app/(app)/settings/**`.
  Done when: web checks.

### V3. People

- [ ] **V3.1 Contact fields and notes: kagami** (M) Needs: V0.1
  Do: expose `relationship` (recruiter, engineer, manager, alumni, friend, other; decision D5, add a CHECK in a new migration after mapping unknown values to other), `how_we_met`, `last_contacted` on Contact. `GetContact` returns notes and history from `contact_events` and jobs linked by company. New `AddContactNote` (event `contact.note_added`) and `MarkContacted` (status to reached out when allowed, sets `last_contacted`).
  Files: `services/kagami/**`, `proto/shogun/kagami/v1/*.proto`, `pkg/bus/routes.go`.
  Done when: `make proto && cd services/kagami && go test -race ./...`, `make lint`.

- [ ] **V3.2 Import preview per row: kagami app** (M) Needs: V3.1
  Do: the dry run returns every row: index, name, role, company, email, guessed relationship (rules on the title, no AI call: recruit/talent/sourc to recruiter; manager/head/director/vp/lead/cto to manager; engineer/developer/swe to engineer; else other), and a duplicate flag with the matching contact id (same email or LinkedIn, else same normalised name and company). Keep the AI column mapping from P11.13.
  Files: `services/kagami/internal/app/importcsv*.go`.
  Done when: `cd services/kagami && go test -race ./internal/app/...`.

- [ ] **V3.3 Import with your choices: kagami** (M) Needs: V3.2
  Do: `ImportContacts` accepts the user's mapping (overrides the AI's, same validation) and the list of row indexes to import; duplicates are skipped unless ticked. Limit 5,000 rows (reason `IMPORT_TOO_LARGE`).
  Files: `services/kagami/**`, `proto/shogun/kagami/v1/kagami.proto`.
  Done when: `make proto && cd services/kagami && go test -race ./...`.

- [ ] **V3.4 People API in torii** (S) Needs: V3.1, V3.3
  Do: the new fields and RPCs on `shogun.api.v1.ContactsService`, reasons kept.
  Files: `proto/shogun/api/v1/contacts.proto`, `services/torii/internal/transport/connectapi/contact*.go`.
  Done when: `make proto && cd services/torii && go test -race ./...`.

- [ ] **V3.5 People screen** (M) Needs: V3.4, V1.3
  Do: `People.dc.html`: list and detail, filter chips per relationship plus Write back (follow-up due), detail with email, last contact, how you know them, linked roles, notes and history, Add note, Mark contacted (Undo), Draft a note (fude `GenerateDraft`, goes to Inbox), empty state, Add person side panel (only name required; relationship choice).
  Files: `web/src/components/contacts/**` (rename to `people/` is fine), `web/src/app/(app)/people/page.tsx`.
  Done when: web checks.

- [ ] **V3.6 Import panel** (M) Needs: V3.5
  Do: `flow-people-import-*.png`: step 1 choose file (CSV up to 5,000 rows); step 2 AI reads it with progress lines; step 3 mapping table (your column, goes to, example, editable select) and per-row tick list with relationship tag or "Already in People"; Import N people; toast with Undo (archives the imported rows). No provider: the mapping step falls back to the template columns, as today.
  Files: `web/src/components/contacts/import-*.tsx`.
  Done when: web checks.

### V4. Growth and resume

- [ ] **V4.1 Resume text extraction: katana** (M) Needs: V0.1
  Do: decision D4. Read PDF, DOCX and TXT up to 10 MB in memory only; never write the file to disk or the database. DOCX via `archive/zip` + `encoding/xml`; PDF needs a library, name it and say why in the PR. Reject scanned PDFs with no text (`RESUME_NO_TEXT`).
  Files: `services/katana/internal/resume/**`.
  Done when: `cd services/katana && go test -race ./...`, `make lint`.

- [ ] **V4.2 Resume parsing with AI: katana** (M) Needs: V4.1
  Do: feature `katana.resume_parse` in `pkg/llm` config and a soroban seed-migration budget row (`TODO(owner)` to confirm amounts). `ParseResume` returns name, headline, location, email, years of experience, roles (title, company, start, end), skills with a 0 to 10 evidence score, and per field a `needs_check` flag with a reason. Strict JSON validation like `importcsv_reshape.go`; the resume is data in the prompt, never instructions; cap text length. Stores nothing.
  Files: `services/katana/internal/app/resume*.go`, `pkg/llm/config.go`, `services/soroban/migrations/`.
  Done when: `cd services/katana && go test -race ./...`, `cd pkg && go test -race ./llm/...`.

- [ ] **V4.3 Profile and skills store: katana** (M) Needs: V4.2
  Do: tables `profiles` (one per owner), `profile_roles`, `skills` (name, level 0 to 10, source). `SaveProfile` writes only what the user kept; an existing skill's level only goes up. `GetProfile`, `ListSkills`. Event `profile.updated`.
  Files: `services/katana/migrations/`, `services/katana/internal/store/**`, `proto/shogun/katana/v1/*.proto`, `pkg/bus/routes.go`.
  Done when: `make proto && cd services/katana && go test -race ./...`.

- [ ] **V4.4 Skills asked by saved roles: katana** (M) Needs: V4.3
  Do: consume `job.added`, fetch the description from kagami, extract skills with feature `katana.job_skills` (skip when no active model), store in `job_skills`. `ListSkills` returns "asked in N of your M saved roles" per skill.
  Files: `services/katana/internal/events/**`, `services/katana/internal/jobs/**`.
  Done when: `cd services/katana && go test -race ./...`.

- [ ] **V4.5 Profile API in torii** (S) Needs: V4.3
  Do: `ParseResume` (bytes up to 10 MB), `SaveProfile`, `GetProfile`, `ListSkills` on `shogun.api.v1.ProfileService`.
  Done when: `make proto && cd services/torii && go test -race ./...`.

- [ ] **V4.6 Growth screen** (M) Needs: V4.5, V1.3
  Do: `Growth.dc.html`: Your profile card (empty state with Add resume), skills table (you vs asked), learning streak heatmap from dojo activities with 4, 6 and 12 week ranges, learning log with tags and Add entry, Close the gap (rules from the skills gap, no AI), GitHub suggestions kept as a section.
  Files: `web/src/components/growth/**`, `web/src/app/(app)/growth/page.tsx`.
  Done when: web checks.

- [ ] **V4.7 Resume panel** (M) Needs: V4.6
  Do: `flow-growth-resume-*.png`: upload (PDF, Word, text, 10 MB); AI reads it with progress lines; check every field (Check tag and reason on guesses), untick roles and skills, "new" on unknown skills; Add to profile; toast with Undo; button becomes Replace resume.
  Done when: web checks.

### V5. The other screens

- [ ] **V5.1 ASCII hero component** (M) Needs: V1.4
  Do: the canvas animation in `Main.dc.html` (`componentDidMount` draw loop): background code flicker, scrolling code inside the figure, scan band every 8 s, rare sideways glitch, accent colour from `--accent`, figure right of the text on desktop with 40px padding, behind the text at half strength below 860px, still under reduced motion, paused off screen. Use a neutral placeholder grid. `TODO(owner)`: final art (decision D7). Never use `docs/v2/design/hero-ascii-reference`.
  Files: `web/src/components/dashboard/ascii-hero.tsx`.
  Done when: web checks.

- [ ] **V5.2 Dashboard** (M) Needs: V5.1, V1.4
  Do: `Main.dc.html`: date and greeting, "N things need you" with Open inbox, the 7 widgets (needs you, pipeline by stage, follow-ups due, AI budget, top matches, learning streak, what Shogun did with Undo) from existing RPCs, Add widget, hide, reorder, reset, density, saved via `useAppearance`.
  Done when: web checks.

- [ ] **V5.3 Inbox** (M) Needs: V1.3
  Do: `Inbox.dc.html`: one list of drafts, matches and status changes; filters; J/K move, A approve, S skip (no global handlers while typing); detail with versions, Copy, Regenerate with modes; approve goes through the existing Approve.
  Done when: web checks.

- [ ] **V5.4 Status changes to confirm: kagami** (M) Needs: V0.1
  Do: when the owner's auto-move rule is off, `mail.classified` creates a `status_proposals` row instead of moving the job; `ListStatusProposals`, `ConfirmStatusProposal`, `DismissStatusProposal`; torii mapping; Inbox shows them.
  Done when: `make proto && make test`.

- [ ] **V5.5 Pipeline** (M) Needs: V1.3
  Do: `Pipeline.dc.html` and `flow-pipeline-add-job-1.png`: board restyle, Add job side panel (posting link fills company from the host name), detail with history and Move to.
  Done when: web checks.

- [ ] **V5.6 Writing studio** (M) Needs: V1.3
  Do: `Writing.dc.html`: list by kind, editor with versions, tone and length, Before you approve checklist, source from your log. fude `Regenerate` gains `tone` and `target_words` (add-only proto fields).
  Done when: `make proto && cd services/fude && go test -race ./...`; web checks.

- [ ] **V5.7 Insights data: sensei** (M) Needs: V0.1
  Do: reply rate and median days to reply by source, weekly applied vs replied.
  Done when: `make proto && cd services/sensei && go test -race ./...`.

- [ ] **V5.8 Insights screen** (M) Needs: V5.7
  Do: `Insights.dc.html`: KPIs with deltas, weekly chart, stage drop-off, source table, What Shogun noticed (rules, no AI).
  Done when: web checks.

- [ ] **V5.9 Settings screen** (M) Needs: V2.4, V0.2
  Do: `Settings.dc.html`: tabs All, Approvals, Accounts, AI provider, Appearance, Notifications, plus Discovery, Spend, Users from V0.2. Accounts: Gmail, GitHub, LinkedIn (manual), Resume, Google Calendar with a "Coming soon" tag and no button (decision D9). Appearance via `useAppearance`. Delete drafts with a confirm step.
  Done when: web checks.

- [ ] **V5.10 Approval rules and signature** (M) Needs: V5.4
  Do: torii `approval_rules` per owner (auto-move jobs, suggest profile changes, bring matches to Inbox with score threshold 75); kagami, katana and shinobi read them. Email signature stored per owner and appended by fude to email drafts. "Ask before sending any email" is always on and not stored.
  Done when: `make proto && make test`; web checks.

- [ ] **V5.11a Undo window: tsubame** (M) Needs: V0.1
  Do: decision D8 (yes). After `Send` verifies the Hanko token (unchanged), the send is stored as `held` with `send_after = now + hold` (5, 10, 30 or 60 s, owner setting, default 10); a River job sends it after the hold; `CancelSend` moves `held -> cancelled` and only the owner can call it. Token expiry must be longer than the longest hold. `draft.sent` only after the real send. Tests: cancel in time, cancel too late, restart during hold, double send, edit after approval.
  Files: `services/tsubame/**`, `pkg/hanko` only if the expiry needs a change.
  Done when: `make proto && cd services/tsubame && go test -race ./...`, `cd pkg && go test -race ./hanko/...`.

- [ ] **V5.11b Undo window: torii and web** (M) Needs: V5.11a
  Do: hold setting in Settings > Approvals; after Approve, a toast with Undo for the hold time calls `CancelSend`.
  Done when: `cd services/torii && go test -race ./...`; web checks.

- [ ] **V5.12 Delete all drafts** (S)
  Do: fude `DiscardAllPending`, torii mapping, Settings button with confirm.
  Done when: `make proto && cd services/fude && go test -race ./...`.

### V6. Finish

- [ ] **V6.1 Remove v1 leftovers** (S) Needs: all of V5
  Do: delete unused v1 components and routes (keep redirects).
  Done when: `make test`; web checks.

- [ ] **V6.2 End-to-end tests** (M) Needs: V6.1
  Do: Playwright specs per v2 screen; compose specs for import, add model and resume parse against the mock APIs. The real-stack flake from P11.5e3 must be found or isolated first.
  Done when: `cd web && npx playwright test && npm run test:e2e:compose`.

- [ ] **V6.3 Release** (S) Needs: V6.2
  Do: version bumps and changelogs through release-please, staging deploy. `TODO(owner)`: production go-ahead.
