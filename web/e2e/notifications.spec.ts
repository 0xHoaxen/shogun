import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { NotificationType, NotificationsService } from "../src/gen/shogun/api/v1/notifications_pb";
import { mockRpc, mockServerStream } from "./fixtures";

interface Row {
  id: string;
  type: NotificationType;
  title: string;
  body: string;
  link: string;
  createdAt: ReturnType<typeof timestampFromDate>;
  read: boolean;
}

function row(id: string, title: string, read = false, type = NotificationType.DRAFT_READY): Row {
  return { id, type, title, body: "", link: `/drafts/${id}`, createdAt: timestampFromDate(new Date()), read };
}

// mockNotifications serves the list from rows the test can change, the way
// taiko would, and counts unread across all of them.
async function mockNotifications(page: Page, rows: Row[]) {
  await mockRpc(page, AuthService.method.getSession, {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      expiresAt: timestampFromDate(new Date("2030-01-01T00:00:00Z")),
    },
  });
  await mockRpc(page, NotificationsService.method.listNotifications, (body) => ({
    notifications: rows
      .filter((r) => !body.unreadOnly || !r.read)
      .map((r) => ({ ...r, readAt: r.read ? r.createdAt : undefined })),
    unreadCount: rows.filter((r) => !r.read).length,
  }));
  await mockRpc(page, NotificationsService.method.markAllNotificationsRead, () => {
    rows.forEach((r) => (r.read = true));
    return {};
  });
  await mockRpc(page, NotificationsService.method.markNotificationsRead, (body) => {
    const ids = (body.ids as string[]) ?? [];
    rows.forEach((r) => ids.includes(r.id) && (r.read = true));
    return {};
  });
}

test("a new notification appears in the bell and the list without a reload", async ({ page }) => {
  // Arrange: one unread notification, and a stream that stays quiet until released.
  const rows = [row("n1", "Offer received", false, NotificationType.OFFER)];
  await mockNotifications(page, rows);
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => (release = resolve));
  await mockServerStream(page, NotificationsService.method.stream, async (_body, call) => {
    if (call > 1) return [];
    await gate;
    return [{ notification: { id: "n2", title: "Cover letter ready" } }];
  });
  await page.goto("/notifications");
  await expect(page.getByTestId("unread-count")).toHaveText("1");

  // Act: taiko stores a notification and announces it.
  rows.unshift(row("n2", "Cover letter ready"));
  release();

  // Assert: same page, nothing reloaded.
  await expect(page.getByTestId("unread-count")).toHaveText("2");
  await expect(page.getByRole("link", { name: /Cover letter ready/ })).toBeVisible();
  await expect(page.getByRole("heading", { name: "2 unread." })).toBeVisible();
});

test("the bell shows the unread count on every page and links to the list", async ({ page }) => {
  await mockNotifications(page, [row("n1", "One"), row("n2", "Two", true)]);
  await mockServerStream(page, NotificationsService.method.stream, () => []);

  await page.goto("/notifications");

  const bell = page.getByRole("link", { name: "Notifications, 1 unread" });
  await expect(bell).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("link", { name: /One/ })).toHaveAttribute("href", "/drafts/n1");
});

test("mark all read clears the count", async ({ page }) => {
  await mockNotifications(page, [row("n1", "One"), row("n2", "Two")]);
  await mockServerStream(page, NotificationsService.method.stream, () => []);
  await page.goto("/notifications");
  await expect(page.getByTestId("unread-count")).toHaveText("2");

  await page.getByRole("button", { name: "Mark all read" }).click();

  await expect(page.getByTestId("unread-count")).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "All caught up." })).toBeVisible();
  await expect(page.getByRole("button", { name: "Mark all read" })).toBeDisabled();
});

test("opening an unread notification marks it read", async ({ page }) => {
  const rows = [row("n1", "One"), row("n2", "Two")];
  await mockNotifications(page, rows);
  await mockServerStream(page, NotificationsService.method.stream, () => []);
  await page.route("**/drafts/n1", (route) => route.fulfill({ contentType: "text/html", body: "<html></html>" }));
  await page.goto("/notifications");

  await page.getByRole("link", { name: /One/ }).click();

  await expect.poll(() => rows[0].read).toBe(true);
  expect(rows[1].read).toBe(false);
});

test("the page reconnects from the last notification it saw", async ({ page }) => {
  // Arrange: the first stream delivers n7 and ends, as torii's streams do.
  await mockNotifications(page, [row("n7", "Seven")]);
  const calls = await mockServerStream(page, NotificationsService.method.stream, (_body, call) =>
    call === 1 ? [{ notification: { id: "n7", title: "Seven" } }] : [],
  );

  // Act
  await page.goto("/notifications");

  // Assert: the second request asks for what came after n7.
  await expect.poll(() => calls.length, { timeout: 10_000 }).toBeGreaterThan(1);
  const [first, second] = calls.map((call) => call.body as { lastSeenId?: string });
  expect(first.lastSeenId ?? "").toBe("");
  expect(second.lastSeenId).toBe("n7");
});

test("an empty list says so", async ({ page }) => {
  await mockNotifications(page, []);
  await mockServerStream(page, NotificationsService.method.stream, () => []);

  await page.goto("/notifications");

  await expect(page.getByText("Nothing yet.")).toBeVisible();
});
