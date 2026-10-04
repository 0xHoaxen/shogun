import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test, type Page } from "@playwright/test";

import { AuthService } from "../src/gen/shogun/api/v1/auth_pb";
import { JobStatus, JobsService } from "../src/gen/shogun/api/v1/jobs_pb";
import { mockRpc, mockRpcError } from "./fixtures";

const PIPELINE = [
  JobStatus.SAVED,
  JobStatus.APPLIED,
  JobStatus.SHORTLISTED,
  JobStatus.INTERVIEW,
  JobStatus.OFFER,
  JobStatus.REJECTED,
];

interface CardInit {
  id: string;
  title: string;
  companyName: string;
  status: JobStatus;
  version: number;
  nextFollowUp?: string;
}

// board lays cards out in all six columns, empty ones included, like torii does.
function board(cards: readonly CardInit[]) {
  return {
    columns: PIPELINE.map((status) => ({
      status,
      jobs: cards.filter((card) => card.status === status),
    })),
  };
}

const NORTHWIND: CardInit = {
  id: "j1",
  title: "Senior Backend Engineer",
  companyName: "Northwind",
  status: JobStatus.SAVED,
  version: 3,
  nextFollowUp: "2026-10-08",
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

function column(page: Page, name: string) {
  return page.getByRole("region", { name, exact: true });
}

// moveCardRight picks the card up with Space, carries it one column right with
// the arrow key and drops it with Space again.
async function moveCardRight(page: Page, title: RegExp) {
  const card = page.getByRole("button", { name: title });
  await card.focus();
  await page.keyboard.press("Space");
  // dnd-kit announces which column the card is over once it has measured the
  // columns (column-<status number>), so wait for those before each step.
  const announcement = page.getByRole("status");
  await expect(card).toHaveAttribute("aria-pressed", "true");
  await expect(announcement).toContainText("over droppable area column-1");
  // dnd-kit's key handler can lag the announcement by a frame and ignore the
  // first press, so press again until the card is over the next column.
  await expect(async () => {
    await page.keyboard.press("ArrowRight");
    await expect(announcement).toContainText("over droppable area column-2", { timeout: 1000 });
  }).toPass();
  await page.keyboard.press("Space");
}

test("the board shows every stage with its jobs and the total", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, JobsService.method.getBoard, board([NORTHWIND]));

  // Act
  await page.goto("/jobs");

  // Assert
  await expect(page.getByRole("heading", { name: "1 role, six stages." })).toBeVisible();
  await expect(column(page, "Saved")).toContainText("Senior Backend Engineer");
  await expect(column(page, "Saved")).toContainText("Follow up 2026-10-08");
  for (const name of ["Applied", "Shortlisted", "Interview", "Offer", "Rejected"]) {
    await expect(column(page, name)).toBeVisible();
  }
});

test("adding a job sends the form with an idempotency key and shows the card", async ({ page }) => {
  // Arrange
  await signIn(page);
  const added: CardInit = {
    id: "j2",
    title: "SRE",
    companyName: "Tessellate",
    status: JobStatus.SAVED,
    version: 1,
  };
  let current = board([NORTHWIND]);
  await mockRpc(page, JobsService.method.getBoard, () => current);
  const addCalls = await mockRpc(page, JobsService.method.addJob, () => {
    current = board([NORTHWIND, added]);
    return { job: added };
  });
  await page.goto("/jobs");

  // Act
  await page.getByRole("button", { name: "Add job" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Title").fill("SRE");
  await dialog.getByLabel("Company", { exact: true }).fill("Tessellate");
  await dialog.getByRole("button", { name: "Add job" }).click();

  // Assert
  await expect(column(page, "Saved")).toContainText("SRE");
  await expect(dialog).toBeHidden();
  expect(addCalls).toHaveLength(1);
  expect(addCalls[0].body).toMatchObject({ title: "SRE", companyName: "Tessellate" });
  expect(addCalls[0].headers["idempotency-key"]).toBeTruthy();
});

test("a job cannot be added without a title and company", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, JobsService.method.getBoard, board([]));
  await page.goto("/jobs");

  // Act
  await page.getByRole("button", { name: "Add job" }).click();
  const submit = page.getByRole("dialog").getByRole("button", { name: "Add job" });

  // Assert
  await expect(submit).toBeDisabled();
});

test("dragging a card with the keyboard moves it and sends the change", async ({ page }) => {
  // Arrange
  await signIn(page);
  const applied: CardInit = { ...NORTHWIND, status: JobStatus.APPLIED, version: 4 };
  let current = board([NORTHWIND]);
  await mockRpc(page, JobsService.method.getBoard, () => current);
  const moveCalls = await mockRpc(page, JobsService.method.changeJobStatus, () => {
    current = board([applied]);
    return { job: applied };
  });
  await page.goto("/jobs");

  // Act
  await moveCardRight(page, /Senior Backend Engineer/);

  // Assert
  await expect(column(page, "Applied")).toContainText("Senior Backend Engineer");
  await expect(column(page, "Saved")).not.toContainText("Senior Backend Engineer");
  expect(moveCalls).toHaveLength(1);
  expect(moveCalls[0].body).toMatchObject({ id: "j1", toStatus: "JOB_STATUS_APPLIED", version: 3 });
});

test("a rejected move puts the card back and says why", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, JobsService.method.getBoard, board([NORTHWIND]));
  await mockRpcError(
    page,
    JobsService.method.changeJobStatus,
    "failed_precondition",
    "cannot move from saved to applied",
    "JOB_STATUS_INVALID_TRANSITION",
  );
  await page.goto("/jobs");

  // Act
  await moveCardRight(page, /Senior Backend Engineer/);

  // Assert
  await expect(page.getByRole("main").getByRole("alert")).toHaveText(
    "A job can't move from Saved to Applied.",
  );
  await expect(column(page, "Saved")).toContainText("Senior Backend Engineer");
  await expect(column(page, "Applied")).not.toContainText("Senior Backend Engineer");
});

test("a stale version reloads the board and says the job changed", async ({ page }) => {
  // Arrange
  await signIn(page);
  await mockRpc(page, JobsService.method.getBoard, board([NORTHWIND]));
  await mockRpcError(
    page,
    JobsService.method.changeJobStatus,
    "aborted",
    "version is stale",
    "VERSION_CONFLICT",
  );
  await page.goto("/jobs");

  // Act
  await moveCardRight(page, /Senior Backend Engineer/);

  // Assert
  await expect(page.getByRole("main").getByRole("alert")).toHaveText(
    "This job changed elsewhere. The board was reloaded.",
  );
  await expect(column(page, "Saved")).toContainText("Senior Backend Engineer");
});
