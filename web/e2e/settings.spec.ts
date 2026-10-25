import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { mockRpc } from "./fixtures";

function session(role: string) {
  return {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      role,
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  };
}

test("Settings is the last numbered row of the sidebar on every page", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));

  for (const path of ["/pipeline", "/people", "/insights"]) {
    // Act
    await page.goto(path);

    // Assert
    const links = page.getByRole("navigation", { name: "Shogun" }).getByRole("link");
    await expect(links.last()).toHaveText("08Settings");
  }
});

test("the Settings link opens the hub with the tabs a member may use", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));
  await page.goto("/pipeline");

  // Act
  await page.getByRole("navigation", { name: "Shogun" }).getByRole("link", { name: "Settings" }).click();

  // Assert
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.getByRole("heading", { name: "Your keys and connections." })).toBeVisible();
  const tabs = page.getByRole("navigation", { name: "Settings" });
  await expect(tabs.getByRole("link")).toHaveText([
    "AI provider",
    "Mail",
    "GitHub",
    "Discovery",
    "Spend",
    "Alerts",
    "Account",
  ]);
  await expect(page.getByRole("navigation", { name: "Shogun" }).getByRole("link", { name: "Settings" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("an admin also gets the Users tab", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("admin"));

  // Act
  await page.goto("/settings");

  // Assert
  await expect(page.getByRole("navigation", { name: "Settings" }).getByRole("link")).toHaveText([
    "AI provider",
    "Mail",
    "GitHub",
    "Discovery",
    "Spend",
    "Alerts",
    "Account",
    "Users",
  ]);
});

test("Mail, Spend and Alerts are no longer top-level tabs", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));

  // Act
  await page.goto("/pipeline");

  // Assert
  const top = page.getByRole("navigation", { name: "Shogun" });
  for (const name of ["Mail", "Spend", "Alerts", "Users"]) {
    await expect(top.getByRole("link", { name })).toHaveCount(0);
  }
});

test("Discovery is a Settings tab of its own and the nav marks it on its page", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));

  // Act
  await page.goto("/settings/discovery");

  // Assert
  await expect(page.getByRole("navigation", { name: "Settings" }).getByRole("link", { name: "Discovery" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(page.getByRole("region", { name: "Discovery sources" })).toHaveCount(0);
  await page.getByRole("button", { name: "Sources" }).click();
  await expect(page.getByRole("region", { name: "Discovery sources" })).toBeVisible();
});

test("the four Settings screens are built from cards, not the old ruled look", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("admin"));

  for (const [path, card] of [
    ["/settings/discovery", "Postings"],
    ["/settings/spend", "Spend"],
    ["/settings/users", "Users"],
    ["/settings/github", "GitHub"],
  ] as const) {
    // Act
    await page.goto(path);

    // Assert
    const region = page.getByRole("region", { name: card, exact: true });
    await expect(region).toBeVisible();
    await expect(region).toHaveCSS("border-top-style", "dashed");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  }
});

test("on a phone no Settings screen scrolls sideways", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("admin"));
  await page.setViewportSize({ width: 390, height: 900 });

  for (const path of ["/settings/discovery", "/settings/spend", "/settings/users", "/settings/github", "/settings/provider"]) {
    // Act
    await page.goto(path);
    await expect(page.getByRole("navigation", { name: "Settings" })).toBeVisible();

    // Assert
    const widths = await page.evaluate(() => ({ page: document.documentElement.scrollWidth, window: window.innerWidth }));
    expect(widths.page, path).toBeLessThanOrEqual(widths.window);
  }
});

test("a dialog darkens the page behind it", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));
  await page.goto("/settings/discovery");
  await page.getByRole("button", { name: "Sources" }).click();

  // Act
  await page.getByRole("button", { name: "Add source" }).click();

  // Assert: the scrim is the dark page colour at partial opacity, never a light wash.
  const scrim = page.locator("[data-base-ui-inert], div.fixed.inset-0").first();
  const [r, g, b] = (await scrim.evaluate((el) => getComputedStyle(el).backgroundColor))
    .match(/[\d.]+/g)!
    .slice(0, 3)
    .map(Number);
  expect(r + g + b).toBeLessThan(150);
});
