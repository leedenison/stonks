import path from "node:path";
import { HoldingMark } from "../gen/holding/v1/holding_pb";
import { holdingClient } from "../helpers/api";
import { expect, test } from "../helpers/test";
import { uploadStmt } from "../helpers/upload";

// The fixture buys 10 NVDA before its 10 for 1 split of 2024-06-10 and sells
// 50 after it. It also buys 10 GE before the GE Vernova spin-off of
// 2024-04-02, which Massive states as a split with a ratio that has no whole
// share count. No other spec states NVDA or GE.
//
// A fetch before 00:00 UTC followed by a read after it would leave NVDA
// provisional, because coverage would stop short of today. Its quantity is
// restated either way, so the spec asserts the quantity and leaves out the
// mark.
const fixture = path.resolve(
  __dirname,
  "..",
  "fixtures",
  "standard-split.json",
);

test("restates a holding across a split, and marks one across an unhandled action as provisional", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn();
  // The holdings are read before the upload, so the figures on the page come
  // from the refresh that follows the statement's settling.
  await page.goto("/holdings");
  await uploadStmt(page, fixture, { state: "completed" });

  const res = await holdingClient(session).listHoldings({});
  const byName = (value: string) =>
    res.instrs.find((h) => h.listings.some((l) => l.ticker?.value === value))!;
  const nvda = byName("NVDA");
  const ge = byName("GE");
  expect(nvda.quantity).toBe("50");
  expect(ge.quantity).toBe("10");
  expect(ge.mark).toBe(HoldingMark.CORP_ACTS_PROVISIONAL);
  expect(ge.provisionalFrom).toBe("2023-06-02");
  expect(ge.gaps.map((g) => [g.uncoveredFrom, g.uncoveredBefore])).toEqual([
    ["2024-04-02", "2024-04-03"],
  ]);

  await page.getByTestId("activity-sheet-close").click();
  const nvdaRow = page.getByTestId(`holding-row-${nvda.instrId}`);
  await expect(nvdaRow.getByTestId(`holding-qty-${nvda.instrId}`)).toHaveText(
    "50.00",
  );

  const geRow = page.getByTestId(`holding-row-${ge.instrId}`);
  await expect(geRow.getByTestId("holding-mark")).toHaveAttribute(
    "data-state",
    "provisional",
  );
  await expect(geRow.getByTestId("holding-mark")).toHaveText("Provisional");
  await geRow.click();
  await expect(
    page.getByTestId(`holding-provisional-${ge.instrId}`),
  ).toContainText("Corporate actions are unknown for 2024-04-02.");
});
