import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { ContactsService } from "../src/gen/shogun/api/v1/contacts_pb";
import { DraftsService } from "../src/gen/shogun/api/v1/drafts_pb";
import { ProviderService } from "../src/gen/shogun/api/v1/providers_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const TEMPLATE_HEADER =
  "full_name,company,role,email,linkedin_url,x_handle,phone,relationship,how_we_met,status," +
  "preferred_channel,last_contacted,next_follow_up,target_role,job_link,tags,notes\n";
const TEMPLATE_CSV = `${TEMPLATE_HEADER}Asha Nair,Lumen,Staff Engineer,asha@example.com,,,,,,,,,,,,,\nBen Cole,Corvid,Recruiter,ben@corvid.example,,,,,,,,,,,,,\n`;
const LINKEDIN_CSV = "First Name,Last Name,Company,Position\nAsha,Nair,Lumen,Recruiter\nBen,Cole,Corvid,Engineer\nCy,Dean,Quarry,Designer\n";

const b64 = (text: string) => Buffer.from(text).toString("base64");

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", role: "member", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [{ id: "c1", fullName: "Priya Raman", relationship: "recruiter", version: 1 }] });
  await mockRpc(page, ContactsService.method.getContact, { contact: { id: "c1", fullName: "Priya Raman", version: 1 } });
  await mockRpc(page, DraftsService.method.listQueue, { drafts: [] });
}

async function withProvider(page: Page) {
  await mockRpc(page, ProviderService.method.getProvider, {
    provider: { baseUrl: "https://api.example.com/v1", model: "test-model", keyLast4: "abcd", version: 1 },
  });
}

function panel(page: Page) {
  return page.getByRole("dialog", { name: "Bulk import" });
}

async function openPanel(page: Page) {
  await page.getByRole("button", { name: "Import CSV" }).first().click();
  await expect(panel(page)).toBeVisible();
  await expect(panel(page).getByLabel("CSV file")).toBeEnabled();
}

async function chooseFile(page: Page, name: string, content: string | Buffer) {
  await openPanel(page);
  await panel(page).getByLabel("CSV file").setInputFiles({ name, mimeType: "text/csv", buffer: Buffer.from(content) });
}

interface Row {
  row: number;
  fullName: string;
  role?: string;
  company?: string;
  relationship?: string;
  duplicate?: boolean;
  repeatsRow?: number;
  problem?: string;
}

function rows(...list: Row[]) {
  return list.map((r) => ({
    role: "", company: "", email: "", relationship: "other", relationshipGuessed: true, duplicate: false,
    duplicateContactId: r.duplicate ? "c1" : "", duplicateReason: r.duplicate ? "email" : "", repeatsRow: 0, problem: "", ...r,
  }));
}

const AI_MAPPINGS = [
  { target: "full_name", sources: ["First Name", "Last Name"] },
  { target: "company", sources: ["Company"] },
  { target: "role", sources: ["Position"] },
];

const THREE = rows(
  { row: 2, fullName: "Asha Nair", role: "Recruiter", company: "Lumen", relationship: "recruiter" },
  { row: 3, fullName: "Ben Cole", role: "Engineer", company: "Corvid", relationship: "engineer", duplicate: true },
  { row: 4, fullName: "Cy Dean", role: "Designer", company: "Quarry" },
);

// aiDryRun answers the first read (AI) and any read with the user's mapping.
async function aiImport(page: Page, list = THREE, mappings = AI_MAPPINGS) {
  return mockRpc(page, ContactsService.method.importContacts, (body) => ({
    report: { rowsTotal: list.length, rowsCreated: list.length },
    mappings,
    dateOrder: mappings.length > 0 ? "dmy" : "",
    shapedCsv: mappings.length > 0 ? Buffer.from("shaped") : Buffer.alloc(0),
    rows: list,
    ...(body.dryRun ? {} : { importId: "import-1" }),
  }));
}

test("without an AI provider step 1 shows the template columns and no sample", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProviderService.method.getProvider, {});
  await page.goto("/people");

  await openPanel(page);

  const columns = panel(page).getByRole("list", { name: "Columns" }).getByRole("listitem");
  await expect(columns).toHaveCount(17);
  await expect(columns.first()).toHaveText("full_name");
  const template = panel(page).getByRole("link", { name: "Download template" });
  await expect(template).toHaveAttribute("download", "contacts-template.csv");
  expect(decodeURIComponent((await template.getAttribute("href")) ?? "")).toContain("full_name,company,role,email,");
  await expect(panel(page).getByRole("button", { name: "Use a sample LinkedIn export" })).toHaveCount(0);
  await expect(panel(page).getByRole("listitem", { name: /Choose file/ })).toHaveCount(0);
});

test("with a provider step 1 offers a sample and asks only for a name, not the template", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  await page.goto("/people");

  await openPanel(page);

  await expect(panel(page).getByRole("button", { name: "Use a sample LinkedIn export" })).toBeVisible();
  await expect(panel(page).getByRole("list", { name: "Columns" })).toHaveCount(0);
  await expect(panel(page).getByRole("link", { name: "Download template" })).toHaveCount(0);
});

test("a file in our columns is checked with no mapping table and nothing sent until the Undo time ends", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  await signIn(page);
  await mockRpc(page, ProviderService.method.getProvider, {});
  const calls = await mockRpc(page, ContactsService.method.importContacts, (body) => ({
    report: { rowsTotal: 2, rowsCreated: 2 },
    rows: body.dryRun ? rows({ row: 2, fullName: "Asha Nair", role: "Staff Engineer", company: "Lumen" }, { row: 3, fullName: "Ben Cole", role: "Recruiter", company: "Corvid", relationship: "recruiter" }) : [],
  }));
  await page.goto("/people");

  await chooseFile(page, "contacts-sept.csv", TEMPLATE_CSV);

  await expect(panel(page).getByRole("table", { name: "Matched columns" })).toHaveCount(0);
  await expect(panel(page).getByTestId("selected-count")).toHaveText("2 of 2 people selected");
  expect(calls).toHaveLength(1);
  expect(calls[0].body).toMatchObject({ filename: "contacts-sept.csv", dryRun: true });
  expect((calls[0].body as { reshape?: boolean }).reshape ?? false).toBe(false);

  await panel(page).getByRole("button", { name: "Import 2 people" }).click();

  await expect(panel(page)).toHaveCount(0);
  await expect(page.getByRole("status")).toContainText("Importing 2 people");
  await page.clock.runFor(9_000);
  expect(calls).toHaveLength(1);
  await page.clock.runFor(2_000);
  await expect.poll(() => calls.length).toBe(2);
  const sent = calls[1].body as { dryRun?: boolean; csv?: string; mappings?: unknown[]; selection?: { rows: number[] } };
  expect(sent.dryRun).toBeUndefined();
  expect(sent.csv).toBe(b64(TEMPLATE_CSV));
  expect(sent.mappings ?? []).toEqual([]);
  expect(sent.selection?.rows).toEqual([2, 3]);
  await expect(page.getByRole("status")).toContainText("2 people imported");
});

test("Undo sends nothing", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  await signIn(page);
  await mockRpc(page, ProviderService.method.getProvider, {});
  const calls = await aiImport(page, rows({ row: 2, fullName: "Asha Nair" }), []);
  await page.goto("/people");
  await chooseFile(page, "a.csv", TEMPLATE_CSV);
  await panel(page).getByRole("button", { name: "Import 1 person" }).click();

  await page.getByRole("status").getByRole("button", { name: "Undo" }).click();
  await page.clock.runFor(15_000);

  expect(calls).toHaveLength(1);
});

test("an import still waiting is sent at once when the screen is left", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  await signIn(page);
  await mockRpc(page, ProviderService.method.getProvider, {});
  const calls = await aiImport(page, rows({ row: 2, fullName: "Asha Nair" }), []);
  await page.goto("/people");
  await chooseFile(page, "a.csv", TEMPLATE_CSV);
  await panel(page).getByRole("button", { name: "Import 1 person" }).click();
  expect(calls).toHaveLength(1);

  await page.getByRole("navigation", { name: "Shogun" }).getByRole("link", { name: "Pipeline" }).click();

  await expect.poll(() => calls.length).toBe(2);
});

test("an import that fails says so and is not lost silently", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  await signIn(page);
  await mockRpc(page, ProviderService.method.getProvider, {});
  let first = true;
  await mockRpc(page, ContactsService.method.importContacts, (body) => ({ report: { rowsTotal: 1, rowsCreated: 1 }, rows: body.dryRun ? rows({ row: 2, fullName: "Asha Nair" }) : [] }));
  await page.goto("/people");
  await chooseFile(page, "a.csv", TEMPLATE_CSV);
  await panel(page).getByRole("button", { name: "Import 1 person" }).click();
  await mockRpcError(page, ContactsService.method.importContacts, "invalid_argument", "row 3 is not a row of this file", "INVALID_ROW_SELECTION");
  void first;
  first = false;

  await page.clock.runFor(11_000);

  await expect(page.getByRole("main").getByRole("alert")).toContainText("row 3 is not a row of this file");
  await expect(page.getByRole("status")).toContainText("did not finish");
});

test("a file over 3 MB is refused before it is sent", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProviderService.method.getProvider, {});
  const calls = await mockRpc(page, ContactsService.method.importContacts, { report: {} });
  await page.goto("/people");

  await chooseFile(page, "huge.csv", Buffer.alloc((3 << 20) + 1, "a"));

  await expect(panel(page).getByRole("alert")).toHaveText("That file is larger than 3 MB.");
  expect(calls).toHaveLength(0);
});

test("a file with no rows is refused in words", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProviderService.method.getProvider, {});
  const calls = await mockRpc(page, ContactsService.method.importContacts, { report: {} });
  await page.goto("/people");

  await chooseFile(page, "empty.csv", TEMPLATE_HEADER);

  await expect(panel(page).getByRole("alert")).toHaveText("That file has no rows to import.");
  expect(calls).toHaveLength(0);
});

test("a wrong header keeps the error inside the panel and shows the template", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, ProviderService.method.getProvider, {});
  const message = "header must be exactly: full_name,company,role,email,linkedin_url,x_handle,phone";
  await mockRpcError(page, ContactsService.method.importContacts, "invalid_argument", message, "INVALID_HEADER");
  await page.goto("/people");

  await chooseFile(page, "wrong.csv", "Name\nAsha\n");

  await expect(panel(page).getByRole("alert")).toHaveText(message);
  await expect(panel(page).getByRole("link", { name: "Download template" })).toBeVisible();
  expect(await panel(page).evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
});

test("a failed AI read says why and falls back to the template", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  await mockRpcError(page, ContactsService.method.importContacts, "failed_precondition", "no name", "IMPORT_NO_NAME_COLUMN");
  await page.goto("/people");

  await chooseFile(page, "companies.csv", "Company,Website\nLumen,lumen.example\n");

  await expect(panel(page).getByRole("alert")).toHaveText(
    "Your AI provider found no column with people's names in this sheet. Add one, or use the template below.",
  );
  await expect(panel(page).getByRole("link", { name: "Download template" })).toBeVisible();
});

test("the AI read shows its steps, then each column with where it goes and an example", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  let release: () => void = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route("**/api/shogun.api.v1.ContactsService/ImportContacts", async (route) => {
    await held;
    await route.fallback();
  });
  const calls = await aiImport(page);
  await page.goto("/people");

  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);

  const steps = panel(page).getByRole("list").filter({ hasText: "Reading Connections.csv" });
  await expect(steps.getByRole("listitem")).toHaveText([
    /Reading Connections.csv/,
    /Found 4 columns and 3 rows/,
    /Matching columns/,
    /Guessing who is a recruiter/,
  ]);
  release();

  const table = panel(page).getByRole("table", { name: "Matched columns" });
  await expect(table.getByRole("row", { name: /First Name/ })).toContainText("Asha");
  await expect(table.getByLabel("Field for First Name")).toHaveValue("full_name");
  await expect(table.getByLabel("Field for Last Name")).toHaveValue("full_name");
  await expect(table.getByLabel("Field for Company")).toHaveValue("company");
  await expect(table.getByLabel("Field for Position")).toHaveValue("role");
  await expect(panel(page).getByText("Matched by AI")).toBeVisible();
  expect(calls[0].body).toMatchObject({ dryRun: true, reshape: true });
});

test("people are tagged by what importing them would do, and only new people start ticked", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  await aiImport(page, rows(
    { row: 2, fullName: "Asha Nair", role: "Recruiter", relationship: "recruiter" },
    { row: 3, fullName: "Ben Cole", duplicate: true },
    { row: 4, fullName: "Ben C.", repeatsRow: 3 },
    { row: 5, fullName: "", problem: "name is required" },
  ));
  await page.goto("/people");

  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);

  const list = panel(page).getByRole("list", { name: "People in the file" });
  await expect(list.getByRole("listitem").nth(0)).toContainText("Recruiter");
  await expect(list.getByRole("listitem").nth(1)).toContainText("Already in People");
  await expect(list.getByRole("listitem").nth(2)).toContainText("Repeat of row 3");
  await expect(list.getByRole("listitem").nth(3)).toContainText("name is required");
  await expect(list.getByRole("checkbox").nth(0)).toBeChecked();
  await expect(list.getByRole("checkbox").nth(1)).not.toBeChecked();
  await expect(list.getByRole("checkbox").nth(2)).not.toBeChecked();
  await expect(list.getByRole("checkbox").nth(3)).toBeDisabled();
  await expect(panel(page).getByTestId("selected-count")).toHaveText("1 of 4 people selected");
});

test("ticking someone already in People says they will be updated, and the count follows", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  await aiImport(page);
  await page.goto("/people");
  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);
  const ben = panel(page).getByRole("listitem").filter({ hasText: "Ben Cole" });

  await ben.getByRole("checkbox").check();

  await expect(ben).toContainText("Will update their details");
  await expect(panel(page).getByTestId("selected-count")).toHaveText("3 of 3 people selected");
  await panel(page).getByRole("listitem").filter({ hasText: "Asha Nair" }).getByRole("checkbox").uncheck();
  await expect(panel(page).getByTestId("selected-count")).toHaveText("2 of 3 people selected");
  await expect(panel(page).getByRole("button", { name: "Import 2 people" })).toBeEnabled();
});

test("with nobody ticked Import is off", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  await aiImport(page, rows({ row: 2, fullName: "Ben Cole", duplicate: true }));
  await page.goto("/people");

  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);

  await expect(panel(page).getByRole("button", { name: "Import 0 people" })).toBeDisabled();
});

test("changing where a column goes reads the file again with that mapping and no AI", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  const calls = await mockRpc(page, ContactsService.method.importContacts, (body) => {
    const mapped = (body.mappings as { target: string }[] | undefined) ?? [];
    const positionSkipped = mapped.length > 0 && !mapped.some((m) => m.target === "role");
    return {
      report: { rowsTotal: 3, rowsCreated: 3 },
      mappings: AI_MAPPINGS,
      dateOrder: "dmy",
      rows: positionSkipped ? rows({ row: 2, fullName: "Asha Nair" }, { row: 3, fullName: "Ben Cole" }, { row: 4, fullName: "Cy Dean" }) : THREE,
    };
  });
  await page.goto("/people");
  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);
  await panel(page).getByRole("listitem").filter({ hasText: "Ben Cole" }).getByRole("checkbox").check();

  await panel(page).getByLabel("Field for Position").selectOption("");

  await expect(panel(page).getByRole("listitem").filter({ hasText: "Ben Cole" }).getByRole("checkbox")).toBeChecked();
  expect(calls).toHaveLength(2);
  expect(calls[1].body).toMatchObject({
    dryRun: true,
    dateOrder: "dmy",
    mappings: [
      { target: "full_name", sources: ["First Name", "Last Name"] },
      { target: "company", sources: ["Company"] },
    ],
  });
  expect((calls[1].body as { reshape?: boolean }).reshape ?? false).toBe(false);
  await expect(panel(page).getByTestId("selected-count")).toHaveText("3 of 3 people selected");
});

test("with no column for the name Import is off and nothing is read again", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  const calls = await aiImport(page);
  await page.goto("/people");
  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);

  await panel(page).getByLabel("Field for First Name").selectOption("");
  await panel(page).getByLabel("Field for Last Name").selectOption("");

  await expect(panel(page).getByText("Pick the column that holds the name.")).toBeVisible();
  await expect(panel(page).getByRole("list", { name: "People in the file" })).toHaveCount(0);
  await expect(panel(page).getByRole("button", { name: /^Import / })).toBeDisabled();
  // Skipping First Name still left Last Name as the name, so that one change was read
  // again; skipping Last Name too left no name, so nothing more was sent.
  expect(calls.filter((c) => (c.body as { mappings?: unknown[] }).mappings?.length)).toHaveLength(1);
});

test("the import sends the original file, the mapping the owner saw, the date order and the ticked rows", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  await signIn(page);
  await withProvider(page);
  const calls = await aiImport(page);
  await page.goto("/people");
  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);
  await panel(page).getByRole("listitem").filter({ hasText: "Cy Dean" }).getByRole("checkbox").uncheck();

  await panel(page).getByRole("button", { name: "Import 1 person" }).click();
  await page.clock.runFor(11_000);

  await expect.poll(() => calls.length).toBe(2);
  const sent = calls[1].body as { dryRun?: boolean; reshape?: boolean; csv: string; mappings: unknown[]; dateOrder: string; selection: { rows: number[] } };
  expect(sent.dryRun).toBeUndefined();
  expect(sent.reshape).toBeUndefined();
  expect(sent.csv).toBe(b64(LINKEDIN_CSV));
  expect(sent.mappings).toEqual(AI_MAPPINGS);
  expect(sent.dateOrder).toBe("dmy");
  expect(sent.selection.rows).toEqual([2]);
});

test("rows past the preview are imported without a check, and the panel says so", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  await signIn(page);
  await withProvider(page);
  const first = Array.from({ length: 1000 }, (_, i) => ({ row: i + 2, fullName: `Person ${i}` }));
  const calls = await mockRpc(page, ContactsService.method.importContacts, {
    report: { rowsTotal: 1500, rowsCreated: 1500 },
    mappings: AI_MAPPINGS,
    rows: rows(...first),
    rowsTruncated: true,
  });
  await page.goto("/people");
  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);

  await expect(panel(page).getByText("Showing the first 1,000 of 1,500. The rest are imported without a check.")).toBeVisible();
  await expect(panel(page).getByTestId("selected-count")).toHaveText("1500 of 1500 people selected");
  await panel(page).getByRole("listitem").filter({ hasText: "Person 0" }).getByRole("checkbox").uncheck();
  await panel(page).getByRole("button", { name: "Import 1499 people" }).click();
  await page.clock.runFor(11_000);

  await expect.poll(() => calls.length).toBe(2);
  const rowsSent = (calls[1].body as { selection: { rows: number[] } }).selection.rows;
  expect(rowsSent).toHaveLength(1499);
  expect(rowsSent).not.toContain(2);
  expect(rowsSent.at(-1)).toBe(1501);
});

test("Choose another file goes back to step 1 with nothing sent", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  const calls = await aiImport(page);
  await page.goto("/people");
  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);

  await panel(page).getByRole("button", { name: "Choose another file" }).click();

  await expect(panel(page).getByLabel("CSV file")).toBeVisible();
  expect(calls).toHaveLength(1);
});

test("the sample LinkedIn export is read with the AI", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  const calls = await aiImport(page);
  await page.goto("/people");
  await openPanel(page);

  await panel(page).getByRole("button", { name: "Use a sample LinkedIn export" }).click();

  await expect(panel(page).getByRole("table", { name: "Matched columns" })).toBeVisible();
  expect(calls[0].body).toMatchObject({ filename: "Connections.csv", dryRun: true, reshape: true });
  expect(Buffer.from((calls[0].body as { csv: string }).csv, "base64").toString()).toContain("First Name,Last Name,URL,Email Address");
});

test("cancelling the panel saves nothing and gives focus back", async ({ page }) => {
  await signIn(page);
  await withProvider(page);
  const calls = await aiImport(page);
  await page.goto("/people");
  const opener = page.getByRole("button", { name: "Import CSV" }).first();
  await opener.click();
  await panel(page).getByLabel("CSV file").setInputFiles({ name: "a.csv", mimeType: "text/csv", buffer: Buffer.from(LINKEDIN_CSV) });
  await expect(panel(page).getByRole("table", { name: "Matched columns" })).toBeVisible();

  await page.keyboard.press("Escape");

  await expect(panel(page)).toHaveCount(0);
  await expect(opener).toBeFocused();
  expect(calls).toHaveLength(1);
});

test("on a phone the panel fits and the mapping table does not push it sideways", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await signIn(page);
  await withProvider(page);
  await aiImport(page);
  await page.goto("/people");
  await chooseFile(page, "Connections.csv", LINKEDIN_CSV);

  await expect(panel(page).getByRole("table", { name: "Matched columns" })).toBeVisible();

  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(await panel(page).evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
});
