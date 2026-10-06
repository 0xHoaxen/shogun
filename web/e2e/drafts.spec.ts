import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import {
  DraftChannel,
  DraftKind,
  DraftState,
  DraftTarget,
  DraftsService,
  VersionAuthor,
} from "../src/gen/shogun/api/v1/drafts_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const SUBJECT = "Hello Lumen";
const BODY = "Dear Lumen team,\nI would like to help.";
// hanko.BodyDigest(SUBJECT, BODY), pinned by a golden test in pkg/hanko. The
// browser must compute the same bytes or the server refuses the approval.
const SUBJECT_BODY_DIGEST_HEX = "cb0192909a869c26b21239f1e45504bdd43988692ad93ec0c80ffbf407b3b5aa";
const UNICODE_DIGEST_HEX = "56c9a315ba78c0852aff21f1893447537c0202fafa2a0ec19935f478fda33a69";
const WHEN = timestampFromDate(new Date("2026-10-06T09:00:00Z"));

function base64OfHex(hex: string): string {
  return Buffer.from(hex, "hex").toString("base64");
}

interface Scenario {
  state: DraftState;
  channel: DraftChannel;
  currentVersion: number;
  version: number;
  recipient: string;
  versions: { version: number; subject: string; body: string; author: VersionAuthor; extraContext?: string }[];
}

function emailScenario(): Scenario {
  return {
    state: DraftState.PENDING,
    channel: DraftChannel.EMAIL,
    currentVersion: 1,
    version: 3,
    recipient: "jobs@lumen.example",
    versions: [{ version: 1, subject: SUBJECT, body: BODY, author: VersionAuthor.AI }],
  };
}

function draftOf(s: Scenario) {
  const newest = s.versions.find((v) => v.version === s.currentVersion);
  return {
    id: "d1",
    kind: DraftKind.COVER_LETTER,
    target: DraftTarget.JOB,
    targetId: "j1",
    channel: s.channel,
    state: s.state,
    currentVersion: s.currentVersion,
    recipient: s.recipient,
    version: s.version,
    subject: newest?.subject ?? "",
    preview: newest?.body.slice(0, 200) ?? "",
    createdAt: WHEN,
    updatedAt: WHEN,
  };
}

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
}

// serve answers GetDraft from the scenario, which a test changes to show what
// the server would say after an action.
async function serve(page: Page, s: Scenario) {
  await signIn(page);
  await mockRpc(page, DraftsService.method.getDraft, () => ({
    draft: draftOf(s),
    versions: [...s.versions]
      .sort((a, b) => b.version - a.version)
      .map((v) => ({ ...v, extraContext: v.extraContext ?? "", createdAt: WHEN })),
  }));
}

test("the queue lists drafts with subject and preview and asks for the chosen state", async ({ page }) => {
  await signIn(page);
  const calls = await mockRpc(page, DraftsService.method.listQueue, (body) => ({
    drafts:
      body.state === "DRAFT_STATE_SENT"
        ? []
        : [{ ...draftOf(emailScenario()), preview: "Dear Lumen team, I would like to help." }],
  }));

  await page.goto("/drafts");

  const row = page.getByRole("row", { name: /Hello Lumen/ });
  await expect(row).toContainText("Dear Lumen team");
  await expect(row).toContainText("jobs@lumen.example");
  await expect(row).toContainText("Needs you");
  await expect(row.getByRole("link", { name: "Review" })).toHaveAttribute("href", "/drafts/d1");
  expect(calls[0].body).toMatchObject({ state: "DRAFT_STATE_PENDING" });

  await page.getByRole("button", { name: "Sent", exact: true }).click();

  await expect(page.getByText("Nothing here.")).toBeVisible();
  expect(calls.at(-1)?.body).toMatchObject({ state: "DRAFT_STATE_SENT" });
});

test("a new draft is requested with an idempotency key and opens its page", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, DraftsService.method.listQueue, { drafts: [] });
  const created = await mockRpc(page, DraftsService.method.generateDraft, {
    draft: { ...draftOf(emailScenario()), state: DraftState.GENERATING, currentVersion: 0 },
  });
  await mockRpc(page, DraftsService.method.getDraft, {
    draft: { ...draftOf(emailScenario()), state: DraftState.GENERATING, currentVersion: 0 },
    versions: [],
  });
  await page.goto("/drafts");

  await page.getByRole("button", { name: "New draft" }).click();
  await page.getByLabel("Channel").selectOption({ label: "Email" });
  await page.getByLabel("To").fill("jobs@lumen.example");
  await page.getByLabel("What should it say?").fill("A short note about the role");
  await page.getByRole("button", { name: "Write draft" }).click();

  await expect(page).toHaveURL(/\/drafts\/d1$/);
  await expect(page.getByRole("status")).toContainText("Writing the first version");
  expect(created).toHaveLength(1);
  expect(created[0].body).toMatchObject({
    kind: "DRAFT_KIND_POST",
    target: "DRAFT_TARGET_NONE",
    channel: "DRAFT_CHANNEL_EMAIL",
    recipient: "jobs@lumen.example",
    extraContext: "A short note about the role",
  });
  expect(created[0].headers["idempotency-key"]).toMatch(/^[0-9a-f-]{36}$/);
});

test("approving an email sends the digest of the exact stored text and then shows it sent", async ({ page }) => {
  const s = emailScenario();
  await serve(page, s);
  const approvals = await mockRpc(page, DraftsService.method.approve, () => {
    s.state = DraftState.APPROVED;
    return { draft: draftOf(s), copyReady: false };
  });
  await page.goto("/drafts/d1");

  const preview = page.getByTestId("send-preview");
  await expect(preview).toContainText("jobs@lumen.example");
  await expect(preview).toContainText(SUBJECT);
  await expect(preview).toContainText("I would like to help.");
  await page.getByRole("button", { name: "Approve and send" }).click();

  await expect(page.getByText("The email is being sent")).toBeVisible();
  expect(approvals).toHaveLength(1);
  expect(approvals[0].body).toMatchObject({
    id: "d1",
    version: 1,
    bodySha256: base64OfHex(SUBJECT_BODY_DIGEST_HEX),
  });

  s.state = DraftState.SENT; // tsubame reports the mail out; the page is polling
  await expect(page.getByRole("status").filter({ hasText: "Sent." })).toBeVisible({ timeout: 15_000 });
});

test("the digest covers multi-byte text as the server counts it", async ({ page }) => {
  const s = emailScenario();
  s.versions = [{ version: 1, subject: "Café ✓", body: "Grüße — 日本語", author: VersionAuthor.AI }];
  await serve(page, s);
  const approvals = await mockRpc(page, DraftsService.method.approve, { draft: draftOf(s), copyReady: false });
  await page.goto("/drafts/d1");

  await page.getByRole("button", { name: "Approve and send" }).click();

  await expect.poll(() => approvals.length).toBe(1);
  expect(approvals[0].body).toMatchObject({ bodySha256: base64OfHex(UNICODE_DIGEST_HEX) });
});

test("editing saves a new version and approval waits until the edit is saved", async ({ page }) => {
  const s = emailScenario();
  await serve(page, s);
  const saves = await mockRpc(page, DraftsService.method.editDraft, (body) => {
    s.versions.push({ version: 2, subject: String(body.subject), body: String(body.body), author: VersionAuthor.USER });
    s.currentVersion = 2;
    s.version = 4;
    return { draft: draftOf(s), draftVersion: { version: 2, subject: String(body.subject), body: String(body.body), author: VersionAuthor.USER } };
  });
  await page.goto("/drafts/d1");
  const approve = page.getByRole("button", { name: "Approve and send" });
  await expect(approve).toBeEnabled();

  await page.getByLabel("Body").fill("A better letter.");

  await expect(approve).toBeDisabled();
  await expect(page.getByText("You have an unsaved edit")).toBeVisible();
  await page.getByRole("button", { name: "Save as new version" }).click();

  await expect(page.getByRole("button", { name: /v2/ })).toContainText("current");
  await expect(page.getByTestId("send-preview")).toContainText("A better letter.");
  await expect(approve).toBeEnabled();
  expect(saves[0].body).toMatchObject({ id: "d1", version: 3, subject: SUBJECT, body: "A better letter." });
});

test("an older version can be read but not approved", async ({ page }) => {
  const s = emailScenario();
  s.currentVersion = 2;
  s.versions.push({ version: 2, subject: SUBJECT, body: "The second take.", author: VersionAuthor.USER });
  await serve(page, s);
  await page.goto("/drafts/d1");

  await page.getByRole("button", { name: /v1/ }).click();

  await expect(page.getByLabel("Body")).toHaveValue(BODY);
  await expect(page.getByRole("button", { name: "Approve and send" })).toBeDisabled();
  await expect(page.getByText("Only the current version, 2, can be approved.")).toBeVisible();
  await expect(page.getByTestId("send-preview")).toContainText("The second take.");
});

test("regenerating sends the note and the version, and the new version appears by itself", async ({ page }) => {
  const s = emailScenario();
  await serve(page, s);
  const regenerated = await mockRpc(page, DraftsService.method.regenerate, () => ({ draft: draftOf(s) }));
  await page.goto("/drafts/d1");

  await page.getByLabel("Anything to change or keep in mind?").fill("shorter, mention Go");
  await page.getByRole("button", { name: "Regenerate" }).click();

  await expect(page.getByText("Writing a new version")).toBeVisible();
  expect(regenerated[0].body).toMatchObject({ id: "d1", version: 3, extraContext: "shorter, mention Go" });

  s.versions.push({ version: 2, subject: SUBJECT, body: "Shorter, with Go.", author: VersionAuthor.AI, extraContext: "shorter, mention Go" });
  s.currentVersion = 2;
  await expect(page.getByTestId("send-preview")).toContainText("Shorter, with Go.", { timeout: 15_000 });
  await expect(page.getByText("Writing a new version")).toBeHidden();
});

test("a copy-only draft is approved, then copied by hand, and nothing is sent", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const s = emailScenario();
  s.channel = DraftChannel.LINKEDIN;
  s.recipient = "";
  s.versions = [{ version: 1, subject: "", body: "Excited to share what I learned.", author: VersionAuthor.AI }];
  await serve(page, s);
  const approvals = await mockRpc(page, DraftsService.method.approve, () => {
    s.state = DraftState.APPROVED;
    return { draft: draftOf(s), copyReady: true };
  });
  await page.goto("/drafts/d1");
  await expect(page.getByTestId("send-preview")).not.toContainText("To ");

  await page.getByRole("button", { name: "Approve", exact: true }).click();

  await expect(page.getByText("Approved. Copy it and post it yourself.")).toBeVisible();
  expect(approvals).toHaveLength(1);
  await page.getByRole("button", { name: "Copy text" }).click();
  await expect(page.getByRole("button", { name: "Copied" })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("Excited to share what I learned.");
});

test("an approved copy-only draft is marked as posted once the owner has posted it", async ({ page }) => {
  const s = emailScenario();
  s.channel = DraftChannel.LINKEDIN;
  s.recipient = "";
  s.state = DraftState.APPROVED;
  s.versions = [{ version: 1, subject: "", body: "Excited to share what I learned.", author: VersionAuthor.AI }];
  await serve(page, s);
  const posted = await mockRpc(page, DraftsService.method.markPosted, () => {
    s.state = DraftState.SENT;
    return { draft: draftOf(s) };
  });
  await page.goto("/drafts/d1");

  await page.getByRole("button", { name: "Mark as posted" }).click();

  await expect(page.getByRole("status").filter({ hasText: "Sent." })).toBeVisible();
  expect(posted).toHaveLength(1);
  expect(posted[0].body).toMatchObject({ id: "d1", version: draftOf(emailScenario()).version });
  await expect(page.getByRole("button", { name: "Mark as posted" })).toBeHidden();
});

test("a refused mark as posted says why and shows the draft as it now is", async ({ page }) => {
  const s = emailScenario();
  s.channel = DraftChannel.X;
  s.recipient = "";
  s.state = DraftState.APPROVED;
  s.versions = [{ version: 1, subject: "", body: "A short post.", author: VersionAuthor.AI }];
  await serve(page, s);
  await mockRpcError(page, DraftsService.method.markPosted, "failed_precondition", "draft is not approved", "DRAFT_STATE_INVALID_TRANSITION");
  await page.goto("/drafts/d1");
  await expect(page.getByRole("button", { name: "Mark as posted" })).toBeVisible();
  s.state = DraftState.PENDING; // edited elsewhere while this page was open

  await page.getByRole("button", { name: "Mark as posted" }).click();

  await expect(page.getByRole("button", { name: "Mark as posted" })).toBeHidden();
  await expect(page.getByRole("button", { name: "Discard draft" })).toBeVisible();
});

const REFUSALS = [
  ["VERSION_CONFLICT", "aborted", "This draft changed since you opened it"],
  ["BODY_HASH_MISMATCH", "invalid_argument", "This draft changed since you opened it"],
  ["SEND_REFUSED", "failed_precondition", "Is your mail account connected?"],
  ["SEND_FAILED", "failed_precondition", "Your mail provider did not send it"],
  ["SEND_UNAVAILABLE", "unavailable", "nothing was sent"],
  ["SEND_STATUS_UNKNOWN", "unavailable", "Check your Sent folder before approving again"],
] as const;

for (const [reason, code, message] of REFUSALS) {
  test(`a refused approval (${reason}) tells the owner what became of the mail`, async ({ page }) => {
    await serve(page, emailScenario());
    await mockRpcError(page, DraftsService.method.approve, code, "refused", reason);
    await page.goto("/drafts/d1");

    await page.getByRole("button", { name: "Approve and send" }).click();

    await expect(page.locator('p[role="alert"]')).toContainText(message);
  });
}

test("discarding sends the draft's version", async ({ page }) => {
  const s = emailScenario();
  await serve(page, s);
  const discarded = await mockRpc(page, DraftsService.method.discard, () => {
    s.state = DraftState.DISCARDED;
    return { draft: draftOf(s) };
  });
  await page.goto("/drafts/d1");

  await page.getByRole("button", { name: "Discard draft" }).click();

  await expect(page.getByTestId("draft-state")).toHaveText("Discarded");
  expect(discarded[0].body).toMatchObject({ id: "d1", version: 3 });
  await expect(page.getByRole("button", { name: "Approve and send" })).toBeHidden();
});

test("a failed draft says nothing was sent and offers no approval", async ({ page }) => {
  const s = emailScenario();
  s.state = DraftState.FAILED;
  s.currentVersion = 0;
  s.versions = [];
  await serve(page, s);

  await page.goto("/drafts/d1");

  await expect(page.locator('p[role="alert"]')).toContainText("could not be written");
  await expect(page.locator('p[role="alert"]')).toContainText("Nothing was sent");
  await expect(page.getByRole("button", { name: /Approve/ })).toBeHidden();
});
