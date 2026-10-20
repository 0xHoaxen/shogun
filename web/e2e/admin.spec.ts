import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test } from "@playwright/test";

import { AdminService, UserRole, UserStatus } from "../src/gen/shogun/api/v1/admin_pb";
import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const ADMIN_EMAIL = "owner@example.com";
const MEMBER_EMAIL = "member@example.com";
const MEMBER_ID = "0197f0a0-0000-7000-8000-000000000002";

function session(role: string) {
  return {
    session: {
      email: role === "admin" ? ADMIN_EMAIL : MEMBER_EMAIL,
      displayName: "Someone",
      role,
      gmailAllowed: role === "admin",
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  };
}

const users = {
  users: [
    {
      id: "0197f0a0-0000-7000-8000-000000000001",
      email: ADMIN_EMAIL,
      displayName: "Owner",
      role: UserRole.ADMIN,
      status: UserStatus.ACTIVE,
      gmailAllowed: true,
      createdAt: timestampFromDate(new Date("2026-10-01T00:00:00Z")),
    },
    {
      id: MEMBER_ID,
      email: MEMBER_EMAIL,
      displayName: "Member",
      role: UserRole.MEMBER,
      status: UserStatus.ACTIVE,
      gmailAllowed: false,
      createdAt: timestampFromDate(new Date("2026-10-02T00:00:00Z")),
    },
  ],
  nextPageToken: "",
};

test("an admin sees the Users tab and the list of accounts", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("admin"));
  await mockRpc(page, AdminService.method.listUsers, users);

  // Act
  await page.goto("/settings/users");

  // Assert
  await expect(page.getByRole("link", { name: "Users" })).toHaveAttribute("aria-current", "page");
  const table = page.getByRole("table", { name: "Users" });
  await expect(table.getByRole("row")).toHaveCount(3);
  await expect(table).toContainText(MEMBER_EMAIL);
  await expect(table).toContainText("Not approved");
});

test("an admin approves Gmail for a member", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("admin"));
  await mockRpc(page, AdminService.method.listUsers, users);
  const calls = await mockRpc(page, AdminService.method.setGmailAllowed, { user: users.users[1] });
  await page.goto("/settings/users");

  // Act
  await page.getByRole("button", { name: `Approve Gmail for ${MEMBER_EMAIL}` }).click();

  // Assert
  await expect.poll(() => calls.length).toBe(1);
  expect(calls[0].body).toMatchObject({ id: MEMBER_ID, gmailAllowed: true });
});

test("an admin disables a member", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("admin"));
  await mockRpc(page, AdminService.method.listUsers, users);
  const calls = await mockRpc(page, AdminService.method.setUserStatus, { user: users.users[1] });
  await page.goto("/settings/users");

  // Act
  await page.getByRole("button", { name: `Disable ${MEMBER_EMAIL}` }).click();

  // Assert
  await expect.poll(() => calls.length).toBe(1);
  expect(calls[0].body).toMatchObject({ id: MEMBER_ID, status: "USER_STATUS_DISABLED" });
});

test("an admin cannot disable their own account", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("admin"));
  await mockRpc(page, AdminService.method.listUsers, users);
  await page.goto("/settings/users");

  // Assert
  await expect(page.getByRole("button", { name: `Disable ${ADMIN_EMAIL}` })).toBeDisabled();
});

test("a failed change says so", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("admin"));
  await mockRpc(page, AdminService.method.listUsers, users);
  await mockRpcError(page, AdminService.method.setUserStatus, "internal", "boom");
  await page.goto("/settings/users");

  // Act
  await page.getByRole("button", { name: `Disable ${MEMBER_EMAIL}` }).click();

  // Assert
  await expect(page.getByRole("main").getByRole("alert")).toHaveText("That change did not save. Try again.");
});

test("a member has no Users tab and cannot manage users", async ({ page }) => {
  // Arrange
  await mockRpc(page, AuthService.method.getSession, session("member"));
  await mockRpc(page, AdminService.method.listUsers, users);

  // Act
  await page.goto("/settings/users");

  // Assert
  await expect(page.getByRole("link", { name: "Users" })).toHaveCount(0);
  await expect(page.getByRole("main").getByRole("alert")).toHaveText("Only admins can manage users.");
  await expect(page.getByRole("table", { name: "Users" })).toHaveCount(0);
});
