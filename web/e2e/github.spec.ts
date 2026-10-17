import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { ProfileService } from "../src/gen/shogun/api/v1/profile_pb";
import { mockRpc, mockRpcError } from "./fixtures";

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
}

test("a user who has not connected GitHub sees so and can start connecting", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.getGitHubAccount, { connected: false });
  const connects = await mockRpc(page, ProfileService.method.connectGitHub, {
    authUrl: "https://github.example/login/oauth/authorize?state=abc",
  });
  await page.route("https://github.example/**", (route) =>
    route.fulfill({ status: 200, contentType: "text/html", body: "<h1>GitHub</h1>" }),
  );
  await page.goto("/settings/github");
  await expect(page.getByRole("status")).toContainText("Not connected");

  await page.getByRole("button", { name: "Connect GitHub" }).click();

  await expect(page).toHaveURL("https://github.example/login/oauth/authorize?state=abc");
  expect(connects).toHaveLength(1);
});

test("a connected user sees their login and can disconnect", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.getGitHubAccount, { connected: true, login: "octocat" });
  const disconnects = await mockRpc(page, ProfileService.method.disconnectGitHub, {});
  await page.goto("/settings/github");
  await expect(page.getByRole("status")).toContainText("Connected as octocat");

  await page.getByRole("button", { name: "Disconnect GitHub" }).click();

  await expect.poll(() => disconnects.length).toBe(1);
});

test("an address that is not https is never followed", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.getGitHubAccount, { connected: false });
  await mockRpc(page, ProfileService.method.connectGitHub, { authUrl: "http://evil.example/steal" });
  await page.goto("/settings/github");

  await page.getByRole("button", { name: "Connect GitHub" }).click();

  await expect(page.locator('p[role="alert"]')).toContainText("could not be trusted");
  await expect(page).toHaveURL(/\/settings\/github$/);
});

test("a server with no GitHub OAuth app says so", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.getGitHubAccount, { connected: false });
  await mockRpcError(page, ProfileService.method.connectGitHub, "failed_precondition", "not set up", "GITHUB_NOT_CONFIGURED");
  await page.goto("/settings/github");

  await page.getByRole("button", { name: "Connect GitHub" }).click();

  await expect(page.locator('p[role="alert"]')).toContainText("not set up on this Shogun");
});

test("coming back from GitHub finishes the connection once and clears the address bar", async ({ page }) => {
  await signIn(page);
  const completes = await mockRpc(page, ProfileService.method.completeGitHubConnect, { login: "octocat" });

  await page.goto("/settings/github/callback?code=secret-code&state=opaque-state");

  await expect(page.getByRole("status")).toContainText("Connected octocat");
  expect(completes).toHaveLength(1);
  expect(completes[0].body).toMatchObject({ code: "secret-code", state: "opaque-state" });
  expect(page.url()).not.toContain("secret-code");
  expect(page.url()).not.toContain("opaque-state");
});

test("an expired or foreign link says to start again", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, ProfileService.method.completeGitHubConnect, "invalid_argument", "state is invalid", "INVALID_STATE");

  await page.goto("/settings/github/callback?code=c&state=s");

  await expect(page.locator('p[role="alert"]')).toContainText("expired or is not valid");
  await expect(page.getByRole("link", { name: "Back to GitHub settings" })).toHaveAttribute("href", "/settings/github");
});

test("a code GitHub refused says so", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, ProfileService.method.completeGitHubConnect, "invalid_argument", "refused", "INVALID_CODE");

  await page.goto("/settings/github/callback?code=c&state=s");

  await expect(page.locator('p[role="alert"]')).toContainText("GitHub refused the sign-in");
});

test("denying access connects nothing and calls nothing", async ({ page }) => {
  await signIn(page);
  const completes = await mockRpc(page, ProfileService.method.completeGitHubConnect, { login: "x" });

  await page.goto("/settings/github/callback?error=access_denied&state=s");

  await expect(page.locator('p[role="alert"]')).toContainText("did not give access");
  expect(completes).toHaveLength(0);
});

test("Profile points a user with no GitHub connection to Settings", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProfileService.method.listProfileSuggestions, { suggestions: [], nextPageToken: "" });
  await mockRpcError(page, ProfileService.method.syncGitHub, "failed_precondition", "not connected", "GITHUB_NOT_CONNECTED");
  await page.goto("/profile");

  await page.getByRole("button", { name: "Sync GitHub" }).click();

  await expect(page.getByRole("link", { name: "Connect GitHub in Settings" })).toHaveAttribute("href", "/settings/github");
});
