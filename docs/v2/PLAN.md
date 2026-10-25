# Shogun v2 plan

24 Oct 2026 · owner: sboy99 · compared against `0xHoaxen/shogun` `main` at `8c72f87`.

v2 is mostly a new web front end over the backend that already exists, plus three backend features: a tested AI provider (test before save), a richer People record and import, and resume parsing into a stored profile. The tasks are in `TODO-phase13.md`; the design is in `design/`.

## Owner decisions (final)

| # | Question | Decision |
| --- | --- | --- |
| D1 | Hosted "Shogun default" model? | No. Every user brings their own key. Until they do, AI features show "Set up an AI provider in Settings to use this" and stay off. |
| D2 | Local models (Ollama, LM Studio, no key)? | No, not for now. `pkg/netguard` keeps refusing private and local addresses; the API key stays required. |
| D3 | One active model or one per feature? | One active model for every feature. Per-feature routing is a later feature. |
| D4 | Keep the uploaded resume file? | No file storage (no S3). Read in memory, extract, save only the fields the user checked. |
| D5 | Contact types | The existing `relationship` values: recruiter, engineer, manager, alumni, friend, other. |
| D6 | Where schemes, density, motion, sidebar and Dashboard layout live | Browser `localStorage` only. |
| D7 | ASCII hero art | On hold for new original art (the traced figure may be copyrighted). Build the component with placeholder art. |
| D8 | Undo window for approved email | Yes, in v2. Held send inside tsubame, Hanko checks unchanged. |
| D9 | Google Calendar | "Coming soon" in Connected accounts. Planned for v3. |
| D10 | Discovery sources, Spend, GitHub, admin Users | Keep them as Settings tabs Discovery, Spend and Users (admins), GitHub in Connected accounts. Done in the design (`Settings.dc.html`). |
| D11 | One AI provider or several? | One per user, as today. Several models, an active switch and per-feature routing (D3) come later. The mock's model list, "In use" tag and Use/Remove controls are not built. |

Still open, marked `TODO(owner)` where they land: final hero art (D7), which logo mark ships (the mock uses A, Kabuto), and the page after sign-in (default `/dashboard`).

## Where the code stands, screen by screen

| v2 section | Today | Missing for v2 | Backend work |
| --- | --- | --- | --- |
| Shell | `AppShell` + `Rail`, light, 7 colours, no radii | Dark tokens, 4 accent schemes per page, density, collapsible numbered sidebar, Next hint, Undo toasts, side panels | None (local storage) |
| Dashboard | None, `/` redirects to `/jobs` | Greeting, ASCII hero, 7 widgets you can add, hide, reorder | None at first (composed from existing RPCs) |
| Inbox | `/drafts` + `/notifications` | One list of drafts, matches, status changes; J/K/A/S; versions; Regenerate modes | Status-change proposals (kagami) |
| Pipeline | `/jobs` board | Restyle, Add job panel with link autofill, history | None |
| People | `/contacts` table, AI column mapping on import | Relationship filters, detail pane with notes and linked roles, Mark contacted, Draft a note; editable mapping, per-row tick, duplicates, relationship guess | kagami + torii |
| Writing | `/drafts/[id]`, learning post dialog | Studio list, tone and length, approve checklist | Regenerate tone and length (fude) |
| Growth | `/learning` + `/profile` | Profile card, resume flow, skills table, heatmap, Close the gap | katana resume, profile, skills, job skills |
| Insights | `/insights` funnel + outreach | KPI deltas, weekly chart, drop-off, by-source table, noticed list | sensei by source |
| Settings | 8 sub-pages | v2 tabs, approval rules, undo window, signature, AI provider (test before save), appearance, delete drafts, Discovery/Spend/Users tabs and GitHub row | soroban, fude, torii, tsubame |

## Data model and API

| Service | Migration | API | Events |
| --- | --- | --- | --- |
| soroban | budget rows for `katana.resume_parse`, `katana.job_skills`; the `providers` row stays as it is | none | none |
| fude | none | Test unsaved details; Regenerate tone and length; `DiscardAllPending`; signature on email drafts | none |
| kagami | relationship CHECK; `status_proposals` | Contact relationship, how you know them, last contacted; `GetContact` with notes, history, linked jobs; `AddContactNote`, `MarkContacted`; import preview rows, mapping override, ticked rows; proposals | `contact.note_added` |
| katana | `profiles`, `profile_roles`, `skills`, `job_skills` | `ParseResume`, `SaveProfile`, `GetProfile`, `ListSkills` | `profile.updated`; consumes `job.added` |
| sensei | reply-time projection | By-source reply rate and median, weekly applied vs replied | none |
| tsubame | `sends` gains `held`, `cancelled`, `send_after` | `CancelSend` | `draft.sent` only after the real send |
| torii | `approval_rules`, signature | `ModelService`, approval rules, mappings for all of the above | none |

All proto changes are add-only inside `v1`, so `buf breaking` stays green.

## AI integration

Every new call goes through `pkg/llm` (metered by soroban, fails closed) with a feature name and a default budget row.
- **Resume parsing:** text extracted once in memory, strict JSON answer, resume treated as data, nothing saved until Add to profile.
- **Contact import:** unchanged from P11.13 (header + 5 sample rows only). v2 adds the user's mapping override and a rule-based relationship guess, so no new AI cost.
- **Job skills:** background, once per job, skipped with no active model.
- **Model test:** one short call on unsaved details, counted against the user's budget.

## Risks

| Risk | Mitigation |
| --- | --- |
| A held send weakens the approval gate | Token checks stay in `tsubame.Send`; the hold only delays the provider call; tests for cancel, late cancel, restart, double send, edit after approval. |
| Real-stack e2e is already flaky (P11.5e3) | Find or isolate it before V6.2. |
| Resume text carries instructions or junk | Strict schema, length caps, user checks every field. |
| Dark theme contrast on small mono labels | 4.5:1 check on every token pair in V1.1. |
| v1 and v2 screens side by side | Redirect each old route as its v2 screen lands. |
