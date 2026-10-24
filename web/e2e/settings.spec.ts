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
  await expect(tabs.getByRole("link")).toHaveText(["AI provider", "Mail", "GitHub", "Spend", "Alerts", "Account"]);
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
