import { expect, test } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { mockRpcError } from "./fixtures";

test("the privacy policy is readable while signed out", async ({ page }) => {
  // Arrange
  await mockRpcError(page, AuthService.method.getSession, "unauthenticated", "no session");

  // Act
  await page.goto("/privacy");

  // Assert
  await expect(page).toHaveURL(/\/privacy$/);
  await expect(page.getByRole("heading", { level: 1, name: "Privacy Policy" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Google API Services User Data Policy" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Google API Services User Data Policy" })).toHaveAttribute(
    "href",
    "https://developers.google.com/terms/api-services-user-data-policy",
  );
});

test("the terms of service are readable while signed out", async ({ page }) => {
  // Arrange
  await mockRpcError(page, AuthService.method.getSession, "unauthenticated", "no session");

  // Act
  await page.goto("/terms");

  // Assert
  await expect(page).toHaveURL(/\/terms$/);
  await expect(page.getByRole("heading", { level: 1, name: "Terms of Service" })).toBeVisible();
});

test("the login page links to the privacy policy and terms", async ({ page }) => {
  // Act
  await page.goto("/login");

  // Assert
  const legal = page.getByRole("navigation", { name: "Legal" });
  await expect(legal.getByRole("link", { name: "Privacy Policy" })).toHaveAttribute("href", "/privacy");
  await expect(legal.getByRole("link", { name: "Terms of Service" })).toHaveAttribute("href", "/terms");
});
