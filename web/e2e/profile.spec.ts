import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { ProfileService, SuggestionState, SuggestionTarget } from "../src/gen/shogun/api/v1/profile_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const WHEN = timestampFromDate(new Date("2026-10-06T09:00:00Z"));

interface Init {
  id: string;
  target?: SuggestionTarget;
  section?: string;
  before?: string;
  after?: string;
  reason?: string;
  state?: SuggestionState;
  evidence?: { label: string; url: string }[];
}

function suggestion(i: Init) {
  return {
    target: SuggestionTarget.RESUME,
    section: "projects",
    before: "",
    after: "Built a worker pool in Go",
    reason: "Merged three pull requests",
    state: SuggestionState.OPEN,
    evidence: [{ label: "PR 12", url: "https://github.com/x/y/pull/12" }],
    createdAt: WHEN,
    ...i,
  };
}

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
}

test("open suggestions are listed with their text, reason and evidence, and the filter asks for the chosen state", async ({ page }) => {
  await signIn(page);
  const calls = await mockRpc(page, ProfileService.method.listProfileSuggestions, (body) => ({
    suggestions:
      body.state === "SUGGESTION_STATE_ACCEPTED"
        ? [suggestion({ id: "s2", state: SuggestionState.ACCEPTED, after: "Went to Rust" })]
        : [suggestion({ id: "s1", target: SuggestionTarget.LINKEDIN, section: "skills", before: "Go", after: "Go, Rust" })],
  }));

  await page.goto("/profile");

  const card = page.getByRole("article", { name: "LinkedIn Skills suggestion" });
  await expect(card).toContainText("Go, Rust");
  await expect(card).toContainText("Merged three pull requests");
  await expect(card.getByText("Go", { exact: true })).toHaveClass(/line-through/);
  await expect(card.getByRole("link", { name: "PR 12" })).toHaveAttribute("href", "https://github.com/x/y/pull/12");
  await expect(card.getByRole("link", { name: "PR 12" })).toHaveAttribute("rel", /noopener/);
  await expect(card.getByRole("button", { name: "Accept", exact: true })).toBeVisible();
  expect(calls[0].body).toMatchObject({ state: "SUGGESTION_STATE_OPEN" });

  await page.getByRole("button", { name: "Accepted", exact: true }).click();

  const accepted = page.getByRole("article", { name: "Resume Projects suggestion" });
  await expect(accepted).toContainText("Went to Rust");
  await expect(accepted).toContainText("Accepted");
  await expect(accepted.getByRole("button", { name: "Accept", exact: true })).toHaveCount(0);
  expect(calls.at(-1)?.body).toMatchObject({ state: "SUGGESTION_STATE_ACCEPTED" });
});

test("accepting a suggestion sends its id and the list refreshes", async ({ page }) => {
  await signIn(page);
  let open = [suggestion({ id: "s1" })];
  await mockRpc(page, ProfileService.method.listProfileSuggestions, () => ({ suggestions: open }));
  const accepts = await mockRpc(page, ProfileService.method.acceptProfileSuggestion, () => {
    open = [];
    return { suggestion: suggestion({ id: "s1", state: SuggestionState.ACCEPTED }) };
  });
  await page.goto("/profile");

  await page.getByRole("button", { name: "Accept", exact: true }).click();

  await expect(page.getByText("Nothing here.")).toBeVisible();
  expect(accepts[0].body).toMatchObject({ id: "s1" });
});

test("dismissing a suggestion sends its id and the list refreshes", async ({ page }) => {
  await signIn(page);
  let open = [suggestion({ id: "s1" }), suggestion({ id: "s2", after: "Another edit" })];
  await mockRpc(page, ProfileService.method.listProfileSuggestions, () => ({ suggestions: open }));
  const dismissals = await mockRpc(page, ProfileService.method.dismissProfileSuggestion, () => {
    open = open.filter((s) => s.id !== "s1");
    return { suggestion: suggestion({ id: "s1", state: SuggestionState.DISMISSED }) };
  });
  await page.goto("/profile");

  await page.getByRole("article").first().getByRole("button", { name: "Dismiss", exact: true }).click();

  await expect(page.getByRole("article")).toHaveCount(1);
  await expect(page.getByRole("article")).toContainText("Another edit");
  expect(dismissals[0].body).toMatchObject({ id: "s1" });
});

test("a suggestion decided elsewhere says so and the list reloads", async ({ page }) => {
  await signIn(page);
  let shown = [suggestion({ id: "s1" })];
  await mockRpc(page, ProfileService.method.listProfileSuggestions, () => ({ suggestions: shown }));
  await page.goto("/profile");
  await expect(page.getByRole("button", { name: "Accept", exact: true })).toBeVisible();
  shown = [];
  await mockRpcError(page, ProfileService.method.acceptProfileSuggestion, "failed_precondition", "no", "SUGGESTION_ALREADY_DECIDED");

  await page.getByRole("button", { name: "Accept", exact: true }).click();

  await expect(page.locator("p[role=alert]")).toContainText("already decided");
  await expect(page.getByText("Nothing here.")).toBeVisible();
});

test("syncing GitHub tells the owner whether anything was new", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.listProfileSuggestions, { suggestions: [] });
  let changed = true;
  const syncs = await mockRpc(page, ProfileService.method.syncGitHub, () => ({ changed }));
  await page.goto("/profile");

  await page.getByRole("button", { name: "Sync GitHub" }).click();
  await expect(page.getByRole("status")).toContainText("new activity");
  changed = false;
  await page.getByRole("button", { name: "Sync GitHub" }).click();

  await expect(page.getByRole("status")).toContainText("nothing new");
  expect(syncs).toHaveLength(2);
});

test("a sync that cannot run says what to fix", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.listProfileSuggestions, { suggestions: [] });
  await mockRpcError(page, ProfileService.method.syncGitHub, "failed_precondition", "x", "GITHUB_NOT_CONFIGURED");
  await page.goto("/profile");

  await page.getByRole("button", { name: "Sync GitHub" }).click();

  await expect(page.locator("p[role=alert]")).toContainText("GitHub is not connected");
});

test("only web links are shown as evidence", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.listProfileSuggestions, {
    suggestions: [suggestion({ id: "s1", evidence: [
      { label: "Fine", url: "https://github.com/o/r" },
      { label: "Sneaky", url: "javascript:alert(1)" },
    ] })],
  });

  await page.goto("/profile");

  await expect(page.getByRole("link", { name: "Fine" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Sneaky" })).toHaveCount(0);
});

test("a failed load offers a retry", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, ProfileService.method.listProfileSuggestions, "internal", "boom");

  await page.goto("/profile");

  await expect(page.getByText("Couldn't load your suggestions.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});

test("the nav has a Profile tab", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.listProfileSuggestions, { suggestions: [] });

  await page.goto("/profile");

  await expect(page.getByRole("link", { name: "Profile" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Your profile." })).toBeVisible();
});
