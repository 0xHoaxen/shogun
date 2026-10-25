import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { APPEARANCE_KEY } from "../src/lib/appearance";
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

// Puts a raw value in storage before the app loads.
async function stored(page: Page, raw: string) {
  await page.addInitScript(([key, value]) => window.localStorage.setItem(key, value), [APPEARANCE_KEY, raw]);
}

async function sidebarWidth(page: Page): Promise<number> {
  return (await page.locator("aside").boundingBox())!.width;
}

test("with nothing stored the defaults apply", async ({ page }) => {
  // Act
  await page.goto("/people");

  // Assert
  expect(await sidebarWidth(page)).toBe(200);
  await expect(page.getByRole("contentinfo", { name: "Utility" })).toContainText("Page scheme: Plum");
  expect(await page.evaluate(() => document.documentElement.dataset.density)).toBe("comfortable");
  expect(await page.evaluate(() => document.documentElement.dataset.motion)).toBe("on");
});

test("a collapsed sidebar is remembered across a reload", async ({ page }) => {
  // Arrange
  await page.goto("/people");
  await page.getByRole("button", { name: "Collapse sidebar" }).click();
  expect(await sidebarWidth(page)).toBe(60);

  // Act
  await page.reload();

  // Assert
  await expect(page.getByRole("button", { name: "Expand sidebar" })).toBeVisible();
  expect(await sidebarWidth(page)).toBe(60);
});

test("a stored scheme replaces a page's default, for that page only", async ({ page }) => {
  // Arrange
  await stored(page, JSON.stringify({ schemes: { "/people": "tide" } }));

  // Act
  await page.goto("/people");

  // Assert
  const footer = page.getByRole("contentinfo", { name: "Utility" });
  await expect(footer).toContainText("Page scheme: Tide");
  expect(await page.evaluate(() => document.documentElement.dataset.scheme)).toBe("tide");

  // Act
  await page.goto("/pipeline");

  // Assert
  await expect(footer).toContainText("Page scheme: Moss");
});

test("compact density tightens the row padding", async ({ page }) => {
  // Arrange
  await stored(page, JSON.stringify({ density: "compact" }));

  // Act
  await page.goto("/people");

  // Assert
  await expect(page.getByRole("navigation", { name: "Shogun" })).toBeVisible();
  expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--rowpad").trim())).toBe(
    "7px",
  );
});

test("motion off stops animation", async ({ page }) => {
  // Arrange
  await stored(page, JSON.stringify({ motion: "off" }));

  // Act
  await page.goto("/kit");

  // Assert
  const blinking = page.getByText("Matching columns").locator("i");
  expect(await blinking.evaluate((el) => getComputedStyle(el).animationName)).toBe("none");
});

test("motion on animates unless the system asks for reduced motion", async ({ page }) => {
  // Act
  await page.goto("/kit");

  // Assert
  const blinking = page.getByText("Matching columns").locator("i");
  expect(await blinking.evaluate((el) => getComputedStyle(el).animationName)).toBe("blink");

  // Act
  await page.emulateMedia({ reducedMotion: "reduce" });

  // Assert
  expect(await blinking.evaluate((el) => getComputedStyle(el).animationName)).toBe("none");
});

test("damaged or unknown stored values fall back to the defaults", async ({ page }) => {
  for (const raw of [
    "{not json",
    "null",
    "[]",
    JSON.stringify({ density: "huge", motion: 3, sidebarCollapsed: "yes", schemes: { "/people": "neon" } }),
  ]) {
    // Arrange
    const fresh = await page.context().newPage();
    await mockRpc(fresh, AuthService.method.getSession, {
      session: { email: "o@example.com", displayName: "O", role: "member", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
    });
    await stored(fresh, raw);

    // Act
    await fresh.goto("/people");

    // Assert
    expect(await sidebarWidth(fresh)).toBe(200);
    await expect(fresh.getByRole("contentinfo", { name: "Utility" })).toContainText("Page scheme: Plum");
    await fresh.close();
  }
});

test("blocked storage still works for the visit", async ({ page }) => {
  // Arrange
  await page.addInitScript(() => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    Storage.prototype.getItem = blocked;
    Storage.prototype.setItem = blocked;
  });

  // Act
  await page.goto("/people");
  await page.getByRole("button", { name: "Collapse sidebar" }).click();

  // Assert
  expect(await sidebarWidth(page)).toBe(60);
});
