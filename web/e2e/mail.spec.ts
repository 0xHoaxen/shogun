import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { MailService } from "../src/gen/shogun/api/v1/mail_pb";
import { mockRpc, mockRpcError } from "./fixtures";

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
}

test("connecting sends the browser to Google's address", async ({ page }) => {
  await signIn(page);
  const connects = await mockRpc(page, MailService.method.connectAccount, {
    authUrl: "https://accounts.example/auth?state=abc",
  });
  await page.route("https://accounts.example/**", (route) =>
    route.fulfill({ status: 200, contentType: "text/html", body: "<h1>Google sign-in</h1>" }),
  );
  await page.goto("/settings/mail");

  await page.getByRole("button", { name: "Connect Gmail" }).click();

  await expect(page).toHaveURL("https://accounts.example/auth?state=abc");
  expect(connects).toHaveLength(1);
});

test("an address that is not https is never followed", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, MailService.method.connectAccount, { authUrl: "http://evil.example/steal" });
  await page.goto("/settings/mail");

  await page.getByRole("button", { name: "Connect Gmail" }).click();

  await expect(page.locator('p[role="alert"]')).toContainText("could not be trusted");
  await expect(page).toHaveURL(/\/settings\/mail$/);
});

test("a failed start shows the error and stays put", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, MailService.method.connectAccount, "unavailable", "the service is busy, try again", "UNAVAILABLE");
  await page.goto("/settings/mail");

  await page.getByRole("button", { name: "Connect Gmail" }).click();

  await expect(page.locator('p[role="alert"]')).toContainText("the service is busy");
});

test("coming back from Google finishes the connection once and clears the address bar", async ({ page }) => {
  await signIn(page);
  const completes = await mockRpc(page, MailService.method.completeConnect, { address: "me@example.com" });

  await page.goto("/mail/callback?code=4%2Fsecret-code&state=opaque-state");

  await expect(page.getByRole("status")).toContainText("Connected me@example.com");
  expect(completes).toHaveLength(1);
  expect(completes[0].body).toMatchObject({ code: "4/secret-code", state: "opaque-state" });
  expect(page.url()).not.toContain("secret-code");
  expect(page.url()).not.toContain("opaque-state");
});

test("an expired or foreign link says to start again", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, MailService.method.completeConnect, "invalid_argument", "state is invalid", "INVALID_STATE");

  await page.goto("/mail/callback?code=c&state=s");

  await expect(page.locator('p[role="alert"]')).toContainText("expired or is not valid");
  await expect(page.getByRole("link", { name: "Back to mail settings" })).toHaveAttribute("href", "/settings/mail");
});

test("a code Google refused says so", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, MailService.method.completeConnect, "invalid_argument", "refused", "INVALID_CODE");

  await page.goto("/mail/callback?code=c&state=s");

  await expect(page.locator('p[role="alert"]')).toContainText("Google refused the sign-in");
});

test("denying access connects nothing and calls nothing", async ({ page }) => {
  await signIn(page);
  const completes = await mockRpc(page, MailService.method.completeConnect, { address: "x" });

  await page.goto("/mail/callback?error=access_denied&state=s");

  await expect(page.locator('p[role="alert"]')).toContainText("did not give access");
  expect(completes).toHaveLength(0);
});

test("the callback without a code explains where to start and calls nothing", async ({ page }) => {
  await signIn(page);
  const completes = await mockRpc(page, MailService.method.completeConnect, { address: "x" });

  await page.goto("/mail/callback");

  await expect(page.locator('p[role="alert"]')).toContainText("Start from Settings");
  expect(completes).toHaveLength(0);
});
