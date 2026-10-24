import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { mockRpc } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await mockRpc(page, AuthService.method.getSession, {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      role: "member",
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  });
});

test("the side panel opens, is named by its eyebrow, closes on Esc and returns focus", async ({ page }) => {
  // Arrange
  await page.goto("/kit");
  const opener = page.getByRole("button", { name: "Open panel" });

  // Act
  await opener.click();

  // Assert
  const panel = page.getByRole("dialog", { name: "Bulk import" });
  await expect(panel).toBeVisible();

  // Act
  await page.keyboard.press("Escape");

  // Assert
  await expect(panel).toHaveCount(0);
  await expect(opener).toBeFocused();
});

test("the side panel closes with its × button and keeps Tab inside", async ({ page }) => {
  // Arrange
  await page.goto("/kit");
  await page.getByRole("button", { name: "Open panel" }).click();
  const panel = page.getByRole("dialog", { name: "Bulk import" });

  // Act
  for (let i = 0; i < 6; i++) await page.keyboard.press("Tab");

  // Assert
  expect(await panel.evaluate((el) => el.contains(document.activeElement))).toBe(true);

  // Act
  await panel.getByRole("button", { name: "Close" }).click();

  // Assert
  await expect(panel).toHaveCount(0);
});

test("the side panel takes the page's accent scheme", async ({ page }) => {
  // Arrange
  await page.goto("/kit");

  // Act
  await page.getByRole("button", { name: "Open panel" }).click();

  // Assert: /kit is outside every section, so the default Ember accent applies.
  await expect(page.getByRole("dialog", { name: "Bulk import" }).getByRole("button", { name: "Close" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.dataset.scheme)).toBe("ember");
});

test("a toast with Undo says what happened, runs Undo once and goes away", async ({ page }) => {
  // Arrange
  await page.goto("/kit");

  // Act
  await page.getByRole("button", { name: "Show toast" }).click();

  // Assert
  const toast = page.getByRole("status");
  await expect(toast).toContainText("5 people imported");

  // Act
  await toast.getByRole("button", { name: "Undo" }).click();

  // Assert
  await expect(page.getByText("Undone")).toBeVisible();
  await expect(toast.getByRole("button", { name: "Undo" })).toHaveCount(0);
});

test("a toast leaves by itself after 10 seconds", async ({ page }) => {
  // Arrange
  await page.clock.install();
  await page.goto("/kit");
  await page.getByRole("button", { name: "Show toast" }).click();
  await expect(page.getByRole("status")).toContainText("5 people imported");

  // Act
  await page.clock.runFor(9_000);

  // Assert
  await expect(page.getByRole("status")).toContainText("5 people imported");

  // Act
  await page.clock.runFor(1_500);

  // Assert
  await expect(page.getByText("5 people imported")).toHaveCount(0);
});

test("filter chips show counts and mark the chosen one with aria-pressed", async ({ page }) => {
  // Arrange
  await page.goto("/kit");
  const group = page.getByRole("group", { name: "Relationship" });

  // Assert
  await expect(group.getByRole("button", { name: "All 12" })).toHaveAttribute("aria-pressed", "true");

  // Act
  await group.getByRole("button", { name: "Recruiter 5" }).click();

  // Assert
  await expect(group.getByRole("button", { name: "Recruiter 5" })).toHaveAttribute("aria-pressed", "true");
  await expect(group.getByRole("button", { name: "All 12" })).toHaveAttribute("aria-pressed", "false");
});

test("the segmented control picks one option", async ({ page }) => {
  // Arrange
  await page.goto("/kit");
  const group = page.getByRole("group", { name: "Density" });

  // Act
  await group.getByRole("button", { name: "Compact" }).click();

  // Assert
  await expect(group.getByRole("button", { name: "Compact" })).toHaveAttribute("aria-pressed", "true");
  await expect(group.getByRole("button", { name: "Comfortable" })).toHaveAttribute("aria-pressed", "false");
});

test("the toggle is a switch with its hint, and flips with click and Space", async ({ page }) => {
  // Arrange
  await page.goto("/kit");
  const toggle = page.getByRole("switch", { name: /Undo window/ });
  await expect(toggle).toContainText("Hold sends for 10 seconds.");
  await expect(toggle).toHaveAttribute("aria-checked", "false");

  // Act
  await toggle.click();

  // Assert
  await expect(toggle).toHaveAttribute("aria-checked", "true");

  // Act
  await toggle.focus();
  await page.keyboard.press("Space");

  // Assert
  await expect(toggle).toHaveAttribute("aria-checked", "false");
});

test("the step header marks the current step and the progress lines say where they are", async ({ page }) => {
  // Arrange
  await page.goto("/kit");

  // Assert
  const steps = page.getByRole("list").filter({ hasText: "Choose file" });
  await expect(steps.getByRole("listitem")).toHaveText(["1 Choose file (done)", "2 AI reads it", "3 Check and import"]);
  await expect(steps.getByRole("listitem").nth(1)).toHaveAttribute("aria-current", "step");
  const lines = page.getByRole("list").filter({ hasText: "Matching columns" });
  await expect(lines.getByRole("listitem")).toHaveText([
    "Reading the file (done)",
    "Matching columns (in progress)",
    "Checking for duplicates (waiting)",
  ]);
});

test("tags carry their tone", async ({ page }) => {
  // Arrange
  await page.goto("/kit");

  // Assert
  await expect(page.getByText("Check", { exact: true })).toHaveCSS("color", "rgb(216, 179, 90)");
  await expect(page.getByText("Default", { exact: true })).toHaveCSS("color", "rgb(236, 230, 211)");
});
