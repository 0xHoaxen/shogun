import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

import { expect, test } from "@playwright/test";

// The v1 colour names kept as aliases in globals.css while screens move to v2.
// A folder listed here has moved, and must not use them again.
const MIGRATED = ["ui", "settings", "discovery", "spend", "admin", "github", "provider", "shell", "contacts"];
const LEGACY = /\b(border-ink|bg-ink|text-ink|bg-strip|bg-paper|text-paper|bg-field|bg-signal|border-signal|text-muted-ink|text-rail-ink)\b/;

// The rail frames the login and legal pages, which are rebuilt in their own task.
const NOT_YET = new Set(["rail.tsx"]);

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.(tsx?|css)$/.test(name) && !NOT_YET.has(name) ? [path] : [];
  });
}

for (const folder of MIGRATED) {
  test(`${folder} uses the v2 colours only`, () => {
    const offenders = sources(join(__dirname, "../src/components", folder)).filter((file) =>
      LEGACY.test(readFileSync(file, "utf8")),
    );
    expect(offenders).toEqual([]);
  });
}
