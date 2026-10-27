import path from "node:path";
import { stmtClient } from "../helpers/api";
import { expect, test } from "../helpers/test";

// The fixture is a copy of the client's Fidelity UK test export, modelled on
// a real export with its identifiers replaced. Its period starts on 1
// January; narrowing it to February rejects the four January rows.
const fixture = path.resolve(__dirname, "..", "fixtures", "fidelity-uk.csv");
const januaryRows = 4;

test("uploads a statement and shows its rejections in the activity and the history", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn();
  await page.goto("/txns");
  await page.getByTestId("upload-stmt").click();
  await expect(page.getByTestId("upload-dialog")).toBeVisible();
  await page.getByTestId("upload-file").setInputFiles(fixture);
  await expect(page.getByTestId("upload-recognised")).toHaveText(
    "fidelity-uk.csv - Recognised as: Fidelity UK export",
  );
  await expect(page.getByTestId("upload-rows")).toHaveText("11 rows");
  await page.getByTestId("upload-from").fill("2025-02-01");
  await expect(page.getByTestId("upload-outside")).toContainText(
    `${januaryRows} rows fall outside`,
  );
  await page.getByTestId("upload-submit").click();

  // The dialog hands off to the activity sheet, whose item follows the run.
  await expect(page.getByTestId("upload-dialog")).toBeHidden();
  const sheet = page.getByTestId("activity-sheet");
  await expect(sheet).toBeVisible();
  const item = sheet.getByTestId(/^activity-item-/).first();
  await expect(item.getByTestId("state-chip")).toHaveAttribute(
    "data-state",
    "rejections",
  );
  const runId = (await item.getAttribute("data-testid"))?.replace(
    "activity-item-",
    "",
  );
  expect(runId).toBeTruthy();
  await item.getByTestId(`activity-toggle-${runId}`).click();
  await expect(sheet.getByTestId("rejection-reason-0")).toHaveText(
    "order date outside the claimed period",
  );
  await expect(sheet.getByTestId("rejection-count-0")).toHaveText(
    String(januaryRows),
  );

  // The history agrees, and so does the statement's own page.
  await sheet.getByTestId("activity-all").click();
  await expect(page).toHaveURL("/stmts");
  const row = page.getByTestId(`stmt-row-${runId}`);
  await expect(row.getByTestId("stmt-rejected")).toHaveText(
    String(januaryRows),
  );
  await row.getByRole("link").click();
  await expect(page).toHaveURL(`/stmts/${runId}`);
  await expect(page.getByTestId("page-title")).toContainText(
    "Fidelity UK upload @",
  );
  // The closed sheet keeps its expanded item, so the page is scoped.
  const stmt = page.getByTestId("stmt-page");
  await expect(stmt.getByTestId("stmt-rejected")).toHaveText(
    String(januaryRows),
  );
  await expect(stmt.getByTestId("rejection-count-0")).toHaveText(
    String(januaryRows),
  );

  // The record behind the pages.
  const res = await stmtClient(session).getStmt({
    runId: runId!,
  });
  expect(res.items).toHaveLength(januaryRows);
  expect(
    res.items.every(
      (i) => i.reason === "order date outside the claimed period",
    ),
  ).toBe(true);
});
