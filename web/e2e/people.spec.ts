import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { ContactStatus, ContactsService } from "../src/gen/shogun/api/v1/contacts_pb";
import { DraftsService } from "../src/gen/shogun/api/v1/drafts_pb";
import { JobStatus } from "../src/gen/shogun/api/v1/jobs_pb";
import { mockRpc, mockRpcError } from "./fixtures";

// The same calendar day the app uses for "today" (the owner's zone).
const dayFormat = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata" });
function day(offset: number): string {
  return dayFormat.format(new Date(Date.now() + offset * 24 * 60 * 60 * 1000));
}

interface Person {
  id: string;
  fullName: string;
  role: string;
  companyName: string;
  relationship: string;
  lastContacted: string;
  nextFollowUp: string;
  email: string;
  howWeMet: string;
  preferredChannel: string;
  status: ContactStatus;
  version: number;
}

function person(over: Partial<Person> & Pick<Person, "id" | "fullName">): Person {
  return {
    role: "",
    companyName: "",
    relationship: "",
    lastContacted: "",
    nextFollowUp: "",
    email: "",
    howWeMet: "",
    preferredChannel: "",
    status: ContactStatus.NOT_REACHED,
    version: 3,
    ...over,
  };
}

const PRIYA = person({
  id: "c1", fullName: "Priya Raman", role: "Technical recruiter", companyName: "Halden Labs", relationship: "recruiter",
  lastContacted: day(0), nextFollowUp: day(-1), email: "priya@haldenlabs.example", howWeMet: "Inbound, 6 Oct",
});
const ARJUN = person({
  id: "c2", fullName: "Arjun Mehta", role: "Staff engineer", companyName: "Ferro Systems", relationship: "engineer",
  lastContacted: day(-12), nextFollowUp: day(-3), email: "arjun@ferro.example",
});
const SARA = person({
  id: "c3", fullName: "Sara Lindqvist", role: "Engineering manager", companyName: "Tessellate", relationship: "manager",
  lastContacted: day(-2), nextFollowUp: day(5),
});
const TOM = person({ id: "c4", fullName: "Tom Okafor", role: "Head of platform", companyName: "Brightline" });

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", role: "member", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
}

interface Detail {
  history?: { id: string; kind: string; text?: string; at: Date }[];
  jobs?: { id: string; title: string; status: JobStatus }[];
}

// serve answers the list and each person's page from the people given.
async function serve(page: Page, people: Person[], details: Record<string, Detail> = {}) {
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, () => ({ contacts: people }));
  return mockRpc(page, ContactsService.method.getContact, (body) => {
    const found = people.find((p) => p.id === body.id) ?? people[0];
    const detail = details[found.id] ?? {};
    return {
      contact: found,
      history: (detail.history ?? []).map((h) => ({ id: h.id, kind: h.kind, text: h.text ?? "", occurredAt: timestampFromDate(h.at) })),
      linkedJobs: (detail.jobs ?? []).map((j) => ({ id: j.id, title: j.title, companyName: found.companyName, status: j.status, version: 1 })),
    };
  });
}

test("the list shows each person with role, company, last contact and who is due", async ({ page }) => {
  await serve(page, [PRIYA, ARJUN, SARA, TOM]);

  await page.goto("/people");

  const list = page.getByRole("list", { name: "People" });
  const priya = list.getByRole("listitem").filter({ hasText: "Priya Raman" });
  await expect(priya).toContainText("Technical recruiter · Halden Labs");
  await expect(priya).toContainText("today");
  await expect(priya.getByText("Write back")).toBeVisible();
  await expect(list.getByRole("listitem").filter({ hasText: "Arjun Mehta" })).toContainText("1w ago");
  await expect(list.getByRole("listitem").filter({ hasText: "Sara Lindqvist" }).getByText("Write back")).toHaveCount(0);
  await expect(list.getByRole("listitem").filter({ hasText: "Tom Okafor" })).toContainText("never");
});

test("the chips count people by relationship and filter the list", async ({ page }) => {
  await serve(page, [PRIYA, ARJUN, SARA, TOM]);
  await page.goto("/people");
  const chips = page.getByRole("group", { name: "Filter people" });

  // Tom has no relationship set, so he counts under Other. A relationship nobody has gets no chip.
  await expect(chips.getByRole("button", { name: "All 4" })).toHaveAttribute("aria-pressed", "true");
  await expect(chips.getByRole("button", { name: "Recruiters 1" })).toBeVisible();
  await expect(chips.getByRole("button", { name: "Other 1" })).toBeVisible();
  await expect(chips.getByRole("button", { name: /Friends/ })).toHaveCount(0);
  await expect(chips.getByRole("button", { name: "Write back 2" })).toBeVisible();

  await chips.getByRole("button", { name: "Write back 2" }).click();

  const list = page.getByRole("list", { name: "People" });
  await expect(list.getByRole("listitem")).toHaveCount(2);
  await expect(list).toContainText("Priya Raman");
  await expect(list).toContainText("Arjun Mehta");

  await chips.getByRole("button", { name: "Managers 1" }).click();
  await expect(list.getByRole("listitem")).toHaveCount(1);
  await expect(list).toContainText("Sara Lindqvist");
});

test("an empty book offers to add someone or import a list", async ({ page }) => {
  await serve(page, []);

  await page.goto("/people");

  await expect(page.getByText("Nobody here yet.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Add person" }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Import CSV" }).first()).toBeVisible();
});

test("a failed load offers a retry", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, ContactsService.method.listContacts, "unavailable", "down");

  await page.goto("/people");

  await expect(page.getByRole("main").getByRole("alert")).toContainText("Couldn't load your people");
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});

test("every page of people is loaded, so the counts are the whole book", async ({ page }) => {
  await signIn(page);
  const make = (from: number, n: number) => Array.from({ length: n }, (_, i) => person({ id: `p${from + i}`, fullName: `Person ${from + i}`, relationship: "friend" }));
  const calls = await mockRpc(page, ContactsService.method.listContacts, (body) => {
    const token = (body.pageToken as string | undefined) ?? "";
    if (token === "") return { contacts: make(0, 200), nextPageToken: "a" };
    if (token === "a") return { contacts: make(200, 200), nextPageToken: "b" };
    return { contacts: make(400, 50) };
  });
  await mockRpc(page, ContactsService.method.getContact, { contact: make(0, 1)[0] });

  await page.goto("/people");

  await expect(page.getByRole("button", { name: "All 450" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Friends 450" })).toBeVisible();
  expect(calls).toHaveLength(3);
  expect(calls[0].body).toMatchObject({ pageSize: 200 });
});

test("with more than 5,000 people it says it shows the first 5,000", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, (body) => {
    const n = Number(((body.pageToken as string | undefined) ?? "0") || 0);
    return {
      contacts: Array.from({ length: 200 }, (_, i) => person({ id: `p${n * 200 + i}`, fullName: `Person ${n * 200 + i}` })),
      nextPageToken: String(n + 1),
    };
  });
  await mockRpc(page, ContactsService.method.getContact, { contact: person({ id: "p0", fullName: "Person 0" }) });

  await page.goto("/people");

  await expect(page.getByText("Showing the first 5,000 people.")).toBeVisible();
  await expect(page.getByRole("button", { name: "All 5000" })).toBeVisible();
});

test("a person's page shows how to reach them, linked roles, notes and history in order", async ({ page }) => {
  const now = Date.now();
  await serve(page, [PRIYA, ARJUN], {
    c1: {
      history: [
        { id: "e3", kind: "note", text: "Asked for interview times next week", at: new Date(now - 5 * 3600_000) },
        { id: "e2", kind: "message_sent", at: new Date(now - 2 * 86400_000) },
        { id: "e1", kind: "created", at: new Date(now - 20 * 86400_000) },
      ],
      jobs: [{ id: "j1", title: "Platform Engineer", status: JobStatus.INTERVIEW }],
    },
  });

  await page.goto("/people");

  const detail = page.getByRole("article", { name: "Priya Raman" });
  await expect(detail).toContainText("Technical recruiter · Halden Labs");
  await expect(detail).toContainText("priya@haldenlabs.example");
  await expect(detail.getByTestId("last-contact")).toHaveText("today");
  await expect(detail).toContainText("Recruiter · Inbound, 6 Oct");
  const roles = detail.getByRole("region", { name: "Linked roles" });
  await expect(roles).toContainText("Platform Engineer");
  await expect(roles).toContainText("Interview");
  await expect(detail.getByRole("list", { name: "Notes and history" }).getByRole("listitem")).toHaveText([
    /5h ago.*Asked for interview times next week/,
    /2d ago.*Message sent/,
    /Added to People/,
  ]);
});

test("choosing another person opens theirs and puts them in the address", async ({ page }) => {
  const calls = await serve(page, [PRIYA, ARJUN]);
  await page.goto("/people");
  await expect(page.getByRole("article", { name: "Priya Raman" })).toBeVisible();

  await page.getByRole("button", { name: /Arjun Mehta/ }).click();

  await expect(page.getByRole("article", { name: "Arjun Mehta" })).toBeVisible();
  await expect(page).toHaveURL(/\/people\?person=c2$/);
  expect(calls.map((c) => (c.body as { id: string }).id)).toEqual(["c1", "c2"]);
  await page.goBack();
  await expect(page.getByRole("article", { name: "Priya Raman" })).toBeVisible();
});

test("a link to a person opens that person", async ({ page }) => {
  await serve(page, [PRIYA, ARJUN]);

  await page.goto("/people?person=c2");

  await expect(page.getByRole("article", { name: "Arjun Mehta" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Arjun Mehta/ })).toHaveAttribute("aria-current", "true");
});

test("on a phone the list comes first, then the person, and Back returns to the list", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await serve(page, [PRIYA, ARJUN]);

  await page.goto("/people");

  await expect(page.getByRole("list", { name: "People" })).toBeVisible();
  await expect(page.getByRole("article")).toHaveCount(0);

  await page.getByRole("button", { name: /Priya Raman/ }).click();

  await expect(page.getByRole("article", { name: "Priya Raman" })).toBeVisible();
  await expect(page.getByRole("list", { name: "People" })).toBeHidden();
  await page.getByRole("button", { name: "Back to people" }).click();
  await expect(page.getByRole("list", { name: "People" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("adding a note sends it and shows it; bad notes are explained", async ({ page }) => {
  const now = Date.now();
  let notes: { id: string; kind: string; text: string; at: Date }[] = [];
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [PRIYA] });
  await mockRpc(page, ContactsService.method.getContact, () => ({
    contact: PRIYA,
    history: notes.map((n) => ({ id: n.id, kind: n.kind, text: n.text, occurredAt: timestampFromDate(n.at) })),
  }));
  const added = await mockRpc(page, ContactsService.method.addContactNote, (body) => {
    notes = [{ id: "n1", kind: "note", text: body.text as string, at: new Date(now) }, ...notes];
    return { note: { id: "n1", kind: "note", text: body.text as string } };
  });
  await page.goto("/people");
  const add = page.getByRole("button", { name: "Add note" });
  await expect(add).toBeDisabled();

  await page.getByLabel("Add a note").fill("  Met at GopherCon  ");
  await add.click();

  await expect(page.getByRole("list", { name: "Notes and history" })).toContainText("Met at GopherCon");
  expect(added[0].body).toEqual({ id: "c1", text: "Met at GopherCon" });
  await expect(page.getByLabel("Add a note")).toHaveValue("");

  await mockRpcError(page, ContactsService.method.addContactNote, "invalid_argument", "x", "NOTE_TOO_LONG");
  await page.getByLabel("Add a note").fill("x");
  await add.click();
  await expect(page.getByRole("main").getByRole("alert")).toContainText("at most 2,000 characters");
});

test("Mark contacted waits for the Undo, and Undo sends nothing", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  const marks = await serve(page, [ARJUN]).then(async () => mockRpc(page, ContactsService.method.markContacted, { contact: ARJUN }));
  await page.goto("/people");

  await page.getByRole("button", { name: "Mark contacted" }).click();

  await expect(page.getByTestId("last-contact")).toHaveText("today");
  await expect(page.getByRole("status")).toContainText("Arjun Mehta marked as contacted");
  await page.getByRole("status").getByRole("button", { name: "Undo" }).click();
  await expect(page.getByTestId("last-contact")).toHaveText("1w ago");
  await page.clock.runFor(15_000);
  expect(marks).toHaveLength(0);
});

test("Mark contacted is sent once the Undo time has passed, with the version", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  const marks = await serve(page, [ARJUN]).then(async () => mockRpc(page, ContactsService.method.markContacted, { contact: ARJUN }));
  await page.goto("/people");

  await page.getByRole("button", { name: "Mark contacted" }).click();
  await page.clock.runFor(9_000);
  expect(marks).toHaveLength(0);
  await page.clock.runFor(2_000);

  await expect.poll(() => marks.length).toBe(1);
  expect(marks[0].body).toMatchObject({ id: "c2", version: 3, on: day(0) });
});

test("Mark contacted is sent at once when another person is chosen, never lost", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  const marks = await serve(page, [ARJUN, SARA]).then(async () => mockRpc(page, ContactsService.method.markContacted, { contact: ARJUN }));
  await page.goto("/people");
  await page.getByRole("button", { name: "Mark contacted" }).click();
  expect(marks).toHaveLength(0);

  await page.getByRole("button", { name: /Sara Lindqvist/ }).click();

  await expect.poll(() => marks.length).toBe(1);
  expect(marks[0].body).toMatchObject({ id: "c2" });
});

test("a Mark contacted that fails because the person changed says so", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  await serve(page, [ARJUN]);
  await mockRpcError(page, ContactsService.method.markContacted, "aborted", "x", "VERSION_CONFLICT");
  await page.goto("/people");

  await page.getByRole("button", { name: "Mark contacted" }).click();
  await page.clock.runFor(11_000);

  await expect(page.getByRole("main").getByRole("alert")).toContainText("changed elsewhere");
  await expect(page.getByTestId("last-contact")).toHaveText("1w ago");
});

test("Draft a note asks for a follow-up to this person and points to Inbox", async ({ page }) => {
  await serve(page, [PRIYA]);
  const drafts = await mockRpc(page, DraftsService.method.generateDraft, { draft: { id: "d1" } });
  await page.goto("/people");

  await page.getByRole("button", { name: "Draft a note" }).click();

  await expect(page.getByRole("status").filter({ hasText: "waiting in Inbox" }).getByRole("link", { name: "Inbox" })).toHaveAttribute("href", "/inbox");
  expect(drafts[0].body).toMatchObject({
    kind: "DRAFT_KIND_FOLLOW_UP", target: "DRAFT_TARGET_CONTACT", targetId: "c1", channel: "DRAFT_CHANNEL_EMAIL", recipient: "priya@haldenlabs.example",
  });
  expect(drafts[0].headers["idempotency-key"]).toBeTruthy();
});

test("Draft a note uses the person's preferred channel", async ({ page }) => {
  await serve(page, [person({ id: "c9", fullName: "Lee Park", preferredChannel: "linkedin" })]);
  const drafts = await mockRpc(page, DraftsService.method.generateDraft, { draft: { id: "d1" } });
  await page.goto("/people");

  await page.getByRole("button", { name: "Draft a note" }).click();

  await expect.poll(() => drafts.length).toBe(1);
  expect(drafts[0].body).toMatchObject({ channel: "DRAFT_CHANNEL_LINKEDIN" });
  expect((drafts[0].body as { recipient?: string }).recipient ?? "").toBe("");
});

test("Draft a note with no email and no channel makes no call and says what to add", async ({ page }) => {
  await serve(page, [TOM]);
  const drafts = await mockRpc(page, DraftsService.method.generateDraft, { draft: { id: "d1" } });
  await page.goto("/people");

  await page.getByRole("button", { name: "Draft a note" }).click();

  await expect(page.getByRole("main").getByRole("alert")).toContainText("Add an email or a channel first");
  expect(drafts).toHaveLength(0);
});

test("Draft a note without an AI provider says so and links to Settings", async ({ page }) => {
  await serve(page, [PRIYA]);
  await mockRpcError(page, DraftsService.method.generateDraft, "failed_precondition", "x", "AI_PROVIDER_NOT_CONFIGURED");
  await page.goto("/people");

  await page.getByRole("button", { name: "Draft a note" }).click();

  const alert = page.getByRole("main").getByRole("alert");
  await expect(alert).toContainText("Set up an AI provider to use AI features");
  await expect(alert.getByRole("link", { name: "Add it in Settings" })).toHaveAttribute("href", "/settings/provider");
});

test("Add person needs only a name, and sends the type, the idempotency key and a first note", async ({ page }) => {
  await serve(page, [PRIYA]);
  const adds = await mockRpc(page, ContactsService.method.addContact, { contact: person({ id: "c9", fullName: "Asha Nair" }) });
  const notes = await mockRpc(page, ContactsService.method.addContactNote, { note: { id: "n1", kind: "note" } });
  await page.goto("/people");
  await page.getByRole("button", { name: "Add person" }).first().click();
  const panel = page.getByRole("dialog", { name: "New person" });
  const submit = panel.getByRole("button", { name: "Add person" });
  await expect(submit).toBeDisabled();

  await panel.getByLabel("Name").fill("Asha Nair");
  await expect(submit).toBeEnabled();
  await panel.getByRole("button", { name: "Friend" }).click();
  await panel.getByLabel("How you know them").fill("College");
  await panel.getByLabel("First note").fill("Offered an intro");
  await submit.click();

  await expect(panel).toBeHidden();
  expect(adds).toHaveLength(1);
  expect(adds[0].body).toMatchObject({ fullName: "Asha Nair", relationship: "friend", howWeMet: "College" });
  expect(adds[0].headers["idempotency-key"]).toBeTruthy();
  expect(notes[0].body).toEqual({ id: "c9", text: "Offered an intro" });
  await expect(page).toHaveURL(/\/people\?person=c9$/);
});

test("Add person says when the person is already there and keeps the panel open", async ({ page }) => {
  await serve(page, [PRIYA]);
  await mockRpcError(page, ContactsService.method.addContact, "already_exists", "x", "CONTACT_DUPLICATE");
  await page.goto("/people");
  await page.getByRole("button", { name: "Add person" }).first().click();
  const panel = page.getByRole("dialog", { name: "New person" });

  await panel.getByLabel("Name").fill("Priya Raman");
  await panel.getByRole("button", { name: "Add person" }).click();

  await expect(panel.getByRole("alert")).toContainText("already in People");
  await expect(panel).toBeVisible();
});

test("Add person closes on Escape and gives focus back", async ({ page }) => {
  await serve(page, [PRIYA]);
  await page.goto("/people");
  const opener = page.getByRole("button", { name: "Add person" }).first();
  await opener.click();
  await expect(page.getByRole("dialog", { name: "New person" })).toBeVisible();

  await page.keyboard.press("Escape");

  await expect(page.getByRole("dialog", { name: "New person" })).toHaveCount(0);
  await expect(opener).toBeFocused();
});

test("the six types fit inside the Add person panel at any width", async ({ page }) => {
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await serve(page, [PRIYA]);
    await page.goto("/people");
    await page.getByRole("button", { name: "Add person" }).first().click();
    const panel = page.getByRole("dialog", { name: "New person" });

    await expect(panel.getByRole("button", { name: "Other" })).toBeVisible();

    const box = await panel.boundingBox();
    const last = await panel.getByRole("button", { name: "Other" }).boundingBox();
    expect(last!.x + last!.width, `at ${width}px`).toBeLessThanOrEqual(box!.x + box!.width);
    await page.keyboard.press("Escape");
  }
});
