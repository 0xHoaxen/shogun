import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import {
  BudgetMode,
  BudgetPeriod,
  BudgetScope,
  CostsService,
} from "../src/gen/shogun/api/v1/costs_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const MICROS = 1_000_000;

function dollars(amount: number): bigint {
  return BigInt(Math.round(amount * MICROS));
}

function globalBudget(limit: number, version = 1) {
  return {
    id: "b1",
    scope: BudgetScope.GLOBAL,
    scopeValue: "",
    period: BudgetPeriod.MONTHLY,
    limitMicros: dollars(limit),
    mode: BudgetMode.HARD,
    thresholds: [50, 80, 100],
    enabled: true,
    version,
    spentMicros: dollars(2),
    reservedMicros: dollars(0),
    resetsAt: "2026-10-31T18:30:00Z",
  };
}

const FUDE_FEATURE = {
  id: "b2",
  scope: BudgetScope.FEATURE,
  scopeValue: "fude.post",
  period: BudgetPeriod.DAILY,
  limitMicros: dollars(1),
  mode: BudgetMode.SOFT,
  thresholds: [50, 80, 100],
  enabled: true,
  version: 1,
  spentMicros: dollars(0.25),
  reservedMicros: dollars(0),
  resetsAt: "2026-10-05T18:30:00Z",
};

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: {
      email: "owner@example.com",
      displayName: "Owner",
      expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
    },
  });
}

async function mockQuietSpend(page: Page) {
  await mockRpc(page, CostsService.method.getSpend, { rows: [], totalMicros: BigInt(0) });
}

test("the page lists spend for the range with its total", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, CostsService.method.getSpend, {
    rows: [
      { key: "2026-10-04", costMicros: dollars(3), inputTokens: BigInt(1500), outputTokens: BigInt(200) },
      { key: "2026-10-05", costMicros: dollars(4.5), inputTokens: BigInt(2500), outputTokens: BigInt(300) },
    ],
    totalMicros: dollars(7.5),
  });
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [] });

  // Act
  await page.goto("/settings/spend");

  // Assert
  await expect(page.getByRole("link", { name: "Spend" })).toHaveAttribute("aria-current", "page");
  const row = page.getByRole("row", { name: /2026-10-05/ });
  await expect(row).toContainText("$4.50");
  await expect(row).toContainText("2,500");
  await expect(page.getByRole("row", { name: /2026-10-04/ })).toContainText("$3.00");
  await expect(page.getByTestId("spend-total")).toHaveText("$7.50");
});

test("a spend of a fraction of a cent is not shown as zero", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, CostsService.method.getSpend, {
    rows: [{ key: "2026-10-05", costMicros: BigInt(4000) }],
    totalMicros: BigInt(4000),
  });
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [] });

  await page.goto("/settings/spend");

  await expect(page.getByTestId("spend-total")).toHaveText("$0.004");
});

test("choosing a group asks soroban for that group and relabels the column", async ({ page }) => {
  // Arrange
  await signIn(page);
  const calls = await mockRpc(page, CostsService.method.getSpend, (body) => ({
    rows: [{ key: body.groupBy === "SPEND_GROUP_SERVICE" ? "fude" : "2026-10-05", costMicros: dollars(1) }],
    totalMicros: dollars(1),
  }));
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [] });
  await page.goto("/settings/spend");
  await expect(page.getByRole("row", { name: /2026-10-05/ })).toBeVisible();

  // Act
  await page.getByRole("button", { name: "Service" }).click();

  // Assert
  await expect(page.getByRole("row", { name: /fude/ })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Service" })).toBeVisible();
  const groups = calls.map((call) => (call.body as { groupBy?: string }).groupBy);
  expect(groups[0]).toBe("SPEND_GROUP_DAY");
  expect(groups).toContain("SPEND_GROUP_SERVICE");
});

test("an empty range says nothing was spent", async ({ page }) => {
  await signIn(page);
  await mockQuietSpend(page);
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [] });

  await page.goto("/settings/spend");

  await expect(page.getByText("Nothing spent in this range.")).toBeVisible();
});

test("budgets show what they cover, the spend so far and the limit", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockQuietSpend(page);

  // Act
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [globalBudget(20), FUDE_FEATURE] });
  await page.goto("/settings/spend");

  // Assert
  const global = page.getByRole("row", { name: /^All services/ });
  await expect(global).toContainText("Monthly");
  await expect(global).toContainText("Hard");
  await expect(global).toContainText("$2.00 (10%)");
  await expect(global).toContainText("$20.00");
  const feature = page.getByRole("row", { name: /fude\.post/ });
  await expect(feature).toContainText("Daily");
  await expect(feature).toContainText("Soft");
  await expect(feature).toContainText("$0.25 (25%)");
});

test("editing a budget saves the new limit and shows the updated figures", async ({ page }) => {
  // Arrange: the mock keeps the limit the page last saved.
  await signIn(page);
  await mockQuietSpend(page);
  let limit = 20;
  let version = 1;
  await mockRpc(page, CostsService.method.listBudgets, () => ({ budgets: [globalBudget(limit, version)] }));
  const updates = await mockRpc(page, CostsService.method.updateBudget, (body) => {
    limit = Number(BigInt(body.limitMicros as string) / BigInt(MICROS));
    version += 1;
    return { budget: globalBudget(limit, version) };
  });
  await page.goto("/settings/spend");
  await expect(page.getByRole("row", { name: /^All services/ })).toContainText("$20.00");

  // Act
  await page.getByRole("button", { name: "Edit All services Monthly" }).click();
  await page.getByLabel("Limit (USD)").fill("25");
  await page.getByRole("button", { name: "Save" }).click();

  // Assert
  const row = page.getByRole("row", { name: /^All services/ });
  await expect(row).toContainText("$25.00");
  await expect(row).toContainText("$2.00 (8%)");
  await expect(page.getByRole("form", { name: "Edit All services budget" })).toBeHidden();
  expect(updates).toHaveLength(1);
  expect(updates[0].body).toMatchObject({
    id: "b1",
    limitMicros: "25000000",
    mode: "BUDGET_MODE_HARD",
    enabled: true,
    version: 1,
    thresholds: [50, 80, 100],
  });
});

test("a budget can be switched to soft and turned off", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockQuietSpend(page);
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [globalBudget(20)] });
  const updates = await mockRpc(page, CostsService.method.updateBudget, {
    budget: { ...globalBudget(20, 2), mode: BudgetMode.SOFT, enabled: false },
  });
  await page.goto("/settings/spend");

  // Act
  await page.getByRole("button", { name: "Edit All services Monthly" }).click();
  await page.getByLabel("Mode").selectOption({ label: "Soft: only notifies" });
  await page.getByLabel("Enabled").uncheck();
  await page.getByRole("button", { name: "Save" }).click();

  // Assert
  await expect(page.getByRole("form", { name: "Edit All services budget" })).toBeHidden();
  expect(updates[0].body).toMatchObject({ mode: "BUDGET_MODE_SOFT" });
  expect((updates[0].body as { enabled?: boolean }).enabled ?? false).toBe(false);
});

test("a limit that is not an amount cannot be saved", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockQuietSpend(page);
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [globalBudget(20)] });
  const updates = await mockRpc(page, CostsService.method.updateBudget, { budget: globalBudget(20, 2) });
  await page.goto("/settings/spend");
  await page.getByRole("button", { name: "Edit All services Monthly" }).click();

  // Act
  await page.getByLabel("Limit (USD)").fill("twenty");

  // Assert
  await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(page.getByText("Enter an amount in dollars")).toBeVisible();
  expect(updates).toHaveLength(0);
});

test("cancelling an edit changes nothing", async ({ page }) => {
  await signIn(page);
  await mockQuietSpend(page);
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [globalBudget(20)] });
  const updates = await mockRpc(page, CostsService.method.updateBudget, { budget: globalBudget(20, 2) });
  await page.goto("/settings/spend");
  await page.getByRole("button", { name: "Edit All services Monthly" }).click();
  await page.getByLabel("Limit (USD)").fill("99");

  await page.getByRole("button", { name: "Cancel" }).click();

  await expect(page.getByRole("form", { name: "Edit All services budget" })).toBeHidden();
  await expect(page.getByRole("row", { name: /^All services/ })).toContainText("$20.00");
  expect(updates).toHaveLength(0);
});

test("a budget changed elsewhere says so", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockQuietSpend(page);
  await mockRpc(page, CostsService.method.listBudgets, { budgets: [globalBudget(20)] });
  await mockRpcError(page, CostsService.method.updateBudget, "aborted", "changed", "VERSION_CONFLICT");
  await page.goto("/settings/spend");

  // Act
  await page.getByRole("button", { name: "Edit All services Monthly" }).click();
  await page.getByLabel("Limit (USD)").fill("30");
  await page.getByRole("button", { name: "Save" }).click();

  // Assert
  await expect(page.getByRole("main").getByRole("alert")).toContainText("changed elsewhere");
  await expect(page.getByRole("form", { name: "Edit All services budget" })).toBeVisible();
});

test("a failed load offers a retry", async ({ page }) => {
  await signIn(page);
  await mockQuietSpend(page);
  await mockRpcError(page, CostsService.method.listBudgets, "internal", "boom");

  await page.goto("/settings/spend");

  await expect(page.getByRole("main").getByRole("alert")).toContainText("Couldn't load your budgets.");
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});
