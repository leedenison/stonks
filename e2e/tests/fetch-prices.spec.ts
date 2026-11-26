import fs from "node:fs";
import path from "node:path";
import { fromJson, type JsonValue } from "@bufbuild/protobuf";
import { FetchKind } from "../gen/admin/block/v1/block_pb";
import type { PriceGap } from "../gen/admin/price/v1/price_pb";
import { FetchOutcome, type UserRun } from "../gen/admin/run/v1/run_pb";
import { RunKind, RunState } from "../gen/run/v1/run_pb";
import { StmtSchema } from "../gen/stmt/v1/stmt_pb";
import { IdentifierType } from "../gen/type/v1/type_pb";
import {
  adminRunClient,
  fetchItems,
  fetchRun,
  priceClient,
  settled,
  stmtClient,
} from "../helpers/api";
import { deleteRuns } from "../helpers/db";
import { expect, test } from "../helpers/test";
import { uploadStmt } from "../helpers/upload";

// The fixture buys VOD in London on 2025-06-02, stated by its ISIN, and COST
// on 2026-10-05. No other spec states either, and VOD brings GBP into use.
const fixture = path.resolve(
  __dirname,
  "..",
  "fixtures",
  "standard-prices.json",
);
// A day on which both are held, inside the years the recordings hold.
const held = "2026-10-05";
// The recordings come from keys that serve one or two years. EODHD's
// recorded history of VOD starts in November 2025, and Massive does not serve
// London, so the first months of the VOD holding stay a gap on every day
// the suite runs. A recording from a key that serves every year closes the
// gap, and the run from the panel then needs another gap to ask for.
const gapDay = "2025-06-02";

// EODHD's symbol for VOD in London, which its fetch key records.
const vodSymbol = "VOD.LSE";

// A run from the panel is a system run, so deleting a user does not remove
// it.
const started: string[] = [];

test.afterAll(async () => {
  await deleteRuns(started);
});

// holds reports whether a period of gap contains day.
function holds(gap: PriceGap | undefined, day: string): boolean {
  return (gap?.periods ?? []).some(
    (p) => (p.from ?? "") <= day && day < (p.before ?? "9999"),
  );
}

// fetched returns the fetch prices run beneath the run tree.
function fetched(tree: UserRun): UserRun {
  const runs = tree.children.filter(
    (c) => c.run?.kind === RunKind.FETCH_PRICES,
  );
  expect(runs).toHaveLength(1);
  return runs[0];
}

test("fetches prices after a statement and from the runs panel", async ({
  signIn,
  seed,
  page,
}) => {
  const { session } = await signIn("admin");
  const admin = adminRunClient(session);
  const prices = priceClient(session);

  await page.goto("/txns");
  const runId = await uploadStmt(page, fixture, { state: "completed" });

  // The statement's fetch is a system run. It asks Massive first, and EODHD
  // for what Massive left.
  const after = fetched((await admin.getRun({ runId })).run!);
  expect(after.userId).toBeUndefined();
  expect(after.run?.state).toBe(RunState.COMPLETED);
  expect(
    after.children.map((c) => [c.fetch?.datasource, c.fetch?.kind]),
  ).toEqual([
    ["massive", FetchKind.PRICES],
    ["eodhd", FetchKind.PRICES],
  ]);
  const massive = await fetchItems(
    admin,
    fetchRun(after, "massive", FetchKind.PRICES).run!.id,
  );
  const served = massive
    .filter((i) => i.outcome === FetchOutcome.SERVED)
    .map((i) => [i.sent?.type, i.sent?.value]);
  expect(served).toContainEqual([IdentifierType.MIC_TICKER, "COST"]);
  expect(served).toContainEqual([IdentifierType.CURRENCY, "GBP"]);
  // Massive serves the US alone, so it declines the London ticker.
  expect(
    massive.filter((i) => i.outcome === FetchOutcome.NOT_SERVED),
  ).toHaveLength(1);
  const eodhd = await fetchItems(
    admin,
    fetchRun(after, "eodhd", FetchKind.PRICES).run!.id,
  );
  const vod = eodhd.filter(
    (i) => i.outcome === FetchOutcome.SERVED && i.sent?.value === vodSymbol,
  );
  expect(vod).toHaveLength(1);

  // The day both are held is covered for each listing and for the rate. The
  // first months of VOD stay a gap.
  const gaps = (await prices.listPriceGaps({})).gaps;
  const gapOf = (type: IdentifierType, value: string) =>
    gaps.find(
      (g) => g.identifier?.type === type && g.identifier.value === value,
    );
  expect(holds(gapOf(IdentifierType.MIC_TICKER, "COST"), held)).toBe(false);
  expect(holds(gapOf(IdentifierType.MIC_TICKER, "VOD"), held)).toBe(false);
  expect(holds(gapOf(IdentifierType.CURRENCY, "GBP"), held)).toBe(false);
  expect(holds(gapOf(IdentifierType.MIC_TICKER, "VOD"), gapDay)).toBe(true);

  // The run from the panel asks for the gap again and reads past the cache
  // that the statement's fetch filled. The other gaps of the day follow the
  // clock, so the spec reads the VOD request alone.
  await page.goto("/admin");
  await page.getByTestId("runs-card-start-fetch-prices").click();
  await page.getByTestId("fetch-prices-start").click();
  await page.waitForURL(/\/admin\/runs\/[0-9a-f-]+$/);
  const panelId = page.url().split("/").pop()!;
  started.push(panelId);
  await expect(page.getByTestId("page-title")).toContainText("fetch prices");
  await expect
    .poll(async () => (await admin.getRun({ runId: panelId })).run?.run?.state)
    .toBe(RunState.COMPLETED);
  const panel = (await admin.getRun({ runId: panelId })).run!;
  expect(panel.run?.kind).toBe(RunKind.FETCH_PRICES);
  expect(panel.userId).toBeUndefined();
  const again = (
    await fetchItems(admin, fetchRun(panel, "eodhd", FetchKind.PRICES).run!.id)
  ).filter((i) => i.sent?.value === vodSymbol);
  expect(again).toHaveLength(1);
  expect(again[0].outcome).toBe(FetchOutcome.SERVED);
  expect(again[0].attempts).toBeGreaterThan(0);

  // A second user states the same holdings. The gap of VOD is the one the
  // panel run just asked for, so the statement's fetch reads its answer
  // from the cache.
  const other = await seed();
  const stmts = stmtClient(other.session);
  const stmt = fromJson(
    StmtSchema,
    JSON.parse(fs.readFileSync(fixture, "utf8")) as JsonValue,
  );
  const second = (await stmts.createStmt({ stmt })).run!.id;
  await expect
    .poll(async () => (await admin.getRun({ runId: second })).run?.run?.state)
    .toBe(RunState.COMPLETED);
  await settled(stmts, second);
  const cached = (
    await fetchItems(
      admin,
      fetchRun(
        fetched((await admin.getRun({ runId: second })).run!),
        "eodhd",
        FetchKind.PRICES,
      ).run!.id,
    )
  ).filter((i) => i.sent?.value === vodSymbol);
  expect(cached).toHaveLength(1);
  expect(cached[0].outcome).toBe(FetchOutcome.SERVED);
  expect(cached[0].attempts).toBe(0);
});
