import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { ProviderFailure, ProviderService } from "../src/gen/shogun/api/v1/providers_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const KEY = "sk-live-0123456789-SECRET";

const UPDATED = timestampFromDate(new Date("2026-10-03T00:00:00Z"));

function stored(over: Record<string, unknown> = {}) {
  return {
    baseUrl: "https://llm.example.com",
    model: "my-llama",
    keyLast4: "CRET",
    version: 3,
    updatedAt: UPDATED,
    ...over,
  };
}

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      role: "member",
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  });
}

// serveProvider answers getProvider from a state the test can change, so a save,
// a removal and the refetch after them read like the real thing.
async function serveProvider(page: Page, initial: ReturnType<typeof stored> | undefined, tokens = BigInt(0)) {
  const state = { provider: initial, tokens };
  await mockRpc(page, ProviderService.method.getProvider, () => ({
    provider: state.provider,
    tokensThisMonth: state.tokens,
  }));
  return state;
}

test("with no provider the page says AI features are off and asks for the address, model and key", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);

  await page.goto("/settings/provider");

  await expect(page.getByTestId("provider-state")).toContainText("AI features are off");
  await expect(page.getByLabel("Address")).toHaveValue("");
  await expect(page.getByLabel("API key")).toHaveAttribute("type", "password");
  await expect(page.getByRole("button", { name: "Test connection" })).toHaveCount(0);
});

test("saving creates the provider, then shows it without the key", async ({ page }) => {
  await signIn(page);
  const state = await serveProvider(page, undefined);
  const saves = await mockRpc(page, ProviderService.method.setProvider, (body) => {
    state.provider = stored({ baseUrl: body.baseUrl as string, model: body.model as string, version: 1 });
    return { provider: state.provider };
  });
  await page.goto("/settings/provider");

  await page.getByLabel("Address").fill("https://llm.example.com");
  await page.getByLabel("Model").fill("my-llama");
  await page.getByLabel("API key").fill(KEY);
  await page.getByRole("button", { name: "Save" }).click();

  await expect.poll(() => saves.length).toBe(1);
  expect(saves[0].body).toMatchObject({ baseUrl: "https://llm.example.com", model: "my-llama", apiKey: KEY });
  // Version 0 means create; proto3 JSON leaves a zero out.
  expect((saves[0].body as Record<string, unknown>).version ?? 0).toBe(0);
  await expect(page.getByTestId("provider-state")).toContainText("AI features are on");
  await expect(page.getByTestId("provider-state")).toContainText("my-llama");
  await expect(page.getByLabel("API key")).toHaveValue("");
  await expect(page.locator("body")).not.toContainText(KEY);
});

test("a saved provider shows its model, address, last four and tokens against the limit", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored({ monthlyTokenCap: BigInt(500000) }), BigInt(12345));

  await page.goto("/settings/provider");

  await expect(page.getByTestId("provider-state")).toContainText("my-llama at https://llm.example.com");
  await expect(page.getByTestId("provider-state")).toContainText("CRET");
  await expect(page.getByText("12,345 tokens used this month of 500,000.")).toBeVisible();
  await expect(page.getByLabel("Address")).toHaveValue("https://llm.example.com");
  await expect(page.getByLabel("Monthly token limit")).toHaveValue("500000");
  await expect(page.getByLabel("API key")).toHaveAttribute("placeholder", "Keep the key ending CRET");
});

test("updating without typing a key sends none, so the stored key is kept", async ({ page }) => {
  await signIn(page);
  const state = await serveProvider(page, stored());
  const saves = await mockRpc(page, ProviderService.method.setProvider, (body) => {
    state.provider = stored({ model: body.model as string, version: 4 });
    return { provider: state.provider };
  });
  await page.goto("/settings/provider");

  await page.getByLabel("Model").fill("my-llama-2");
  await page.getByRole("button", { name: "Save" }).click();

  await expect.poll(() => saves.length).toBe(1);
  expect(saves[0].body).toMatchObject({ model: "my-llama-2", version: 3 });
  expect((saves[0].body as Record<string, unknown>).apiKey ?? "").toBe("");
});

test("prices are sent as micro-dollars and the limit as a number", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  const saves = await mockRpc(page, ProviderService.method.setProvider, () => ({ provider: stored({ version: 4 }) }));
  await page.goto("/settings/provider");

  await page.getByLabel("Input price ($ per million tokens)").fill("3");
  await page.getByLabel("Output price ($ per million tokens)").fill("15.5");
  await page.getByLabel("Monthly token limit").fill("250000");
  await page.getByRole("button", { name: "Save" }).click();

  await expect.poll(() => saves.length).toBe(1);
  expect(saves[0].body).toMatchObject({ inputMicrosPerMtok: "3000000", outputMicrosPerMtok: "15500000", monthlyTokenCap: "250000" });
});

test("a half-filled price or a bad limit cannot be saved", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  const saves = await mockRpc(page, ProviderService.method.setProvider, () => ({ provider: stored() }));
  await page.goto("/settings/provider");

  await page.getByLabel("Input price ($ per million tokens)").fill("3");
  await expect(page.getByRole("main").getByRole("alert")).toContainText("Enter both prices");
  await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();

  await page.getByLabel("Output price ($ per million tokens)").fill("9");
  await page.getByLabel("Monthly token limit").fill("0");
  await expect(page.getByRole("main").getByRole("alert")).toContainText("whole number above 0");
  await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
  expect(saves).toHaveLength(0);
});

test("each reason the server gives is explained in words the user can act on", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);
  await mockRpcError(page, ProviderService.method.setProvider, "invalid_argument", "x", "INVALID_PROVIDER_URL");
  await page.goto("/settings/provider");

  await page.getByLabel("Address").fill("http://10.0.0.1");
  await page.getByLabel("Model").fill("m");
  await page.getByLabel("API key").fill(KEY);
  await page.getByRole("button", { name: "Save" }).click();

  await expect(page.getByRole("main").getByRole("alert")).toContainText("public https address");
  await expect(page.locator("body")).not.toContainText(KEY);
});

test("a provider changed elsewhere says so", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  await mockRpcError(page, ProviderService.method.setProvider, "aborted", "x", "VERSION_CONFLICT");
  await page.goto("/settings/provider");

  await page.getByLabel("Model").fill("other");
  await page.getByRole("button", { name: "Save" }).click();

  await expect(page.getByRole("main").getByRole("alert")).toContainText("changed elsewhere");
});

test("test connection reports a provider that works", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  await mockRpc(page, ProviderService.method.testProvider, { ok: true, model: "my-llama" });
  await page.goto("/settings/provider");

  await page.getByRole("button", { name: "Test connection" }).click();

  await expect(page.getByRole("status")).toContainText("It works. my-llama answered.");
});

test("test connection says why a provider that was reached did not work", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  await mockRpc(page, ProviderService.method.testProvider, { ok: false, failure: ProviderFailure.REJECTED });
  await page.goto("/settings/provider");

  await page.getByRole("button", { name: "Test connection" }).click();

  await expect(page.getByRole("main").getByRole("alert")).toContainText("The provider refused the key");
});

test("test connection explains a spent budget", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  await mockRpcError(page, ProviderService.method.testProvider, "resource_exhausted", "x", "BUDGET_EXHAUSTED");
  await page.goto("/settings/provider");

  await page.getByRole("button", { name: "Test connection" }).click();

  await expect(page.getByRole("main").getByRole("alert")).toContainText("budget or token limit is used up");
});

test("removing needs a second click and then switches AI features off", async ({ page }) => {
  await signIn(page);
  const state = await serveProvider(page, stored());
  const removals = await mockRpc(page, ProviderService.method.deleteProvider, () => {
    state.provider = undefined;
    return {};
  });
  await page.goto("/settings/provider");

  await page.getByRole("button", { name: "Remove" }).click();
  expect(removals).toHaveLength(0);
  await page.getByRole("button", { name: "Yes, remove it" }).click();

  await expect.poll(() => removals.length).toBe(1);
  await expect(page.getByTestId("provider-state")).toContainText("AI features are off");
});

test("backing out of a removal changes nothing", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  const removals = await mockRpc(page, ProviderService.method.deleteProvider, () => ({}));
  await page.goto("/settings/provider");

  await page.getByRole("button", { name: "Remove" }).click();
  await page.getByRole("button", { name: "Keep it" }).click();

  await expect(page.getByRole("button", { name: "Remove" })).toBeVisible();
  expect(removals).toHaveLength(0);
});

test("a failed load offers a retry", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, ProviderService.method.getProvider, "unavailable", "down");

  await page.goto("/settings/provider");

  await expect(page.getByRole("main").getByRole("alert")).toContainText("Couldn't load your provider");
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});

test("the AI provider tab is in Settings and the notice on an AI page links to it", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);
  await mockRpc(page, ProviderService.method.testProvider, { ok: true });

  await page.goto("/discovery");
  const notice = page.getByTestId("ai-provider-notice");

  await expect(notice).toContainText("AI features are off until you add your AI provider");
  await notice.getByRole("link", { name: "Add it in Settings" }).click();
  await expect(page).toHaveURL(/\/settings\/provider$/);
  await expect(page.getByRole("navigation", { name: "Settings" }).getByRole("link", { name: "AI provider" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("the notice is gone once a provider is set", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());

  await page.goto("/discovery");

  await expect(page.getByRole("heading", { name: "What's out there." })).toBeVisible();
  await expect(page.getByTestId("ai-provider-notice")).toHaveCount(0);
});

test("the notice stays out of the way when the provider cannot be read", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, ProviderService.method.getProvider, "unavailable", "down");

  await page.goto("/discovery");

  await expect(page.getByRole("heading", { name: "What's out there." })).toBeVisible();
  await expect(page.getByTestId("ai-provider-notice")).toHaveCount(0);
});
