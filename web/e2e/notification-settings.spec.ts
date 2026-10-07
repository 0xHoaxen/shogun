import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { NotificationsService } from "../src/gen/shogun/api/v1/notifications_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const MINUTES_PER_HOUR = 60;

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  });
}

test("a new owner sees the defaults: in-app on and no quiet hours", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, NotificationsService.method.getNotificationSettings, {
    settings: { inAppEnabled: true, version: BigInt(0) },
  });

  // Act
  await page.goto("/settings/notifications");

  // Assert
  await expect(page.getByRole("link", { name: "Alerts" })).toHaveAttribute("aria-current", "page");
  const form = page.getByRole("form", { name: "How Shogun reaches you" });
  await expect(form.getByLabel("In-app notifications and the daily digest")).toBeChecked();
  await expect(form.getByLabel("Quiet hours")).not.toBeChecked();
  await expect(form.getByLabel("Quiet from")).toHaveCount(0);
});

test("saving a quiet window sends it in minutes with the version it was loaded at", async ({ page }) => {
  // Arrange: the second read, after the save, returns what was saved.
  await signIn(page);
  let stored = { inAppEnabled: true, version: BigInt(0), quiet: undefined as undefined | { fromMinute: number; toMinute: number } };
  await mockRpc(page, NotificationsService.method.getNotificationSettings, () => ({ settings: stored }));
  const saves = await mockRpc(page, NotificationsService.method.saveNotificationSettings, (body) => {
    const sent = (body as { settings: { quiet?: { fromMinute: number; toMinute: number } } }).settings;
    stored = { inAppEnabled: true, version: BigInt(1), quiet: sent.quiet };
    return { settings: stored };
  });
  await page.goto("/settings/notifications");
  const form = page.getByRole("form", { name: "How Shogun reaches you" });

  // Act
  await form.getByLabel("Quiet hours").check();
  await form.getByLabel("Quiet from").fill("23:30");
  await form.getByLabel("Quiet until").fill("07:15");
  await form.getByRole("button", { name: "Save" }).click();

  // Assert
  await expect(form.getByRole("status")).toHaveText("Saved.");
  expect(saves).toHaveLength(1);
  expect(saves[0].body).toMatchObject({
    settings: {
      inAppEnabled: true,
      quiet: { fromMinute: 23 * MINUTES_PER_HOUR + 30, toMinute: 7 * MINUTES_PER_HOUR + 15 },
    },
  });
  // Version 0, the owner never saved, is left out of the JSON as proto3 does.
  expect((saves[0].body as { settings: { version?: string } }).settings.version).toBeUndefined();
  await expect(form.getByLabel("Quiet from")).toHaveValue("23:30");
});

test("a saved window is shown again on the next visit, and can be switched off", async ({ page }) => {
  // Arrange
  await signIn(page);
  let stored = {
    inAppEnabled: true,
    version: BigInt(3),
    quiet: undefined as undefined | { fromMinute: number; toMinute: number },
  };
  stored = { ...stored, quiet: { fromMinute: 22 * MINUTES_PER_HOUR, toMinute: 8 * MINUTES_PER_HOUR } };
  await mockRpc(page, NotificationsService.method.getNotificationSettings, () => ({ settings: stored }));
  const saves = await mockRpc(page, NotificationsService.method.saveNotificationSettings, (body) => {
    stored = { inAppEnabled: true, version: BigInt(4), quiet: undefined };
    return { settings: { ...stored, quiet: (body as { settings: { quiet?: undefined } }).settings.quiet } };
  });
  await page.goto("/settings/notifications");
  const form = page.getByRole("form", { name: "How Shogun reaches you" });
  await expect(form.getByLabel("Quiet from")).toHaveValue("22:00");
  await expect(form.getByLabel("Quiet until")).toHaveValue("08:00");

  // Act
  await form.getByLabel("Quiet hours").uncheck();
  await form.getByRole("button", { name: "Save" }).click();

  // Assert
  await expect(form.getByRole("status")).toHaveText("Saved.");
  expect(saves[0].body).toMatchObject({ settings: { version: "3" } });
  expect((saves[0].body as { settings: { quiet?: unknown } }).settings.quiet).toBeUndefined();
  await expect(form.getByLabel("Quiet hours")).not.toBeChecked();
});

test("a window with the same start and end cannot be saved", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, NotificationsService.method.getNotificationSettings, {
    settings: { inAppEnabled: true, version: BigInt(0) },
  });
  const saves = await mockRpc(page, NotificationsService.method.saveNotificationSettings, {
    settings: { inAppEnabled: true, version: BigInt(1) },
  });
  await page.goto("/settings/notifications");
  const form = page.getByRole("form", { name: "How Shogun reaches you" });

  // Act
  await form.getByLabel("Quiet hours").check();
  await form.getByLabel("Quiet until").fill("22:00");

  // Assert
  await expect(form.getByRole("alert")).toHaveText("The start and the end must be different times.");
  await expect(form.getByRole("button", { name: "Save" })).toBeDisabled();
  expect(saves).toHaveLength(0);
});

test("a save refused as stale reloads the settings and says they changed elsewhere", async ({ page }) => {
  // Arrange: another tab already saved version 5 with in-app switched off.
  await signIn(page);
  let reads = 0;
  await mockRpc(page, NotificationsService.method.getNotificationSettings, () => {
    reads += 1;
    return reads === 1
      ? { settings: { inAppEnabled: true, version: BigInt(4) } }
      : { settings: { inAppEnabled: false, version: BigInt(5) } };
  });
  await mockRpcError(
    page,
    NotificationsService.method.saveNotificationSettings,
    "aborted",
    "settings changed elsewhere",
    "VERSION_CONFLICT",
  );
  await page.goto("/settings/notifications");
  const form = page.getByRole("form", { name: "How Shogun reaches you" });
  await expect(form.getByLabel("In-app notifications and the daily digest")).toBeChecked();

  // Act
  await form.getByLabel("Quiet hours").check();
  await form.getByRole("button", { name: "Save" }).click();

  // Assert
  await expect(form.getByRole("alert")).toContainText("changed elsewhere");
  await expect(form.getByLabel("In-app notifications and the daily digest")).not.toBeChecked();
  await expect(form.getByLabel("Quiet hours")).not.toBeChecked();
});

test("a failed load offers a retry", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpcError(page, NotificationsService.method.getNotificationSettings, "internal", "boom");

  // Act
  await page.goto("/settings/notifications");

  // Assert
  await expect(page.getByText("Couldn't load your settings.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});
