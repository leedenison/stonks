import path from "node:path";
import { FetchKind } from "../gen/admin/block/v1/block_pb";
import { HoldingMark, type InstrHolding } from "../gen/holding/v1/holding_pb";
import { adminRunClient, fetchRun, holdingClient } from "../helpers/api";
import { nextDay, utcDay } from "../helpers/day";
import { expect, test } from "../helpers/test";
import { uploadStmt } from "../helpers/upload";

// The fixture buys 10 NVDA before its 10 for 1 split of 2024-06-10 and sells
// 50 after it. It also buys 10 GE before the GE Vernova spin-off of
// 2024-04-02, which Massive states as a split with a ratio that has no whole
// share count. No other spec states NVDA or GE.
const fixture = path.resolve(
  __dirname,
  "..",
  "fixtures",
  "standard-split.json",
);

test("restates a holding across a split, and marks one across an unhandled action as provisional", async ({
  signIn,
  seed,
  page,
}) => {
  const { session } = await signIn();
  const admin = adminRunClient((await seed("admin")).session);
  // The holdings are read before the upload, so the figures on the page come
  // from the refresh that follows the statement's settling.
  await page.goto("/holdings");
  const runId = await uploadStmt(page, fixture, { state: "completed" });
  const fetched = fetchRun(
    (await admin.getRun({ runId })).run!,
    "massive",
    FetchKind.CORP_ACTS,
  ).run!;
  // The fetch covers the days through the day it starts. If the read falls
  // on the next day, that day is a gap, so a holding may carry one more gap.
  const day = nextDay(utcDay(fetched.startedAt!));
  const later = [day, nextDay(day)];

  const res = await holdingClient(session).listHoldings({});
  const byName = (value: string) =>
    res.instrs.find((h) => h.listings.some((l) => l.ticker?.value === value))!;
  const nvda = byName("NVDA");
  const ge = byName("GE");
  const gaps = (h: InstrHolding) => h.gaps.map((g) => [g.from, g.before]);
  expect(nvda.quantity).toBe("50");
  if (nvda.mark === HoldingMark.ADJUSTED) {
    expect(gaps(nvda)).toEqual([]);
  } else {
    expect(nvda.mark).toBe(HoldingMark.CORP_ACTS_PROVISIONAL);
    expect(gaps(nvda)).toEqual([later]);
  }
  expect(ge.quantity).toBe("10");
  expect(ge.mark).toBe(HoldingMark.CORP_ACTS_PROVISIONAL);
  expect(ge.provisionalFrom).toBe("2023-06-02");
  const split = ["2024-04-02", "2024-04-03"];
  expect([[split], [split, later]]).toContainEqual(gaps(ge));

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
