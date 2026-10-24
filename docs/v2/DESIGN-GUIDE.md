# v2 design guide

How to read the v2 mock and the rules to build it by. The mock wins on layout,
spacing, copy and interaction; `PLAN.md` decisions win over the mock.

## Reading the design files

`design/source/<Screen>.dc.html` is one screen each (`Main` is the Dashboard).
Each file has three parts:

1. `<helmet><style>`: the CSS. Token values, spacing, breakpoints and component styles live here.
2. `<x-dc>` markup: the layout. `{{ name }}` is a value from `renderVals()`; `<sc-for list=… as=…>` is a list; `<sc-if value=…>` is a condition; `onClick="{{ fn }}"` is a handler.
3. `<script type="text/x-dc" data-dc-script>`: the behaviour. Constants at the top (`PEOPLE`, `RULES`, `R_FIELDS`, …) are **placeholder data** that show the shape of what an RPC must return. `renderVals()` holds every interaction: filters, panels, steps, validation, toasts, undo.

To view a screen, open the file in Chromium with `support.js` beside it
(Playwright works: `page.goto('file://…/People.dc.html')`). `canvas.json` lists the
screens and their frame sizes. `design/screenshots/` has desktop (1440px), phone
(390px) and step-by-step flow renders. The mapping table in
`flow-people-import-3-check.png` renders only one row; the `mapRows` logic in
`People.dc.html` is the spec (one row per CSV column with an example value).

## Tokens

| Token | Value | Use |
| --- | --- | --- |
| `--bg` | `#1C1C1A` | page ground |
| `--panel` | `rgba(38,38,35,.62)` + `backdrop-filter: blur(6px)` | cards, widgets, active nav |
| `--line` | `#34342F` | solid rules |
| `--line2` | `#4A4942` | dashed rules, borders of buttons |
| `--text` | `#ECE6D3` | body text |
| `--muted` | `#ABA694` | labels, secondary text |
| `--cream` | `#D8D0B8` | primary "cream" buttons, selected segmented items |
| `--ink` | `#1C1C1A` | text on accent and cream |
| `--accent` | per page scheme | highlights, counts, focus ring, primary actions |

Accent schemes: Ember `#E8693C`, Moss `#9DAE6C`, Tide `#6FA8BE`, Plum `#C08AB0`.
Default per page: Dashboard Ember, Inbox Tide, Pipeline Moss, People Plum, Writing Ember,
Growth Moss, Insights Tide, Settings Plum. Users can change each in Settings > Appearance.

Type: Inter Tight 400/500/600 for UI and headings (h1 52/56, weight 500,
letter-spacing -0.035em, second line in `--muted`); IBM Plex Mono 400/500 for labels
(11px uppercase, 0.06em tracking), numbers (tabular) and buttons in mono; DM Serif
Display for display accents; Noto Serif JP 700 for the 将 mark. Body 14/22.

Density: `--rowpad` 12px comfortable, 7px compact.

Background: two soft radial glows (accent top right, cream bottom left) over a
16px dot grid. Gutters are 40px columns with diagonal hatching and dashed rules.

## Layout

- Top bar 64px: logo, "Next" strip with a dotted leader and status text, Search ⌘K on Dashboard.
- Sidebar 200px (60px collapsed): numbered rows `01 Dashboard` … `08 Settings`, 44px high, active row on `--panel` with a 2px accent inset, Inbox count in an accent chip.
- Page header: two-line h1 ("Who you know. / Who to write to."), then filter chips and the page's actions.
- Breakpoints: 1180px and 1100px tighten grids; below 860px the sidebar becomes a top menu, grids go to one column, side panels go full width, buttons stay at least 44px tall.
- Footer strip: "Page scheme: <name>" and "Nothing leaves without your approval".

## Patterns

- **Side panel** (Add person, Import, Add job, Resume): slides in from the right over a scrim, labelled by its eyebrow ("Bulk import", "New person"), Esc and × close, focus stays inside.
- **AI steps**: a `StepHeader` (1 Choose file, 2 AI reads it, 3 Check and import), then progress lines that tick one by one, then a review where every AI guess can be changed or unticked. Guesses carry a warn tag ("Check", "Already in People") and a one-line reason.
- **Toasts**: bottom, 10 seconds, always with Undo when the action can be undone. Copy says what happened ("5 people imported").
- **Nothing is sent**: every panel that saves something says so under its button ("Saved to People only. Nothing is sent.").
- **Buttons**: outline default; `cream` with clipped corners for the main page action; `acc` for the main action in a panel; disabled until the form is valid (Add model stays off until the test passes).
- **Tags**: mono 11px in a 1px box; `t-ok` accent, `t-warn` amber with a square bullet.
- **Accessibility**: real buttons and labelled inputs, `aria-pressed` on chips and segmented controls, `role=switch` on toggles, focus ring 2px accent with 2px offset, reduced motion stops all animation.

## Copy

The design already follows the owner decisions: Settings > AI models has no hosted default and no local-model copy, Accounts shows Calendar as "Coming soon" and says the resume file is not kept. Contact types in People still show the mock's Recruiter / Referral / Hiring manager / Other; ship the relationship values instead (recruiter, engineer, manager, alumni, friend, other, decision D5).
