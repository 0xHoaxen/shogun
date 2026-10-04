import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { ContactStatus, ContactsService } from "../src/gen/shogun/api/v1/contacts_pb";
import { mockRpc } from "./fixtures";

interface ContactInit {
  id: string;
  fullName: string;
  role: string;
  companyName: string;
  status: ContactStatus;
  version: number;
  nextFollowUp?: string;
}

const PRIYA: ContactInit = {
  id: "c1",
  fullName: "Priya Raman",
  role: "Staff Engineer",
  companyName: "Lumen",
  status: ContactStatus.REPLIED,
  version: 2,
  nextFollowUp: "2026-10-08",
};

const KENJI: ContactInit = {
  id: "c2",
  fullName: "Kenji Watanabe",
  role: "Tech Lead",
  companyName: "Northwind",
  status: ContactStatus.NOT_REACHED,
  version: 1,
};

const CSV = "full_name,email\nAsha Nair,asha@example.com\n";

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  });
}

function importDialog(page: Page) {
  return page.getByRole("dialog", { name: "Import CSV" });
}

async function chooseFile(page: Page, name: string, content: string | Buffer) {
  await page.getByRole("button", { name: "Import CSV" }).click();
  await importDialog(page).getByLabel("CSV file").setInputFiles({
    name,
    mimeType: "text/csv",
    buffer: Buffer.from(content),
  });
}

test("the table lists contacts with their status and next step", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [PRIYA, KENJI] });

  // Act
  await page.goto("/contacts");

  // Assert
  const row = page.getByRole("row", { name: /Priya Raman/ });
  await expect(row).toContainText("Staff Engineer");
  await expect(row).toContainText("Lumen");
  await expect(row).toContainText("Replied");
  await expect(row).toContainText("Follow up 2026-10-08");
  await expect(page.getByRole("row", { name: /Kenji Watanabe/ })).toContainText("Not reached");
});

test("a status filter asks for that status and shows only its people", async ({ page }) => {
  // Arrange
  await signIn(page);
  const calls = await mockRpc(page, ContactsService.method.listContacts, (body) => ({
    contacts: body.status === "CONTACT_STATUS_REPLIED" ? [PRIYA] : [PRIYA, KENJI],
  }));
  await page.goto("/contacts");
  await expect(page.getByRole("row", { name: /Kenji Watanabe/ })).toBeVisible();

  // Act
  await page.getByRole("button", { name: "Replied" }).click();

  // Assert
  await expect(page.getByRole("row", { name: /Kenji Watanabe/ })).toBeHidden();
  await expect(page.getByRole("row", { name: /Priya Raman/ })).toBeVisible();
  expect(calls.map((call) => (call.body as { status?: string }).status)).toContain(
    "CONTACT_STATUS_REPLIED",
  );
});

test("an empty status says nobody is there", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [] });

  // Act
  await page.goto("/contacts");

  // Assert
  await expect(page.getByText("Nobody in this status yet.")).toBeVisible();
});

test("adding a contact sends the form with an idempotency key and shows the row", async ({ page }) => {
  // Arrange
  await signIn(page);
  let current: ContactInit[] = [PRIYA];
  await mockRpc(page, ContactsService.method.listContacts, () => ({ contacts: current }));
  const addCalls = await mockRpc(page, ContactsService.method.addContact, () => {
    const added: ContactInit = { ...KENJI, id: "c3", fullName: "Asha Nair", companyName: "Quayside" };
    current = [PRIYA, added];
    return { contact: added };
  });
  await page.goto("/contacts");

  // Act
  await page.getByRole("button", { name: "Add contact" }).click();
  const dialog = page.getByRole("dialog", { name: "Add contact" });
  await dialog.getByLabel("Name").fill("Asha Nair");
  await dialog.getByLabel("Company").fill("Quayside");
  await dialog.getByRole("button", { name: "Add contact" }).click();

  // Assert
  await expect(page.getByRole("row", { name: /Asha Nair/ })).toContainText("Quayside");
  await expect(dialog).toBeHidden();
  expect(addCalls).toHaveLength(1);
  expect(addCalls[0].body).toMatchObject({ fullName: "Asha Nair", companyName: "Quayside" });
  expect(addCalls[0].headers["idempotency-key"]).toBeTruthy();
});

test("a good CSV is previewed as a dry run and saved only after confirming", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [PRIYA] });
  const importCalls = await mockRpc(page, ContactsService.method.importContacts, (body) =>
    body.dryRun
      ? { report: { rowsTotal: 3, rowsCreated: 2, rowsUpdated: 1 } }
      : { report: { rowsTotal: 3, rowsCreated: 2, rowsUpdated: 1 }, importId: "import-1" },
  );
  await page.goto("/contacts");

  // Act
  await chooseFile(page, "contacts-sept.csv", CSV);

  // Assert
  const dialog = importDialog(page);
  await expect(dialog).toContainText(
    "contacts-sept.csv has 3 rows. 2 are new, 1 update existing people, 0 are skipped.",
  );
  expect(importCalls).toHaveLength(1);
  expect(importCalls[0].body).toMatchObject({ filename: "contacts-sept.csv", dryRun: true });

  // Act
  await dialog.getByRole("button", { name: "Import 3 rows" }).click();

  // Assert
  await expect(dialog).toContainText("Imported 2 new and updated 1.");
  expect(importCalls).toHaveLength(2);
  const confirmBody = importCalls[1].body as { dryRun?: boolean; csv?: string };
  expect(confirmBody.dryRun).toBeUndefined();
  expect(confirmBody.csv).toBe(Buffer.from(CSV).toString("base64"));
});

test("a CSV with bad rows lists them and still imports the good ones", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [] });
  await mockRpc(page, ContactsService.method.importContacts, {
    report: {
      rowsTotal: 3,
      rowsCreated: 1,
      rowsUpdated: 1,
      rowsFailed: 1,
      errors: [{ row: 4, column: "full_name", message: "name is required" }],
    },
  });
  await page.goto("/contacts");

  // Act
  await chooseFile(page, "mixed.csv", CSV);

  // Assert
  const dialog = importDialog(page);
  const problem = dialog.getByRole("row", { name: /name is required/ });
  await expect(problem).toContainText("4");
  await expect(problem).toContainText("full_name");
  await expect(dialog.getByRole("button", { name: "Import 2 rows" })).toBeEnabled();
});

test("a CSV with nothing importable cannot be confirmed", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [] });
  await mockRpc(page, ContactsService.method.importContacts, {
    report: {
      rowsTotal: 1,
      rowsFailed: 1,
      errors: [{ row: 2, column: "email", message: "email is not valid" }],
    },
  });
  await page.goto("/contacts");

  // Act
  await chooseFile(page, "bad.csv", CSV);

  // Assert
  const dialog = importDialog(page);
  await expect(dialog).toContainText("email is not valid");
  await expect(dialog.getByRole("button", { name: "Import 0 rows" })).toBeDisabled();
});

test("cancelling the preview saves nothing", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [] });
  const importCalls = await mockRpc(page, ContactsService.method.importContacts, {
    report: { rowsTotal: 1, rowsCreated: 1 },
  });
  await page.goto("/contacts");
  await chooseFile(page, "contacts.csv", CSV);
  await expect(importDialog(page)).toContainText("contacts.csv has 1 row.");

  // Act
  await importDialog(page).getByRole("button", { name: "Cancel" }).click();

  // Assert
  await expect(importDialog(page)).toBeHidden();
  expect(importCalls).toHaveLength(1);
  expect((importCalls[0].body as { dryRun?: boolean }).dryRun).toBe(true);
});

test("a file over 3 MB is refused before it is sent", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, ContactsService.method.listContacts, { contacts: [] });
  const importCalls = await mockRpc(page, ContactsService.method.importContacts, { report: {} });
  await page.goto("/contacts");

  // Act
  await chooseFile(page, "huge.csv", Buffer.alloc((3 << 20) + 1, "a"));

  // Assert
  await expect(importDialog(page).getByRole("alert")).toHaveText("That file is larger than 3 MB.");
  expect(importCalls).toHaveLength(0);
});
