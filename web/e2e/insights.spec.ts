import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { InsightsService } from "../src/gen/shogun/api/v1/insights_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const n = (v: number) => BigInt(v);

function funnelRow(key: string, over: Record<string, number> = {}) {
  const v = { jobsAdded: 10, applications: 8, shortlisted: 3, interviews: 2, offers: 1, rejections: 4, ...over };
  return {
    key, jobsAdded: n(v.jobsAdded), applications: n(v.applications), shortlisted: n(v.shortlisted), interviews: n(v.interviews),
    offers: n(v.offers), rejections: n(v.rejections),
    interviewRate: v.applications ? v.interviews / v.applications : 0, offerRate: v.applications ? v.offers / v.applications : 0,
  };
}

async function signIn(page: Page) {
  await mockRpc(page, AuthService.method.getSession, {
    session: { email: "owner@example.com", displayName: "Owner", expiresAt: timestampFromDate(new Date("2026-11-01T00:00:00Z")) },
  });
}

async function noOutreach(page: Page) {
  await mockRpc(page, InsightsService.method.getInsightsOutreach, { from: "", to: "", rows: [] });
}

test("the funnel lists each source with counts and rates, and a total", async ({ page }) => {
  await signIn(page);
  await noOutreach(page);
  const calls = await mockRpc(page, InsightsService.method.getInsightsFunnel, {
    from: "2026-09-09", to: "2026-10-08",
    rows: [funnelRow("linkedin"), funnelRow("referral", { applications: 0, interviews: 0, offers: 0 })],
    total: funnelRow("total", { jobsAdded: 20, applications: 8 }),
  });

  await page.goto("/insights");

  const funnel = page.getByRole("region", { name: "Funnel" });
  const linkedin = funnel.getByRole("row", { name: /^linkedin/ });
  await expect(linkedin).toContainText("10");
  await expect(linkedin).toContainText("25%");
  await expect(linkedin).toContainText("13%");
  // No applications means no meaningful rate.
  await expect(funnel.getByRole("row", { name: /^referral/ })).toContainText("-");
  await expect(funnel.getByRole("row", { name: /^All/ })).toContainText("20");
  const body = calls[0].body as { from: string; to: string; groupBy: string };
  expect(body.groupBy).toBe("INSIGHTS_FUNNEL_GROUP_SOURCE");
  expect(body.from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  expect(body.to).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  // Thirty days, both ends included.
  expect((Date.parse(body.to) - Date.parse(body.from)) / 86_400_000).toBe(29);
});

test("changing the range and the grouping asks for them", async ({ page }) => {
  await signIn(page);
  await noOutreach(page);
  const calls = await mockRpc(page, InsightsService.method.getInsightsFunnel, { from: "", to: "", rows: [funnelRow("2026-10")], total: funnelRow("total") });
  await page.goto("/insights");
  const funnel = page.getByRole("region", { name: "Funnel" });

  await funnel.getByRole("button", { name: "By month" }).click();
  await funnel.getByRole("button", { name: "Last year" }).click();

  await expect(funnel.getByRole("row", { name: /^2026-10/ })).toBeVisible();
  const last = calls.at(-1)?.body as { from: string; to: string; groupBy: string };
  expect(last.groupBy).toBe("INSIGHTS_FUNNEL_GROUP_MONTH");
  expect((Date.parse(last.to) - Date.parse(last.from)) / 86_400_000).toBe(364);
});

test("an empty funnel says nothing yet and explains the delay", async ({ page }) => {
  await signIn(page);
  await noOutreach(page);
  await mockRpc(page, InsightsService.method.getInsightsFunnel, { from: "", to: "", rows: [] });

  await page.goto("/insights");

  await expect(page.getByText(/Nothing yet in this range\. Today's events/)).toBeVisible();
});

test("outreach by channel shows contacted, replied and the reply rate", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, InsightsService.method.getInsightsFunnel, { from: "", to: "", rows: [] });
  const calls = await mockRpc(page, InsightsService.method.getInsightsOutreach, {
    from: "", to: "",
    rows: [
      { key: "email", sent: n(8), replied: n(4), replyRate: 0.5, movedIn: n(0) },
      { key: "linkedin", sent: n(0), replied: n(0), replyRate: 0, movedIn: n(0) },
    ],
    total: { key: "total", sent: n(8), replied: n(4), replyRate: 0.5, movedIn: n(0) },
  });

  await page.goto("/insights");

  const outreach = page.getByRole("region", { name: "Outreach" });
  await expect(outreach.getByRole("row", { name: /^email/ })).toContainText("50%");
  await expect(outreach.getByRole("row", { name: /^linkedin/ })).toContainText("-");
  await expect(outreach).toContainText("All channels: 8 contacted, 4 replied (50%)");
  expect(calls[0].body).toMatchObject({ groupBy: "INSIGHTS_OUTREACH_GROUP_CHANNEL" });
});

test("outreach by status shows how many moved into each", async ({ page }) => {
  await signIn(page);
  await mockRpc(page, InsightsService.method.getInsightsFunnel, { from: "", to: "", rows: [] });
  const calls = await mockRpc(page, InsightsService.method.getInsightsOutreach, {
    from: "", to: "",
    rows: [{ key: "reached_out", sent: n(0), replied: n(0), replyRate: 0, movedIn: n(6) }, { key: "replied", sent: n(0), replied: n(0), replyRate: 0, movedIn: n(2) }],
    total: { key: "total", sent: n(6), replied: n(2), replyRate: 1 / 3, movedIn: n(0) },
  });
  await page.goto("/insights");

  await page.getByRole("button", { name: "By status" }).click();

  const outreach = page.getByRole("region", { name: "Outreach" });
  await expect(outreach.getByRole("row", { name: /^reached_out/ })).toContainText("6");
  await expect(outreach.getByRole("columnheader", { name: "Moved into" })).toBeVisible();
  await expect(outreach).toContainText("Overall: 6 contacted, 2 replied (33%)");
  expect(calls.at(-1)?.body).toMatchObject({ groupBy: "INSIGHTS_OUTREACH_GROUP_STATUS" });
});

test("a refused range says what to check, and a failed load offers a retry", async ({ page }) => {
  await signIn(page);
  await noOutreach(page);
  await mockRpcError(page, InsightsService.method.getInsightsFunnel, "invalid_argument", "x", "INVALID_RANGE");

  await page.goto("/insights");

  await expect(page.locator("p[role=alert]")).toContainText("range does not work");
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});

test("the nav has an Insights tab", async ({ page }) => {
  await signIn(page);
  await noOutreach(page);
  await mockRpc(page, InsightsService.method.getInsightsFunnel, { from: "", to: "", rows: [] });

  await page.goto("/insights");

  await expect(page.getByRole("link", { name: "Insights" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "How it's going." })).toBeVisible();
});
