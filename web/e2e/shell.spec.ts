import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { DraftsService } from "../src/gen/shogun/api/v1/drafts_pb";
import { mockRpc } from "./fixtures";

const SECTION_LINKS = [
  "01Dashboard",
  "02Inbox",
  "03Pipeline",
  "04People",
  "05Writing",
  "06Growth",
  "07Insights",
  "08Settings",
];

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

test("the sidebar lists the eight sections in order and marks the current one", async ({ page }) => {
  // Act
  await page.goto("/people");

  // Assert
  const nav = page.getByRole("navigation", { name: "Shogun" });
  await expect(nav.getByRole("link")).toHaveText(SECTION_LINKS);
  await expect(nav.getByRole("link", { name: "People" })).toHaveAttribute("aria-current", "page");
});

test("the footer names the page scheme and keeps the approval promise", async ({ page }) => {
  for (const [path, scheme] of [
    ["/pipeline", "Moss"],
    ["/people", "Plum"],
    ["/inbox", "Tide"],
  ]) {
    // Act
    await page.goto(path);

    // Assert
    const footer = page.getByRole("contentinfo", { name: "Utility" });
    await expect(footer).toContainText(`Page scheme: ${scheme}`);
    await expect(footer).toContainText("Nothing leaves without your approval");
  }
});

test("Inbox shows how many drafts wait, and the Next strip says so", async ({ page }) => {
  // Arrange
  await mockRpc(page, DraftsService.method.listQueue, { drafts: [{ id: "d1" }, { id: "d2" }, { id: "d3" }] });

  // Act
  await page.goto("/insights");

  // Assert
  await expect(page.getByTestId("inbox-count")).toHaveText("3 waiting3");
  await expect(page.getByTestId("next-strip")).toContainText("3 drafts are waiting for your approval");
});

test("with nothing waiting there is no chip and the Next strip says so", async ({ page }) => {
  // Arrange
  await mockRpc(page, DraftsService.method.listQueue, { drafts: [] });

  // Act
  await page.goto("/insights");

  // Assert
  await expect(page.getByTestId("inbox-count")).toHaveCount(0);
  await expect(page.getByTestId("next-strip")).toContainText("Nothing is waiting for you");
});

test("the sidebar collapses to 60px and expands again", async ({ page }) => {
  // Arrange
  await page.goto("/insights");
  const sidebar = page.locator("aside");
  expect((await sidebar.boundingBox())!.width).toBe(200);

  // Act
  await page.getByRole("button", { name: "Collapse sidebar" }).click();

  // Assert
  expect((await sidebar.boundingBox())!.width).toBe(60);
  await expect(page.getByRole("navigation", { name: "Shogun" }).getByRole("link", { name: "Pipeline" })).toBeVisible();

  // Act
  await page.getByRole("button", { name: "Expand sidebar" }).click();

  // Assert
  expect((await sidebar.boundingBox())!.width).toBe(200);
});

test("below 860px the sidebar becomes a menu across the top", async ({ page }) => {
  // Arrange
  await page.setViewportSize({ width: 390, height: 800 });

  // Act
  await page.goto("/insights");

  // Assert
  const dashboard = await page.getByRole("link", { name: "Dashboard" }).boundingBox();
  const settings = await page.getByRole("link", { name: "Settings" }).boundingBox();
  expect(dashboard!.y).toBeLessThan(200);
  expect(settings!.x + settings!.width).toBeLessThanOrEqual(390);
  await expect(page.getByRole("button", { name: "Collapse sidebar" })).toBeHidden();
});

test("old addresses land on their v2 route", async ({ page }) => {
  for (const [from, to] of [
    ["/jobs", /\/pipeline$/],
    ["/contacts", /\/people$/],
    ["/drafts", /\/inbox$/],
    ["/drafts/d1", /\/writing\?draft=d1$/],
    ["/learning", /\/growth$/],
    ["/profile", /\/growth$/],
    ["/notifications", /\/inbox$/],
    ["/discovery", /\/settings\/discovery$/],
    ["/settings?tab=discovery", /\/settings\/discovery$/],
  ] as const) {
    // Act
    await page.goto(from);

    // Assert
    await expect(page).toHaveURL(to);
  }
});

test("labels use the muted text colour, not the surface colour", async ({ page }) => {
  // Arrange
  const MUTED = "rgb(171, 166, 148)";

  // Act
  await page.goto("/insights");

  // Assert
  await expect(page.getByTestId("next-strip").getByText("Next")).toHaveCSS("color", MUTED);
  await expect(page.getByRole("contentinfo", { name: "Utility" }).getByText(/^Page scheme/)).toHaveCSS("color", MUTED);
});
