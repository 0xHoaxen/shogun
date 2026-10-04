import { expect, test } from "@playwright/test";

import { moveCardRight } from "../e2e/drag";

const OWNER_EMAIL = process.env.TORII_E2E_EMAIL ?? "owner@example.com";

test("log in, add a job, move it and keep it across a reload", async ({ page }) => {
  // A fresh title each run, so a leftover database cannot satisfy the checks.
  const title = `E2E Engineer ${Date.now()}`;
  const card = (name: string) => page.getByRole("region", { name, exact: true }).getByText(title);

  // Act: open a page while signed out; torii's Google flow runs against the stub.
  await page.goto("/jobs");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByRole("link", { name: "Sign in with Google" }).click();

  // Assert: back on the board as the allowed owner.
  await expect(page).toHaveURL(/\/jobs$/);
  await expect(page.getByTestId("owner-email")).toHaveText(OWNER_EMAIL);

  // Act: add a job.
  await page.getByRole("button", { name: "Add job" }).click();
  const dialog = page.getByRole("dialog", { name: "Add job" });
  await dialog.getByLabel("Title").fill(title);
  await dialog.getByLabel("Company", { exact: true }).fill("Stack Test Co");
  await dialog.getByRole("button", { name: "Add job" }).click();

  // Assert: it is listed in Saved.
  await expect(card("Saved")).toBeVisible();

  // Act: move it to Applied.
  await moveCardRight(page, new RegExp(title));

  // Assert: it moved, and the move was stored by kagami rather than held in the page.
  await expect(card("Applied")).toBeVisible();
  await page.reload();
  await expect(card("Applied")).toBeVisible();
  await expect(card("Saved")).toBeHidden();

  // Act: sign out.
  await page.getByRole("button", { name: "Logout" }).click();

  // Assert: the session is gone, so the board is no longer reachable.
  await expect(page).toHaveURL(/\/login$/);
  await page.goto("/jobs");
  await expect(page).toHaveURL(/\/login$/);
});
