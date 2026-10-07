import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { DraftsService } from "../src/gen/shogun/api/v1/drafts_pb";
import { ItemKind, ItemStatus, LearningService } from "../src/gen/shogun/api/v1/learning_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const WHEN = timestampFromDate(new Date("2026-10-06T09:00:00Z"));

interface ItemInit {
  id: string;
  title: string;
  kind: ItemKind;
  status: ItemStatus;
  version: number;
  insight?: string;
  startedOn?: string;
  completedOn?: string;
}

function itemOf(i: ItemInit) {
  return { insight: "", startedOn: "", completedOn: "", url: "", createdAt: WHEN, updatedAt: WHEN, ...i };
}

function activityOf(id: string, summary: string, itemId = "") {
  return { id, itemId, summary, minutes: 45, occurredOn: "2026-10-06", tags: ["go"], createdAt: WHEN };
}

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
}

const GO_COURSE = { id: "i1", title: "Go course", kind: ItemKind.COURSE, status: ItemStatus.PLANNED, version: 1, insight: "channels first" };

test("the screen lists items and activities and asks for the chosen status", async ({ page }) => {
  await signIn(page);
  const items = await mockRpc(page, LearningService.method.listLearningItems, (body) => ({
    items: body.status === "ITEM_STATUS_DONE" ? [] : [itemOf(GO_COURSE)],
  }));
  await mockRpc(page, LearningService.method.listLearningActivities, {
    activities: [activityOf("a1", "built a worker pool", "i1")],
  });

  await page.goto("/learning");

  const row = page.getByRole("row", { name: /^Go course/ });
  await expect(row).toContainText("channels first");
  await expect(row).toContainText("Course");
  await expect(row).toContainText("Planned");
  const activity = page.getByRole("row", { name: /built a worker pool/ });
  await expect(activity).toContainText("Go course");
  await expect(activity).toContainText("2026-10-06");

  await page.getByRole("button", { name: "Done", exact: true }).click();

  await expect(page.getByText("Nothing here.")).toBeVisible();
  expect(items.some((call) => (call.body as { status?: string }).status === "ITEM_STATUS_DONE")).toBe(true);
});

test("adding an item sends its fields and shows the refreshed list", async ({ page }) => {
  await signIn(page);
  const stored: ItemInit[] = [];
  await mockRpc(page, LearningService.method.listLearningItems, () => ({ items: stored.map(itemOf) }));
  await mockRpc(page, LearningService.method.listLearningActivities, { activities: [] });
  const adds = await mockRpc(page, LearningService.method.addLearningItem, (body) => {
    const item = { id: "i2", title: String(body.title), kind: ItemKind.BOOK, status: ItemStatus.PLANNED, version: 1 };
    stored.push(item);
    return { item: itemOf(item) };
  });
  await page.goto("/learning");

  await page.getByRole("button", { name: "Add item" }).click();
  await page.getByLabel("Title").fill("  Designing Data-Intensive Applications ");
  await page.getByLabel("Kind").selectOption({ label: "Book" });
  await page.getByLabel("Link").fill("https://example.com/ddia");
  await page.getByRole("button", { name: "Add item" }).last().click();

  await expect(page.getByRole("row", { name: /Designing Data-Intensive Applications/ })).toBeVisible();
  expect(adds[0].body).toMatchObject({
    title: "Designing Data-Intensive Applications", kind: "ITEM_KIND_BOOK", url: "https://example.com/ddia",
  });
});

test("starting an item moves it with the version the page read", async ({ page }) => {
  await signIn(page);
  let current = itemOf(GO_COURSE);
  await mockRpc(page, LearningService.method.listLearningItems, () => ({ items: [current] }));
  await mockRpc(page, LearningService.method.listLearningActivities, { activities: [] });
  const moves = await mockRpc(page, LearningService.method.changeLearningItemStatus, () => {
    current = itemOf({ ...GO_COURSE, status: ItemStatus.IN_PROGRESS, version: 2, startedOn: "2026-10-07" });
    return { item: current };
  });
  await page.goto("/learning");

  await page.getByRole("button", { name: "Start Go course" }).click();

  const row = page.getByRole("row", { name: /^Go course/ });
  await expect(row).toContainText("In progress");
  await expect(row).toContainText("Since 2026-10-07");
  await expect(page.getByRole("button", { name: "Finish Go course" })).toBeVisible();
  expect(moves[0].body).toMatchObject({ id: "i1", toStatus: "ITEM_STATUS_IN_PROGRESS", version: 1 });
});

test("a refused move says why and shows what the server holds", async ({ page }) => {
  await signIn(page);
  let current = itemOf(GO_COURSE);
  await mockRpc(page, LearningService.method.listLearningItems, () => ({ items: [current] }));
  await mockRpc(page, LearningService.method.listLearningActivities, { activities: [] });
  await page.goto("/learning");
  await expect(page.getByRole("button", { name: "Start Go course" })).toBeVisible();
  // Another tab finished the course after this page loaded.
  current = itemOf({ ...GO_COURSE, status: ItemStatus.DONE, version: 5, completedOn: "2026-10-07" });
  await mockRpcError(page, LearningService.method.changeLearningItemStatus, "aborted", "stale", "VERSION_CONFLICT");

  await page.getByRole("button", { name: "Start Go course" }).click();

  await expect(page.locator("p[role=alert]")).toContainText("changed somewhere else");
  await expect(page.getByRole("row", { name: /^Go course/ })).toContainText("Done 2026-10-07");
});

test("a move the state machine refuses is explained", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, LearningService.method.listLearningItems, { items: [itemOf(GO_COURSE)] });
  await mockRpc(page, LearningService.method.listLearningActivities, { activities: [] });
  await mockRpcError(page, LearningService.method.changeLearningItemStatus, "failed_precondition", "no", "ITEM_STATUS_INVALID_TRANSITION");
  await page.goto("/learning");

  await page.getByRole("button", { name: "Finish Go course" }).click();

  await expect(page.locator("p[role=alert]")).toContainText("not allowed");
});

test("logging an activity sends the fields and refreshes the activity", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, LearningService.method.listLearningItems, { items: [itemOf(GO_COURSE)] });
  const stored: ReturnType<typeof activityOf>[] = [];
  await mockRpc(page, LearningService.method.listLearningActivities, () => ({ activities: stored }));
  const logs = await mockRpc(page, LearningService.method.logLearningActivity, (body) => {
    const activity = activityOf("a1", String(body.summary), String(body.itemId));
    stored.push(activity);
    return { activity };
  });
  await page.goto("/learning");

  await page.getByRole("button", { name: "Log activity" }).first().click();
  await page.getByLabel("What did you learn?").fill("read the race detector docs");
  await page.getByLabel("Part of").selectOption({ label: "Go course" });
  await page.getByLabel("Minutes").fill("30");
  await page.getByLabel("Tags, separated by commas").fill("go, testing , ");
  await page.getByRole("button", { name: "Log activity" }).last().click();

  await expect(page.getByRole("row", { name: /read the race detector docs/ })).toBeVisible();
  expect(logs[0].body).toMatchObject({ itemId: "i1", summary: "read the race detector docs", minutes: 30, tags: ["go", "testing"] });
});

test("a validation message from the server is shown in the dialog", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, LearningService.method.listLearningItems, { items: [] });
  await mockRpc(page, LearningService.method.listLearningActivities, { activities: [] });
  await mockRpcError(page, LearningService.method.addLearningItem, "invalid_argument", "invalid input: url must be an http or https address", "INVALID_ARGUMENT");
  await page.goto("/learning");

  await page.getByRole("button", { name: "Add item" }).click();
  await page.getByLabel("Title").fill("x");
  await page.getByRole("button", { name: "Add item" }).last().click();

  await expect(page.locator("p[role=alert]")).toHaveText("url must be an http or https address");
});

test("chosen activities become a post draft and the page opens it", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, LearningService.method.listLearningItems, { items: [itemOf(GO_COURSE)] });
  await mockRpc(page, LearningService.method.listLearningActivities, {
    activities: [activityOf("a1", "built a worker pool", "i1"), activityOf("a2", "read the race detector docs")],
  });
  await mockRpc(page, DraftsService.method.getDraft, { draft: { id: "d9" }, versions: [] });
  await mockRpc(page, DraftsService.method.listQueue, { drafts: [] });
  const posts = await mockRpc(page, LearningService.method.generateLearningPost, { draftId: "d9" });
  await page.goto("/learning");
  const write = page.getByRole("button", { name: /^Write a post/ });
  await expect(write).toBeDisabled();

  await page.getByRole("checkbox", { name: "Choose: built a worker pool" }).check();
  await page.getByRole("checkbox", { name: "Choose: read the race detector docs" }).check();
  await expect(write).toHaveText("Write a post (2)");
  await write.click();
  await page.getByLabel("Where will you post it?").selectOption({ label: "X" });
  await page.getByRole("button", { name: "Write the post" }).click();

  await expect(page).toHaveURL(/\/drafts\/d9$/);
  expect(posts[0].body).toMatchObject({ activityIds: ["a1", "a2"], channel: "DRAFT_CHANNEL_X" });
});

test("a post that cannot be started says so and stays on the page", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, LearningService.method.listLearningItems, { items: [] });
  await mockRpc(page, LearningService.method.listLearningActivities, { activities: [activityOf("a1", "built a worker pool")] });
  await mockRpcError(page, LearningService.method.generateLearningPost, "unavailable", "busy", "UNAVAILABLE");
  await page.goto("/learning");

  await page.getByRole("checkbox", { name: "Choose: built a worker pool" }).check();
  await page.getByRole("button", { name: /^Write a post/ }).click();
  await page.getByRole("button", { name: "Write the post" }).click();

  await expect(page.locator("p[role=alert]")).toContainText("busy right now");
  await expect(page).toHaveURL(/\/learning$/);
});

test("the nav has a Learning tab", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, LearningService.method.listLearningItems, { items: [] });
  await mockRpc(page, LearningService.method.listLearningActivities, { activities: [] });

  await page.goto("/learning");

  await expect(page.getByRole("link", { name: "Learning" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "What you're learning." })).toBeVisible();
});
