import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const OWNER_EMAIL = "owner@example.com";

const SIGNED_IN = {
  session: {
    email: OWNER_EMAIL,
    displayName: "Owner",
    expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
  },
};

test("a signed-out visit to a page lands on the login page", async ({ page }) => {
  // Arrange
  await mockRpcError(page, AuthService.method.getSession, "unauthenticated", "no session");

  // Act
  await page.goto("/jobs");

  // Assert
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "Nothing leaves without your seal." })).toBeVisible();
});

test("the login page starts Google sign-in on torii", async ({ page }) => {
  // Act
  await page.goto("/login");

  // Assert
  await expect(page.getByRole("link", { name: "Sign in with Google" })).toHaveAttribute(
    "href",
    "/auth/login",
  );
});

test("the login page explains why a sign-in was refused", async ({ page }) => {
  // Act
  await page.goto("/login?error=not_allowed");

  // Assert
  await expect(page.getByRole("main").getByRole("alert")).toHaveText(
    "This Google account is not allowed to sign in.",
  );
});

test("the login page has a fallback for an unknown error code", async ({ page }) => {
  // Act
  await page.goto("/login?error=something_new");

  // Assert
  await expect(page.getByRole("main").getByRole("alert")).toHaveText("Sign-in failed. Try again.");
});

test("a signed-in visit shows the shell with the owner and the page tabs", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, SIGNED_IN);

  // Act
  await page.goto("/jobs");

  // Assert
  await expect(page.getByTestId("owner-email")).toHaveText(OWNER_EMAIL);
  await expect(page.getByRole("link", { name: "Jobs" })).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("link", { name: "Contacts" })).not.toHaveAttribute("aria-current");
});

test("the root sends a signed-in owner to the jobs page", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, SIGNED_IN);

  // Act
  await page.goto("/");

  // Assert
  await expect(page).toHaveURL(/\/jobs$/);
});

test("logout ends the session and returns to the login page", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, SIGNED_IN);
  const logoutCalls = await mockRpc(page, AuthService.method.logout, {});
  await page.goto("/contacts");

  // Act
  await page.getByRole("button", { name: "Logout" }).click();

  // Assert
  await expect(page).toHaveURL(/\/login$/);
  expect(logoutCalls).toHaveLength(1);
});
