import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { mockRpc, mockRpcError } from "./fixtures";

async function signIn(page: Page, timezone = "Asia/Kolkata") {
  await mockRpc(page, AuthService.method.getSession, {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      timezone,
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  });
}

test("the Account tab shows the current time zone and saves a new one", async ({ page }) => {
  await signIn(page);
  const saves = await mockRpc(page, AuthService.method.setTimezone, { timezone: "Europe/Berlin" });
  await page.goto("/settings/account");
  const zone = page.getByLabel("Time zone");
  await expect(zone).toHaveValue("Asia/Kolkata");
  await expect(page.getByRole("button", { name: "Save time zone" })).toBeDisabled();

  await zone.selectOption("Europe/Berlin");
  await page.getByRole("button", { name: "Save time zone" }).click();

  await expect(page.getByRole("status")).toContainText("Time zone saved");
  expect(saves).toHaveLength(1);
  expect(saves[0].body).toMatchObject({ timezone: "Europe/Berlin" });
});

test("a time zone the server refuses is explained", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, AuthService.method.setTimezone, "invalid_argument", "bad zone", "INVALID_TIMEZONE");
  await page.goto("/settings/account");

  await page.getByLabel("Time zone").selectOption("Europe/Berlin");
  await page.getByRole("button", { name: "Save time zone" }).click();

  await expect(page.locator('p[role="alert"]')).toContainText("not a time zone Shogun knows");
});

test("signing out everywhere ends the sessions and returns to the login page", async ({ page }) => {
  await signIn(page);
  const calls = await mockRpc(page, AuthService.method.logoutEverywhere, { ended: BigInt(3) });
  await page.goto("/settings/account");

  await page.getByRole("button", { name: "Sign out everywhere" }).click();

  await expect(page).toHaveURL(/\/login/);
  expect(calls).toHaveLength(1);
});

test("a failed sign out everywhere says so and stays put", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, AuthService.method.logoutEverywhere, "internal", "boom", "INTERNAL");
  await page.goto("/settings/account");

  await page.getByRole("button", { name: "Sign out everywhere" }).click();

  await expect(page.locator('p[role="alert"]')).toContainText("Couldn't sign you out everywhere");
  await expect(page).toHaveURL(/\/settings\/account$/);
});
