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

test("the Settings button sits at the bottom left of the footer on every page", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));

  for (const path of ["/jobs", "/contacts", "/insights"]) {
    // Act
    await page.goto(path);

    // Assert
    const footer = page.getByRole("contentinfo", { name: "Utility" });
    const settings = footer.getByRole("link", { name: "Settings" });
    await expect(settings).toBeVisible();
    const footerBox = await footer.boundingBox();
    const settingsBox = await settings.boundingBox();
    const emailBox = await footer.getByTestId("owner-email").boundingBox();
    expect(settingsBox!.x).toBeLessThan(emailBox!.x);
    expect(settingsBox!.x - footerBox!.x).toBeLessThan(40);
  }
});

test("the Settings button opens the hub with the tabs a member may use", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));
  await page.goto("/jobs");

  // Act
  await page.getByRole("contentinfo", { name: "Utility" }).getByRole("link", { name: "Settings" }).click();

  // Assert
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.getByRole("heading", { name: "Your keys and connections." })).toBeVisible();
  const tabs = page.getByRole("navigation", { name: "Settings" });
  await expect(tabs.getByRole("link")).toHaveText(["AI provider", "Mail", "GitHub", "Spend", "Alerts"]);
  await expect(page.getByRole("contentinfo", { name: "Utility" }).getByRole("link", { name: "Settings" })).toHaveAttribute(
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
    "Spend",
    "Alerts",
    "Users",
  ]);
});

test("Mail, Spend and Alerts are no longer top-level tabs", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));

  // Act
  await page.goto("/jobs");

  // Assert
  const top = page.getByRole("navigation", { name: "Shogun" });
  for (const name of ["Mail", "Spend", "Alerts", "Users"]) {
    await expect(top.getByRole("link", { name })).toHaveCount(0);
  }
});
