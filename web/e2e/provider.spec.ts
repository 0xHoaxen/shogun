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
// A typed address, model and key, as the tests below fill them in.
const TYPED = { address: "https://llm.example.com/v1", model: "my-llama", key: KEY };

async function fill(page: Page, over: Partial<typeof TYPED> = {}) {
  const v = { ...TYPED, ...over };
  await page.getByLabel("Base URL").fill(v.address);
  await page.getByLabel("Model name").fill(v.model);
  await page.getByLabel("API key", { exact: true }).fill(v.key);
}

const passes = { ok: true, model: "my-llama", latencyMs: BigInt(390) };

async function serveProvider(page: Page, initial: ReturnType<typeof stored> | undefined, tokens = BigInt(0)) {
  const state = { provider: initial, tokens };
  await mockRpc(page, ProviderService.method.getProvider, () => ({
    provider: state.provider,
    tokensThisMonth: state.tokens,
  }));
  return state;
}

test("with no provider the page says AI features are off, and Test and Save wait for the details", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);

  await page.goto("/settings/provider");

  await expect(page.getByTestId("provider-state")).toContainText("AI features are off");
  await expect(page.getByTestId("provider-state")).toContainText("Set up an AI provider");
  await expect(page.getByLabel("Base URL")).toHaveValue("");
  await expect(page.getByLabel("API key", { exact: true })).toHaveAttribute("type", "password");
  await expect(page.getByRole("button", { name: "Test connection" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await page.getByLabel("Base URL").fill(TYPED.address);
  await page.getByLabel("Model name").fill(TYPED.model);
  await expect(page.getByRole("button", { name: "Test connection" })).toBeDisabled();
  await page.getByLabel("API key", { exact: true }).fill(KEY);
  await expect(page.getByRole("button", { name: "Test connection" })).toBeEnabled();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
});

test("Show and Hide flip the key field and say which is on", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);
  await page.goto("/settings/provider");
  const key = page.getByLabel("API key", { exact: true });
  const toggle = page.getByRole("button", { name: "Show" });

  await expect(toggle).toHaveAttribute("aria-pressed", "false");
  await toggle.click();

  await expect(key).toHaveAttribute("type", "text");
  await expect(page.getByRole("button", { name: "Hide" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Hide" }).click();
  await expect(key).toHaveAttribute("type", "password");
});

test("Test connection sends exactly what was typed and shows how fast the model replied", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);
  const tests = await mockRpc(page, ProviderService.method.testProvider, passes);
  await page.goto("/settings/provider");
  await fill(page, { address: "  https://llm.example.com/v1 ", model: " my-llama " });

  await page.getByRole("button", { name: "Test connection" }).click();

  await expect(page.getByTestId("test-result")).toContainText("Connected. my-llama replied in 390 ms.");
  expect(tests).toHaveLength(1);
  expect(tests[0].body).toEqual({ baseUrl: "https://llm.example.com/v1", model: "my-llama", apiKey: KEY });
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
  await expect(page.locator("body")).not.toContainText(KEY);
});

test("each way a test can fail is explained, and nothing can be saved", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);
  const saves = await mockRpc(page, ProviderService.method.setProvider, () => ({ provider: stored() }));
  let failure = ProviderFailure.REJECTED;
  await mockRpc(page, ProviderService.method.testProvider, () => ({ ok: false, failure }));
  await page.goto("/settings/provider");
  await fill(page);

  for (const [kind, words] of [
    [ProviderFailure.REJECTED, "refused the key"],
    [ProviderFailure.RATE_LIMITED, "going too fast"],
    [ProviderFailure.NOT_FOUND, "does not know this model"],
    [ProviderFailure.UNREACHABLE, "No answer came"],
    [ProviderFailure.NOT_PUBLIC, "private network"],
    [ProviderFailure.PROVIDER_ERROR, "error on its side"],
    [ProviderFailure.ENDPOINT_IN_URL, "already ends in /chat/completions"],
    [ProviderFailure.INVALID_ADDRESS, "https address"],
    [ProviderFailure.OTHER, "could not name"],
  ] as const) {
    failure = kind;
    await page.getByRole("button", { name: "Test connection" }).click();
    await expect(page.getByTestId("test-result")).toContainText(words);
    await expect(page.getByTestId("test-result")).toHaveAttribute("data-tone", "bad");
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  }
  expect(saves).toHaveLength(0);
});

test("any edit after a passing test turns Save off again, even typing the old value back", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);
  await mockRpc(page, ProviderService.method.testProvider, passes);
  await page.goto("/settings/provider");
  await fill(page);
  await page.getByRole("button", { name: "Test connection" }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();

  for (const [label, value, back] of [
    ["Base URL", "https://other.example.com/v1", TYPED.address],
    ["Model name", "another", TYPED.model],
  ] as const) {
    await page.getByLabel(label).fill(value);
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    await expect(page.getByTestId("test-result")).toHaveCount(0);
    await page.getByLabel(label).fill(back);
    // Typing the tested value back brings the result back: it is the same details.
    await expect(page.getByTestId("test-result")).toContainText("Connected");
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
  }

  await page.getByLabel("API key", { exact: true }).fill(KEY + "x");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await expect(page.getByTestId("test-result")).toHaveCount(0);
});

test("saving sends what was tested, then shows the provider without the key", async ({ page }) => {
  await signIn(page);
  const state = await serveProvider(page, undefined);
  await mockRpc(page, ProviderService.method.testProvider, passes);
  const saves = await mockRpc(page, ProviderService.method.setProvider, (body) => {
    state.provider = stored({ baseUrl: body.baseUrl as string, model: body.model as string, version: 1 });
    return { provider: state.provider };
  });
  await page.goto("/settings/provider");
  await fill(page);
  await page.getByRole("button", { name: "Test connection" }).click();

  await page.getByRole("button", { name: "Save", exact: true }).click();

  await expect.poll(() => saves.length).toBe(1);
  expect(saves[0].body).toMatchObject({ baseUrl: TYPED.address, model: "my-llama", apiKey: KEY });
  // Version 0 means create; proto3 JSON leaves a zero out.
  expect((saves[0].body as Record<string, unknown>).version ?? 0).toBe(0);
  await expect(page.getByTestId("provider-state")).toContainText("AI features are on");
  await expect(page.getByLabel("API key", { exact: true })).toHaveValue("");
  // The form starts again from what is stored, so the test result is gone.
  await expect(page.getByTestId("test-result")).toHaveCount(0);
  await expect(page.locator("body")).not.toContainText(KEY);
});

test("a save the server refuses after its own test says it was not saved", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);
  await mockRpc(page, ProviderService.method.testProvider, passes);
  await mockRpcError(page, ProviderService.method.setProvider, "failed_precondition", "x", "PROVIDER_TEST_FAILED");
  await page.goto("/settings/provider");
  await fill(page);
  await page.getByRole("button", { name: "Test connection" }).click();

  await page.getByRole("button", { name: "Save", exact: true }).click();

  await expect(page.getByRole("main").getByRole("alert")).toContainText("did not pass its test, so it was not saved");
  await expect(page.locator("body")).not.toContainText(KEY);
});

test("a saved provider shows its model, address, last four and tokens against the limit", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored({ monthlyTokenCap: BigInt(500000) }), BigInt(12345));

  await page.goto("/settings/provider");

  await expect(page.getByTestId("provider-state")).toContainText("my-llama at https://llm.example.com");
  await expect(page.getByTestId("provider-state")).toContainText("CRET");
  await expect(page.getByText("12,345 tokens used this month of 500,000.")).toBeVisible();
  await expect(page.getByLabel("Base URL")).toHaveValue("https://llm.example.com");
  await expect(page.getByLabel("Monthly token limit")).toHaveValue("500000");
  await expect(page.getByLabel("API key", { exact: true })).toHaveAttribute("placeholder", "Keep the key ending CRET");
});

test("prices and the limit of a saved provider save with no test and no key", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  const tests = await mockRpc(page, ProviderService.method.testProvider, passes);
  const saves = await mockRpc(page, ProviderService.method.setProvider, () => ({ provider: stored({ version: 4 }) }));
  await page.goto("/settings/provider");

  await page.getByLabel("Input price ($ per million tokens)").fill("3");
  await page.getByLabel("Output price ($ per million tokens)").fill("15.5");
  await page.getByLabel("Monthly token limit").fill("250000");
  await page.getByRole("button", { name: "Save", exact: true }).click();

  await expect.poll(() => saves.length).toBe(1);
  expect(saves[0].body).toMatchObject({
    baseUrl: "https://llm.example.com",
    model: "my-llama",
    version: 3,
    inputMicrosPerMtok: "3000000",
    outputMicrosPerMtok: "15500000",
    monthlyTokenCap: "250000",
  });
  expect((saves[0].body as Record<string, unknown>).apiKey ?? "").toBe("");
  expect(tests).toHaveLength(0);
});

test("a trailing slash or space on the saved address is not a change", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  await page.goto("/settings/provider");

  await page.getByLabel("Base URL").fill(" https://llm.example.com/ ");

  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
});

test("changing a saved provider's address or model needs the key again and a passing test", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  const tests = await mockRpc(page, ProviderService.method.testProvider, passes);
  const saves = await mockRpc(page, ProviderService.method.setProvider, () => ({ provider: stored({ version: 4 }) }));
  await page.goto("/settings/provider");

  await page.getByLabel("Model name").fill("my-llama-2");

  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Test connection" })).toBeDisabled();
  await expect(page.getByTestId("save-hint")).toContainText("Enter the API key again");
  expect(saves).toHaveLength(0);

  await page.getByLabel("API key", { exact: true }).fill(KEY);
  await page.getByRole("button", { name: "Test connection" }).click();
  await expect(page.getByTestId("test-result")).toContainText("Connected");
  await page.getByRole("button", { name: "Save", exact: true }).click();

  await expect.poll(() => saves.length).toBe(1);
  expect(tests[0].body).toEqual({ baseUrl: "https://llm.example.com", model: "my-llama-2", apiKey: KEY });
  expect(saves[0].body).toMatchObject({ model: "my-llama-2", apiKey: KEY, version: 3 });
});

test("a half-filled price or a bad limit cannot be saved", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  const saves = await mockRpc(page, ProviderService.method.setProvider, () => ({ provider: stored() }));
  await page.goto("/settings/provider");

  await page.getByLabel("Input price ($ per million tokens)").fill("3");
  await expect(page.getByRole("main").getByRole("alert")).toContainText("Enter both prices");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();

  await page.getByLabel("Output price ($ per million tokens)").fill("9");
  await page.getByLabel("Monthly token limit").fill("0");
  await expect(page.getByRole("main").getByRole("alert")).toContainText("whole number above 0");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  expect(saves).toHaveLength(0);
});

test("a provider changed elsewhere says so", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  await mockRpcError(page, ProviderService.method.setProvider, "aborted", "x", "VERSION_CONFLICT");
  await page.goto("/settings/provider");

  await page.getByLabel("Monthly token limit").fill("100");
  await page.getByRole("button", { name: "Save", exact: true }).click();

  await expect(page.getByRole("main").getByRole("alert")).toContainText("changed elsewhere");
});

test("a spent budget or metering down during a test says so and enables nothing", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, undefined);
  await page.goto("/settings/provider");
  await fill(page);

  for (const [reason, code, words] of [
    ["BUDGET_EXHAUSTED", "resource_exhausted", "budget or token limit is used up"],
    ["METERING_UNAVAILABLE", "unavailable", "Cost metering is unavailable"],
  ] as const) {
    await mockRpcError(page, ProviderService.method.testProvider, code, "x", reason);
    await page.getByRole("button", { name: "Test connection" }).click();
    await expect(page.getByTestId("test-result")).toContainText(words);
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  }
});

test("the saved provider can be tested on its own", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  const tests = await mockRpc(page, ProviderService.method.testProvider, passes);
  await page.goto("/settings/provider");

  await page.getByRole("button", { name: "Test the saved provider" }).click();

  await expect(page.getByTestId("saved-test-result")).toContainText("It works. my-llama replied in 390 ms.");
  expect(tests[0].body ?? {}).toEqual({});
});

test("the saved provider's test says why it did not work", async ({ page }) => {
  await signIn(page);
  await serveProvider(page, stored());
  await mockRpc(page, ProviderService.method.testProvider, { ok: false, failure: ProviderFailure.REJECTED });
  await page.goto("/settings/provider");

  await page.getByRole("button", { name: "Test the saved provider" }).click();

  await expect(page.getByTestId("saved-test-result")).toContainText("The provider refused the key");
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

  await page.goto("/settings/discovery");
  const notice = page.getByTestId("ai-provider-notice");

  await expect(notice).toContainText("Set up an AI provider to use AI features");
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

  await page.goto("/settings/discovery");

  await expect(page.getByRole("heading", { name: "What's out there." })).toBeVisible();
  await expect(page.getByTestId("ai-provider-notice")).toHaveCount(0);
});

test("the notice stays out of the way when the provider cannot be read", async ({ page }) => {
  await signIn(page);
  await mockRpcError(page, ProviderService.method.getProvider, "unavailable", "down");

  await page.goto("/settings/discovery");

  await expect(page.getByRole("heading", { name: "What's out there." })).toBeVisible();
  await expect(page.getByTestId("ai-provider-notice")).toHaveCount(0);
});
