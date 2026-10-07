import { expect, test } from "@playwright/test";

// Wait for the whole chain: kagami writes job.added, fude writes the cover
// letter with the stubbed Claude and writes draft.ready, taiko stores the
// notification and streams it through torii.
const CHAIN_TIMEOUT_MS = 45_000;

test("adding a job leads to a cover letter ready notification, live and without a reload", async ({ page }) => {
  const title = `Notify Engineer ${Date.now()}`;

  // Arrange: sign in through the stub identity provider.
  await page.goto("/login");
  await page.getByRole("link", { name: "Sign in with Google" }).click();
  await expect(page).toHaveURL(/\/jobs$/);
  const bell = page.getByRole("link", { name: /^Notifications/ });
  const before = Number((await bell.getByTestId("unread-count").textContent({ timeout: 1000 }).catch(() => "0")) ?? 0);

  // Act: add a job, and stay on the page.
  await page.getByRole("button", { name: "Add job" }).click();
  const dialog = page.getByRole("dialog", { name: "Add job" });
  await dialog.getByLabel("Title").fill(title);
  await dialog.getByLabel("Company", { exact: true }).fill("Notify Test Co");
  await dialog.getByRole("button", { name: "Add job" }).click();

  // Assert: the bell counts one more without the page being reloaded.
  await expect(bell.getByTestId("unread-count")).toHaveText(String(before + 1), { timeout: CHAIN_TIMEOUT_MS });

  // Assert: the list names the cover letter, and opens the draft.
  await bell.click();
  await expect(page).toHaveURL(/\/notifications$/);
  const entry = page.getByRole("link", { name: /Cover letter ready/ }).first();
  await expect(entry).toBeVisible();
  await entry.click();
  await expect(page).toHaveURL(/\/drafts\/[0-9a-f-]{36}$/);

  // Assert: opening it marked it read, so the count is back to what it was.
  const count = bell.getByTestId("unread-count");
  if (before === 0) {
    await expect(count).toHaveCount(0, { timeout: 10_000 });
  } else {
    await expect(count).toHaveText(String(before), { timeout: 10_000 });
  }
});
