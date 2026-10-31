import path from "node:path";
import { FetchKind } from "../gen/admin/block/v1/block_pb";
import { CorpActDecision } from "../gen/admin/corpact/v1/corpact_pb";
import {
  HoldingMark,
  type InstrHolding,
  type ListHoldingsResponse,
} from "../gen/holding/v1/holding_pb";
import {
  adminRunClient,
  corpActClient,
  fetchRun,
  holdingClient,
} from "../helpers/api";
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

// byName picks the holding of the instrument a ticker names.
function byName(res: ListHoldingsResponse, value: string): InstrHolding {
  return res.instrs.find((h) =>
    h.listings.some((l) => l.ticker?.value === value),
  )!;
}

const gaps = (h: InstrHolding) => h.gaps.map((g) => [g.from, g.before]);

test("restates a holding across a split, marks one across an unhandled action as provisional, and adjusts it once the action is decided", async ({
  signIn,
  page,
}) => {
  // Deciding a corporate action needs an administrator.
  const { session } = await signIn("admin");
  const admin = adminRunClient(session);
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
  const nvda = byName(res, "NVDA");
  const ge = byName(res, "GE");
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

  // The administrator decides the action is a spin-off price factor, which
  // leaves the share count and closes the hole in the calendar.
  const acts = await corpActClient(session).listInstrCorpActs({
    instrId: ge.instrId,
  });
  const spun = acts.corpActs.find((a) => a.exDate === "2024-04-02")!;
  expect(spun.restatesShares).toBe(false);
  expect(spun.decision).toBe(CorpActDecision.UNSPECIFIED);
  await page.goto(`/admin/instrs?q=GE&id=${ge.instrId}&tab=corp-acts`);
  await page.getByTestId(`corp-act-decide-${spun.id}`).click();
  await page
    .getByTestId("decide-type")
    .selectOption(String(CorpActDecision.SPIN_OFF_PRICE_FACTOR));
  await page.getByTestId("decide-save").click();
  await expect(page.getByTestId(`corp-act-row-${spun.id}`)).toHaveAttribute(
    "data-decision",
    "SPIN_OFF_PRICE_FACTOR",
  );

  // The holding keeps its quantity and loses the 2024-04-02 gap. The fetch-day
  // gap may remain, as above.
  const decided = byName(await holdingClient(session).listHoldings({}), "GE");
  expect(decided.quantity).toBe("10");
  if (decided.mark === HoldingMark.ADJUSTED) {
    expect(gaps(decided)).toEqual([]);
  } else {
    expect(decided.mark).toBe(HoldingMark.CORP_ACTS_PROVISIONAL);
    expect(gaps(decided)).toEqual([later]);
  }
  await page.goto("/holdings");
  const geDecided = page.getByTestId(`holding-row-${ge.instrId}`);
  await expect(geDecided.getByTestId(`holding-qty-${ge.instrId}`)).toHaveText(
    "10.00",
  );
  if (decided.mark === HoldingMark.ADJUSTED) {
    await expect(geDecided.getByTestId("holding-mark")).toHaveCount(0);
  } else {
    await geDecided.click();
    await expect(
      page.getByTestId(`holding-provisional-${ge.instrId}`),
    ).not.toContainText("2024-04-02");
  }
});
