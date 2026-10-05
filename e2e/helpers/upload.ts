import type { Page } from "@playwright/test";
import { expect } from "./test";

// uploadStatement uploads the export at fixture from the current page and
// returns the id of its run once the run reaches state. The activity sheet
// stays open.
export async function uploadStatement(
  page: Page,
  fixture: string,
  opts: {
    state: string;
    // earlier is how many uploads the activity sheet already lists. The
    // sheet keeps listing them until it has read the new run, so the helper
    // waits for the new item before it checks the state.
    earlier?: number;
    // from narrows the start of the period the statement claims.
    from?: string;
    // rows is the count of rows the dialog must read before submitting.
    rows?: string;
  },
): Promise<string> {
  await page.getByTestId("upload-statement").click();
  await page.getByTestId("upload-file").setInputFiles(fixture);
  if (opts.rows) {
    await expect(page.getByTestId("upload-rows")).toHaveText(opts.rows);
  }
  if (opts.from) {
    await page.getByTestId("upload-from").fill(opts.from);
  }
  await page.getByTestId("upload-submit").click();
  const items = page
    .getByTestId("activity-sheet")
    .getByTestId(/^activity-item-/);
  await expect(items).toHaveCount((opts.earlier ?? 0) + 1);
  const item = items.first();
  await expect(item.getByTestId("state-chip")).toHaveAttribute(
    "data-state",
    opts.state,
  );
  const id = await item.getAttribute("data-testid");
  if (!id) {
    throw new Error("the activity item names no run");
  }
  return id.replace("activity-item-", "");
}
