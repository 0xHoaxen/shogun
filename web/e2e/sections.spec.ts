import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { SECTIONS } from "../src/content/sections";
import { mockRpc } from "./fixtures";

// Every section of the app, with the route that shows it.
const ROUTES: Record<keyof typeof SECTIONS, string> = {
  jobs: "/jobs",
  contacts: "/contacts",
  drafts: "/drafts",
  learning: "/learning",
  profile: "/profile",
  discovery: "/discovery",
  insights: "/insights",
  notifications: "/notifications",
  settings: "/settings",
  provider: "/settings/provider",
  mail: "/settings/mail",
  github: "/settings/github",
  spend: "/settings/spend",
  alerts: "/settings/notifications",
  users: "/settings/users",
};

test.beforeEach(async ({ page }) => {
  await mockRpc(page, AuthService.method.getSession, {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      role: "admin",
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  });
});

for (const [key, route] of Object.entries(ROUTES)) {
  test(`${key} explains its responsibility, what it does and how it helps`, async ({ page }) => {
    // Act
    await page.goto(route);

    // Assert
    const about = page.getByTestId(`about-${key}`);
    await expect(about).toBeVisible();
    await expect(about.getByText("Responsibility")).toBeVisible();
    await expect(about.getByText("What it does")).toBeVisible();
    await expect(about.getByText("How it helps you")).toBeVisible();
    const copy = SECTIONS[key as keyof typeof SECTIONS];
    await expect(about).toContainText(copy.role);
    await expect(about).toContainText(copy.helps);
  });
}

test("every section has copy and a route", () => {
  expect(Object.keys(ROUTES).sort()).toEqual(Object.keys(SECTIONS).sort());
});

test("a closed explainer stays closed after a reload", async ({ page }) => {
  // Arrange
  await page.goto("/jobs");
  const about = page.getByTestId("about-jobs");
  await expect(about).toHaveJSProperty("open", true);

  // Act
  await about.getByText("About this section").click();
  await expect(about).toHaveJSProperty("open", false);
  await page.reload();

  // Assert
  await expect(page.getByTestId("about-jobs")).toHaveJSProperty("open", false);
  await page.goto("/contacts");
  await expect(page.getByTestId("about-contacts")).toHaveJSProperty("open", true);
});

test("reopening an explainer forgets that it was closed", async ({ page }) => {
  // Arrange
  await page.goto("/jobs");
  const summary = page.getByTestId("about-jobs").getByText("About this section");
  await summary.click();
  await summary.click();

  // Act
  await page.reload();

  // Assert
  await expect(page.getByTestId("about-jobs")).toHaveJSProperty("open", true);
});
