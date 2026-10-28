import path from "node:path";
import { FetchKind } from "../gen/admin/block/v1/block_pb";
import {
  FetchOutcome,
  FindingKind,
  type UserRun,
} from "../gen/admin/run/v1/run_pb";
import { TxnMark } from "../gen/stmt/v1/stmt_pb";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  instrListedAs,
  adminRunClient,
  fetchItems,
  holdingClient,
  stmtClient,
} from "../helpers/api";
import { expect, test } from "../helpers/test";
import { uploadStmt } from "../helpers/upload";

// Two statements, dated 2024 then 2021, each stating META at Nasdaq. META
// began naming Meta on 2022-06-09, which Massive's ticker history states. The
// first statement, dated 2024-01-16, dates the key through the value and asks
// Massive for Meta's history under its composite FIGI and for the value on
// its date. The second, dated 2021-06-01, reads the facts the first wrote,
// skips the identity fetch, and asks Massive for the value on that day alone.
// The day sits before 2021-06-30, when a fund took the ticker, so Massive
// answers that META was not in use. The statement details page marks the
// 2024 transaction confirmed and the 2021 one unresolved. Holdings sum the
// 2024 transaction into Meta and the 2021 one into the key's group.
const fixtures = ["standard-meta-2024.json", "standard-meta-2021.json"].map(
  (f) => path.resolve(__dirname, "..", "fixtures", f),
);
const meta = { type: IdentifierType.MIC_TICKER, domain: "XNAS", value: "META" };

// identifying lists the datasource of each identity fetch and identifier facts
// fetch under a resolve statement keys run. It leaves out corporate actions
// fetches, which new identifier facts also start there.
function identifying(resolution: UserRun): (string | undefined)[] {
  return resolution.children
    .filter((c) => c.fetch?.kind !== FetchKind.CORP_ACTS)
    .map((c) => c.fetch?.datasource);
}
const composite = "BBG000MM2P62";

test("dates a key through its ticker and marks each transaction by its day", async ({
  signIn,
  seed,
  page,
}) => {
  const { session } = await signIn();
  const { session: adminSession } = await seed("admin");
  const stmts = stmtClient(session);
  const admin = adminRunClient(adminSession);

  await page.goto("/txns");
  const runs: string[] = [];
  for (const [n, fixture] of fixtures.entries()) {
    runs.push(
      await uploadStmt(page, fixture, { state: "completed", earlier: n }),
    );
    await page.getByTestId("activity-sheet-close").click();
  }
  const [first, second] = runs;

  const read = await Promise.all(
    [first, second].map((runId) => stmts.getStmt({ runId })),
  );
  const keys = read.map((r) => r.keys);
  for (const k of keys) {
    expect(k).toHaveLength(1);
    expect(k[0].outcome).toBe(ResolutionOutcome.MATCHED);
  }
  expect(keys[1][0].statedKeyId).toBe(keys[0][0].statedKeyId);

  const marks = read.map((r) => r.txns.map((t) => t.mark));
  expect(marks).toEqual([[TxnMark.CONFIRMED], [TxnMark.UNRESOLVED]]);
  for (const [i, state] of ["confirmed", "unresolved"].entries()) {
    await page.goto(`/stmts/${[first, second][i]}`);
    const row = page.getByTestId(`txn-row-${read[i].txns[0].id}`);
    await expect(row.getByTestId("mark-chip")).toHaveAttribute(
      "data-state",
      state,
    );
  }

  // Holdings give each transaction the instrument META named on its day.
  const held = await holdingClient(session).listHoldings({});
  expect(held.instrs).toHaveLength(1);
  const [metaHolding] = held.instrs;
  expect(metaHolding.name).toMatchObject(meta);
  expect(metaHolding.quantity).toBe("10");
  const usd = metaHolding.listings.find((l) => l.currency === "USD");
  expect(usd).toBeDefined();
  // XNAS is META's primary venue, so its row names the listing.
  expect(usd!.ticker).toMatchObject({ market: "US", value: "META" });
  expect(usd!.venue).toBe("Nasdaq");
  expect(metaHolding.keys.map((k) => [k.quantity, k.listingId])).toEqual([
    ["10", usd!.id],
  ]);
  expect(held.groups).toHaveLength(1);
  const [metaGroup] = held.groups;
  expect(metaGroup.quantity).toBe("10");
  expect(metaGroup.identifiers).toEqual([expect.objectContaining(meta)]);
  expect(metaGroup.keys.map((k) => k.quantity)).toEqual(["10"]);

  await page.goto("/holdings");
  const table = page.getByTestId("holdings-table");
  const metaRow = table.getByTestId(`holding-row-${metaHolding.instrId}`);
  await expect(metaRow).toHaveAttribute("data-kind", "instr");
  await expect(metaRow).toContainText("META");
  await expect(
    metaRow.getByTestId(`holding-venue-${metaHolding.instrId}`),
  ).toHaveText("Nasdaq");
  const groupRow = table.getByTestId(`holding-row-${metaGroup.groupId}`);
  await expect(groupRow).toHaveAttribute("data-kind", "group");
  await expect(groupRow).toContainText("META");
  await expect(groupRow.getByTestId("holding-basis")).toHaveAttribute(
    "data-state",
    "unidentified",
  );

  const factsOf = async (runId: string) => {
    const got = await admin.getRun({ runId });
    // EODHD's answer joins OpenFIGI's through XNAS:META on the day of the
    // run, so no answer is dropped. The day semantics are pinned in the unit
    // and dbtest tiers; this spec checks that the real answer shapes join.
    // The facts of the value conflict nowhere.
    const conflicts = got.findings.filter(
      (f) =>
        f.kind === FindingKind.DROPPED ||
        f.kind === FindingKind.IDENTIFIER_DISAGREEMENT ||
        f.kind === FindingKind.BROKEN_TICKER_VALIDITY_BRACKET,
    );
    expect(conflicts).toHaveLength(0);
    const resolution = got.run!.children[0];
    const massive = resolution.children.filter(
      (c) =>
        c.fetch?.datasource === "massive" &&
        c.fetch.kind === FetchKind.IDENTIFIER_FACTS,
    );
    const out = [];
    for (const run of massive) {
      out.push(await fetchItems(admin, run.run!.id));
    }
    return { resolution, facts: out };
  };

  const one = await factsOf(first);
  expect(identifying(one.resolution).sort()).toEqual([
    "eodhd",
    "massive",
    "massive",
    "openfigi",
  ]);
  expect(one.facts).toHaveLength(1);
  const sent = one.facts[0].map((i) => [
    i.outcome,
    i.sent?.type,
    i.sent?.value,
  ]);
  expect(sent).toEqual(
    expect.arrayContaining([
      [FetchOutcome.SERVED, IdentifierType.OPENFIGI_COMPOSITE, composite],
      [FetchOutcome.SERVED, IdentifierType.MIC_TICKER, "META"],
    ]),
  );
  expect(sent).toHaveLength(2);

  const two = await factsOf(second);
  expect(identifying(two.resolution)).toEqual(["massive"]);
  expect(two.facts).toHaveLength(1);
  expect(
    two.facts[0].map((i) => [i.outcome, i.sent?.type, i.sent?.value]),
  ).toEqual([[FetchOutcome.SERVED, IdentifierType.MIC_TICKER, "META"]]);

  const instr = await instrListedAs(adminSession, {
    domain: "XNAS",
    value: "META",
  });
  // EODHD's answer, attached through the ticker, wrote Meta's ISIN.
  expect(instr.identifiers).toContainEqual(
    expect.objectContaining({
      type: IdentifierType.ISIN,
      value: "US30303M1027",
    }),
  );
});
