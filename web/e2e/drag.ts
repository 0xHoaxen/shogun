import { expect, type Page } from "@playwright/test";

// moveCardRight picks the card up with Space, carries it one column right with
// the arrow key and drops it with Space again.
export async function moveCardRight(page: Page, title: RegExp): Promise<void> {
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
