import { expect, type Page } from "@playwright/test";

// The stand-in for an Anthropic-compatible provider, as the services see it on
// the compose network (see stubs/mock-apis.mjs). The stack runs in local mode,
// which is what allows an address that is not public.
const MOCK_PROVIDER_URL = "http://mock-apis:8090";
const MOCK_MODEL = "e2e-model";
const MOCK_KEY = "e2e-key-0123456789";

// addProvider gives the signed-in user the stub as their AI provider, the way a
// real user would, through Settings. It works whether or not one is set already.
export async function addProvider(page: Page): Promise<void> {
  await page.goto("/settings/provider");
  await page.getByLabel("Address").fill(MOCK_PROVIDER_URL);
  await page.getByLabel("Model").fill(MOCK_MODEL);
  await page.getByLabel("API key").fill(MOCK_KEY);
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByTestId("provider-state")).toContainText("AI features are on");
}
