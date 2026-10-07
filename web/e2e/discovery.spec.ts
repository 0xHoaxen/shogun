import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { DiscoveryService, DiscoverySourceKind } from "../src/gen/shogun/api/v1/discovery_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const WHEN = timestampFromDate(new Date("2026-10-06T09:00:00Z"));

function posting(over: Record<string, unknown> = {}) {
  return {
    id: "p1", sourceId: "s1", title: "Backend Engineer", company: "Acme", url: "https://acme.example/1", location: "Remote",
    scored: true, score: 0.82, reasons: ["the title matches the wanted role backend engineer"], scoredBy: "rule", savedJobId: "",
    createdAt: WHEN, ...over,
  };
}

function source(over: Record<string, unknown> = {}) {
  return {
    id: "s1", name: "Acme feed", kind: DiscoverySourceKind.RSS, schedule: "0 7 * * *", enabled: true, lastError: "",
    config: { url: "https://acme.example/feed.xml", document: "" }, ...over,
  };
}

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
}

test("postings are listed with score and reasons, and the filter asks for the chosen minimum", async ({ page }) => {
  await signIn(page);
  const calls = await mockRpc(page, DiscoveryService.method.listDiscoveryPostings, (body) => ({
    postings: body.minScore === 0.85 ? [] : [posting(), posting({ id: "p2", title: "Unscored role", scored: false, score: 0, reasons: [] })],
  }));

  await page.goto("/discovery");

  const card = page.getByRole("article", { name: "Backend Engineer" });
  await expect(card).toContainText("82%");
  await expect(card).toContainText("Acme · Remote");
  await expect(card.getByRole("list", { name: "Why" })).toContainText("matches the wanted role");
  await expect(card.getByRole("link", { name: "Open posting" })).toHaveAttribute("rel", /noopener/);
  await expect(page.getByRole("article", { name: "Unscored role" })).toContainText("Not scored yet");

  await page.getByRole("button", { name: "85% and up" }).click();

  await expect(page.getByText(/Nothing here yet/)).toBeVisible();
  expect(calls.at(-1)?.body).toMatchObject({ minScore: 0.85 });
});

test("saving a posting asks for it and then shows it on the board", async ({ page }) => {
  await signIn(page);
  let saved = "";
  await mockRpc(page, DiscoveryService.method.listDiscoveryPostings, () => ({ postings: [posting({ savedJobId: saved })] }));
  const saves = await mockRpc(page, DiscoveryService.method.saveDiscoveryPosting, () => {
    saved = "job1";
    return { jobId: "job1" };
  });
  await page.goto("/discovery");

  await page.getByRole("button", { name: "Save Backend Engineer to the tracker" }).click();

  await expect(page.getByRole("link", { name: "Saved to the board" })).toHaveAttribute("href", "/jobs");
  expect(saves[0].body).toMatchObject({ postingId: "p1" });
});

test("a posting that cannot be saved says why", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, DiscoveryService.method.listDiscoveryPostings, { postings: [posting()] });
  await mockRpcError(page, DiscoveryService.method.saveDiscoveryPosting, "unavailable", "x", "TRACKER_UNAVAILABLE");
  await page.goto("/discovery");

  await page.getByRole("button", { name: "Save Backend Engineer to the tracker" }).click();

  await expect(page.locator("p[role=alert]")).toContainText("tracker is busy");
});

test("an unsafe posting link is not shown", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, DiscoveryService.method.listDiscoveryPostings, { postings: [posting({ url: "javascript:alert(1)" })] });

  await page.goto("/discovery");

  await expect(page.getByRole("article", { name: "Backend Engineer" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open posting" })).toHaveCount(0);
});

test("sources show their last failure and can be read now", async ({ page }) => {
  await signIn(page);
  let failed = "robots_disallowed";
  await mockRpc(page, DiscoveryService.method.listDiscoverySources, () => ({ sources: [source({ lastError: failed })] }));
  const runs = await mockRpc(page, DiscoveryService.method.runDiscoverySource, () => {
    failed = "";
    return { fetched: 12, added: 5 };
  });
  await page.goto("/discovery");
  await page.getByRole("button", { name: "Sources" }).click();

  const card = page.getByRole("article", { name: "Acme feed" });
  await expect(card).toContainText("RSS or Atom feed");
  await expect(card).toContainText("robots.txt does not allow");
  await card.getByRole("button", { name: "Read Acme feed now" }).click();

  await expect(page.getByRole("status")).toContainText("Read 12 postings, 5 of them new.");
  await expect(card).not.toContainText("Last read failed");
  expect(runs[0].body).toMatchObject({ id: "s1" });
});

test("a source that cannot be read explains it", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, DiscoveryService.method.listDiscoverySources, { sources: [source()] });
  await mockRpcError(page, DiscoveryService.method.runDiscoverySource, "failed_precondition", "x", "SOURCE_ADDRESS_NOT_PUBLIC");
  await page.goto("/discovery");
  await page.getByRole("button", { name: "Sources" }).click();

  await page.getByRole("button", { name: "Read Acme feed now" }).click();

  await expect(page.locator("p[role=alert]")).toContainText("not a public one");
});

test("adding an API source sends its address, schedule and field mapping", async ({ page }) => {
  await signIn(page);
  const stored: ReturnType<typeof source>[] = [];
  await mockRpc(page, DiscoveryService.method.listDiscoverySources, () => ({ sources: stored }));
  const saves = await mockRpc(page, DiscoveryService.method.saveDiscoverySource, (body) => {
    stored.push(source({ id: "s2", name: String((body.source as { name: string }).name) }));
    return { source: stored[0] };
  });
  await page.goto("/discovery");
  await page.getByRole("button", { name: "Sources" }).click();

  await page.getByRole("button", { name: "Add source" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Add source" });
  await dialog.getByLabel("Name").fill("API board");
  await dialog.getByLabel("Kind").selectOption({ label: "JSON API" });
  await dialog.getByLabel("Address (https)").fill("https://jobs.example.com/api");
  await dialog.getByLabel("Where the list is (blank if the document is the list)").fill("data.jobs");
  await dialog.getByLabel("Id field").fill("id");
  await dialog.getByLabel("Title field").fill("title");
  await dialog.getByLabel("Company field").fill("company.name");
  await dialog.getByLabel("Schedule (cron, India time)").fill("30 6 * * *");
  await dialog.getByRole("button", { name: "Add source" }).click();

  await expect(page.getByRole("article", { name: "API board" })).toBeVisible();
  expect(saves[0].body).toMatchObject({
    source: {
      name: "API board", kind: "DISCOVERY_SOURCE_KIND_API", schedule: "30 6 * * *", enabled: true,
      config: { url: "https://jobs.example.com/api", mapping: { itemsPath: "data.jobs", id: "id", title: "title", company: "company.name" } },
    },
  });
});

test("a refused source shows the server's validation message", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, DiscoveryService.method.listDiscoverySources, { sources: [] });
  await mockRpcError(page, DiscoveryService.method.saveDiscoverySource, "invalid_argument", "invalid input: url must be an https address without credentials", "INVALID_ARGUMENT");
  await page.goto("/discovery");
  await page.getByRole("button", { name: "Sources" }).click();

  await page.getByRole("button", { name: "Add source" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Add source" });
  await dialog.getByLabel("Name").fill("x");
  await dialog.getByLabel("Address (https)").fill("https://x.example/feed");
  await dialog.getByRole("button", { name: "Add source" }).click();

  await expect(dialog.getByRole("alert")).toHaveText("url must be an https address without credentials");
});

test("a source can be turned off with its other fields kept", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, DiscoveryService.method.listDiscoverySources, { sources: [source()] });
  const saves = await mockRpc(page, DiscoveryService.method.saveDiscoverySource, { source: source({ enabled: false }) });
  await page.goto("/discovery");
  await page.getByRole("button", { name: "Sources" }).click();

  await page.getByRole("button", { name: "Turn off Acme feed" }).click();

  await expect.poll(() => saves.length).toBe(1);
  // A false flag is the proto3 default, so it is left out of the JSON.
  const sent = (saves[0].body as { source: Record<string, unknown> }).source;
  expect(sent).toMatchObject({ id: "s1", name: "Acme feed", config: { url: "https://acme.example/feed.xml" } });
  expect(sent.enabled).toBeUndefined();
});

test("preferences load, save as lists and a minimum from 0 to 1", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, DiscoveryService.method.getDiscoveryPreferences, {
    preferences: { roles: ["backend engineer"], locations: ["remote"], mustHave: ["go", "postgres"], niceToHave: [], exclude: ["unpaid"], minScore: 0.7 },
  });
  const sets = await mockRpc(page, DiscoveryService.method.setDiscoveryPreferences, { preferences: {} });
  await page.goto("/discovery");
  await page.getByRole("button", { name: "What you want" }).click();

  await expect(page.getByLabel("Terms a posting must mention")).toHaveValue("go, postgres");
  await page.getByLabel("Roles you want (matched in the title)").fill("sre, platform engineer ,");
  await page.getByLabel(/Count a posting as a match at/).fill("60");
  await page.getByRole("button", { name: "Save preferences" }).click();

  await expect(page.getByRole("status")).toContainText("scored again");
  expect(sets[0].body).toMatchObject({
    preferences: { roles: ["sre", "platform engineer"], locations: ["remote"], mustHave: ["go", "postgres"], exclude: ["unpaid"], minScore: 0.6 },
  });
});

test("a failed postings load offers a retry and the nav has a Discovery tab", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, DiscoveryService.method.listDiscoveryPostings, "internal", "boom");

  await page.goto("/discovery");

  await expect(page.getByText("Couldn't load your postings.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Discovery" })).toBeVisible();
});
