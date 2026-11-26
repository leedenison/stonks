import { RefusalReason } from "../gen/admin/price/v1/price_pb";
import { RunKind, RunState } from "../gen/run/v1/run_pb";
import { adminInstrClient, adminRunClient, priceClient } from "../helpers/api";
import { deleteRuns, described } from "../helpers/db";
import { expect, test } from "../helpers/test";

// An upload is a system run, so deleting a user does not remove it.
const uploaded: string[] = [];

test.afterAll(async () => {
  await deleteRuns(uploaded);
});

// The integration tier owns what an upload stores. This spec proves that the
// shipped binary serves both procedures and records the run. It uploads for
// the instrument that global setup seeds, on days that no holding demands, so
// it changes the gaps of no other spec.
test("lists the price gaps and uploads prices from Google Sheets as a system run", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn("admin");
  const prices = priceClient(session);
  await prices.listPriceGaps({});

  const listed = await adminInstrClient(session).listInstrs({
    query: described.ticker,
  });
  expect(listed.instrs).toHaveLength(1);
  const [listing] = listed.instrs[0].listings;
  const coverage = [{ from: "1999-03-01", before: "1999-03-06" }];
  const res = await prices.uploadPrices({
    asAt: "2026-11-02",
    series: [
      {
        listingId: listing.id,
        symbol: "LON:ZZDESC",
        currency: "GBX",
        coverage,
        prices: [
          { day: "1999-03-01", price: "102.9" },
          { day: "1999-03-02", price: "104.5" },
        ],
      },
    ],
  });
  uploaded.push(res.run!.id);
  expect(res.refused).toHaveLength(0);
  expect(res.run?.kind).toBe(RunKind.UPLOAD_PRICES);
  expect(res.run?.state).toBe(RunState.COMPLETED);

  const run = (await adminRunClient(session).getRun({ runId: res.run!.id }))
    .run!;
  expect(run.children.map((c) => c.fetch?.datasource)).toEqual([
    "google_sheets",
  ]);

  await page.goto(`/admin/runs/${res.run!.id}`);
  await expect(page.getByTestId("page-title")).toContainText("upload prices");

  // A code outside the family of the listing is refused and named.
  const refused = await prices.uploadPrices({
    asAt: "2026-11-02",
    series: [
      {
        listingId: listing.id,
        symbol: "LON:ZZDESC",
        currency: "USD",
        coverage,
        prices: [{ day: "1999-03-01", price: "102.9" }],
      },
    ],
  });
  uploaded.push(refused.run!.id);
  expect(refused.refused.map((r) => [r.listingId, r.reason])).toEqual([
    [listing.id, RefusalReason.CURRENCY],
  ]);
});
