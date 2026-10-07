import { expect, test } from "@playwright/test";

import { mockRpcError } from "./fixtures";
import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";

test("pages are served with the security headers", async ({ page }) => {
  // Act
  const response = await page.goto("/login");

  // Assert
  const headers = response?.headers() ?? {};
  expect(headers["x-content-type-options"]).toBe("nosniff");
  expect(headers["x-frame-options"]).toBe("DENY");
  expect(headers["referrer-policy"]).toBe("strict-origin-when-cross-origin");
  expect(headers["permissions-policy"]).toContain("camera=()");

  const csp = headers["content-security-policy"] ?? "";
  for (const directive of [
    "default-src 'self'",
    "connect-src 'self'",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ]) {
    expect(csp).toContain(directive);
  }
  expect(csp).not.toContain("*");
});

test("the policy lets the app load and hydrate without a violation", async ({ page }) => {
  // Arrange
  const violations: string[] = [];
  page.on("console", (message) => {
    if (message.text().includes("Content Security Policy")) violations.push(message.text());
  });
  await mockRpcError(page, AuthService.method.getSession, "unauthenticated", "no session");

  // Act: a signed-out visit goes through a client-side redirect, so it needs scripts to run.
  await page.goto("/jobs");

  // Assert
  await expect(page).toHaveURL(/\/login$/);
  expect(violations).toEqual([]);
});
