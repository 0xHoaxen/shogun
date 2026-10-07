import { expect, test, type Page } from "@playwright/test";

// The chain under test: dojo writes learning.activity_added, fude consumes it
// and writes a post with the stubbed Claude, and the draft reaches the queue.
const CHAIN_TIMEOUT_MS = 45_000;

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByRole("link", { name: "Sign in with Google" }).click();
  await expect(page).toHaveURL(/\/jobs$/);
}

// pendingPosts counts the LinkedIn posts waiting for approval in the queue.
async function pendingPosts(page: Page): Promise<number> {
  await page.goto("/drafts");
  const rows = page.getByRole("row", { name: /Post.*LinkedIn.*Needs you/ });
  await expect(page.getByRole("button", { name: "Needs you" })).toBeVisible();
  // The rows load after the filter; wait for the table to settle.
  await expect(page.getByRole("table")).toBeVisible();
  return rows.count();
}

// waitForPosts waits until n more posts than before are pending. A test that
// ends with a draft still being written would leak its draft.ready notification
// into the next spec's unread count.
async function waitForPosts(page: Page, before: number, n: number) {
  await expect(async () => {
    expect(await pendingPosts(page)).toBeGreaterThanOrEqual(before + n);
  }).toPass({ timeout: CHAIN_TIMEOUT_MS });
}

test("logging an activity leads to a pending post draft in the queue", async ({ page }) => {
  const summary = `Learned about worker pools ${Date.now()}`;
  await signIn(page);
  const before = await pendingPosts(page);

  await page.goto("/learning");
  await page.getByRole("button", { name: "Log activity" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Log activity" });
  await dialog.getByLabel("What did you learn?").fill(summary);
  await dialog.getByLabel("Minutes").fill("30");
  await dialog.getByRole("button", { name: "Log activity" }).click();
  await expect(page.getByRole("row", { name: new RegExp(summary) })).toBeVisible();

  await waitForPosts(page, before, 1);
});

test("finishing an item is moved by dojo, shown as done and drafts a post", async ({ page }) => {
  const title = `Course ${Date.now()}`;
  await signIn(page);
  const before = await pendingPosts(page);
  await page.goto("/learning");

  await page.getByRole("button", { name: "Add item" }).click();
  const dialog = page.getByRole("dialog", { name: "Add item" });
  await dialog.getByLabel("Title").fill(title);
  await dialog.getByRole("button", { name: "Add item" }).click();
  const row = page.getByRole("row", { name: new RegExp(`^${title}`) });
  await expect(row).toContainText("Planned");
  await page.getByRole("button", { name: `Start ${title}` }).click();
  await expect(row).toContainText("In progress");
  await page.getByRole("button", { name: `Finish ${title}` }).click();

  await expect(row).toContainText("Done");
  await expect(page.getByRole("button", { name: `Reopen ${title}` })).toBeVisible();
  // Finishing an item drafts a post about it.
  await waitForPosts(page, before, 1);
});

test("choosing an activity and writing a post opens its draft", async ({ page }) => {
  const summary = `Read the race detector docs ${Date.now()}`;
  await signIn(page);
  const before = await pendingPosts(page);
  await page.goto("/learning");
  await page.getByRole("button", { name: "Log activity" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Log activity" });
  await dialog.getByLabel("What did you learn?").fill(summary);
  await dialog.getByRole("button", { name: "Log activity" }).click();
  await expect(page.getByRole("row", { name: new RegExp(summary) })).toBeVisible();

  await page.getByRole("checkbox", { name: `Choose: ${summary}` }).check();
  await page.getByRole("button", { name: /^Write a post/ }).click();
  await page.getByRole("button", { name: "Write the post" }).click();

  await expect(page).toHaveURL(/\/drafts\/[0-9a-f-]{36}$/, { timeout: CHAIN_TIMEOUT_MS });
  // Logging the activity drafted one post and asking for a post drafted another.
  await waitForPosts(page, before, 2);
});
