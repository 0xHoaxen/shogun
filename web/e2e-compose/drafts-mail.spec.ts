import { expect, test, type Page } from "@playwright/test";

const OWNER_EMAIL = process.env.TORII_E2E_EMAIL ?? "owner@example.com";
const STUB = process.env.MOCK_APIS_URL ?? "http://127.0.0.1:8091";
const RECIPIENT = "jobs@lumen.example";
const EDITED_BODY = "Hello Lumen, this is the text the owner approved.";

interface SentMail {
  headers: Record<string, string>;
  body: string;
}

async function sentMail(): Promise<SentMail[]> {
  const res = await fetch(`${STUB}/_e2e/sent`);
  return (await res.json()) as SentMail[];
}

// Google's consent screen is replaced: the app follows only https, so tsubame's
// auth address is https on a host that does not exist, and the browser is sent
// straight back to the callback with the state it was given.
async function grantGmailAccess(page: Page): Promise<void> {
  await page.route("https://mock-gmail.test/**", (route) => {
    const state = new URL(route.request().url()).searchParams.get("state") ?? "";
    const back = `http://localhost:3000/mail/callback?code=e2e-code&state=${encodeURIComponent(state)}`;
    return route.fulfill({ status: 302, headers: { location: back } });
  });
  await page.goto("/settings/mail");
  await page.getByRole("button", { name: "Connect Gmail" }).click();
  await expect(page.getByRole("status")).toContainText("Connected");
}

test("connect Gmail, write a draft, regenerate and edit it, approve it, and exactly one email goes out", async ({ page }) => {
  await fetch(`${STUB}/_e2e/sent`, { method: "DELETE" });

  // Act: sign in through the stub identity provider.
  await page.goto("/login");
  await page.getByRole("link", { name: "Sign in with Google" }).click();
  await expect(page.getByTestId("owner-email")).toHaveText(OWNER_EMAIL);

  // Act and assert: connect Gmail.
  await grantGmailAccess(page);

  // Act: ask for an email draft.
  await page.goto("/drafts");
  await page.getByRole("button", { name: "New draft" }).click();
  await page.getByLabel("Channel").selectOption({ label: "Email" });
  await page.getByLabel("To").fill(RECIPIENT);
  await page.getByLabel("What should it say?").fill("A short note about the role");
  await page.getByRole("button", { name: "Write draft" }).click();

  // Assert: the stub model's first version arrives.
  await expect(page).toHaveURL(/\/drafts\/[0-9a-f-]{36}$/);
  const draftId = new URL(page.url()).pathname.split("/").pop();
  const preview = page.getByTestId("send-preview");
  await expect(preview).toContainText(RECIPIENT, { timeout: 30_000 });
  await expect(preview).toContainText("generated text number 1");

  // Act: regenerate with a note.
  await page.getByLabel("Anything to change or keep in mind?").fill("shorter");
  await page.getByRole("button", { name: "Regenerate" }).click();

  // Assert: a second version appears by itself.
  await expect(preview).toContainText("generated text number 2", { timeout: 30_000 });

  // Act: edit it, which saves a third version.
  await page.getByLabel("Body").fill(EDITED_BODY);
  await page.getByRole("button", { name: "Save as new version" }).click();
  await expect(page.getByRole("button", { name: /v3/ })).toContainText("current");
  await expect(preview).toContainText(EDITED_BODY);

  // Act: approve it.
  await page.getByRole("button", { name: "Approve and send" }).click();

  // Assert: tsubame sent it once, carrying the draft and version it was approved for.
  await expect.poll(async () => (await sentMail()).length, { timeout: 30_000 }).toBe(1);
  const [mail] = await sentMail();
  expect(mail.headers["x-shogun-draft"]).toBe(`${draftId}:3`);
  expect(mail.headers["to"]).toContain(RECIPIENT);
  expect(mail.body).toContain(EDITED_BODY);

  // Assert: fude followed tsubame's outcome rather than deciding it.
  await expect(page.getByRole("status").filter({ hasText: "Sent." })).toBeVisible({ timeout: 30_000 });
  expect(await sentMail()).toHaveLength(1);
});
