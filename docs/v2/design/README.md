# v2 design

- `source/` the clickable mock, one `.dc.html` per screen (`Main` = Dashboard). Open in Chromium with `support.js` beside it; fonts load from Google Fonts. `support.js` is the mock's viewer runtime only, never ship it.
- `screenshots/` desktop 1440px, phone 390px, and flows (`flow-*`) step by step.
- `hero-ascii-reference/` the traced ASCII hero. Reference only, do not ship (decision D7).

Read `../docs/v2/DESIGN-GUIDE.md` (or `docs/v2/DESIGN-GUIDE.md` once copied into the repo) first.
