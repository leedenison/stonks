import { FetchOutcome, FindingKind } from "../gen/admin/run/v1/run_pb";
import { RunState } from "../gen/run/v1/run_pb";
import { TransactionMark } from "../gen/statement/v1/statement_pb";
import {
  AssetClass,
  Broker,
  IdentifierType,
  ResolutionOutcome,
} from "../gen/type/v1/type_pb";
import {
  adminInstrumentClient,
  adminRunClient,
  fetchItems,
  runClient,
  statementClient,
} from "../helpers/api";
import { expect, test } from "../helpers/test";

// One stock row stating META at Nasdaq, in two statements built through the
// API, since no broker export states a ticker with its venue. META began
// naming Meta on 2022-06-09, which Massive's ticker history states. The first
// statement, dated 2024, dates the key through the value and asks Massive for
// Meta's history under its composite FIGI and for the value on its date. The
// second, dated 2021-06-01, reads the facts the first wrote, skips the
// identity fetch, and asks Massive for the value on that day alone. The day
// sits before 2021-06-30, when a fund took the ticker, so Massive answers
// that META was not in use. The statement details page marks the 2024
// transaction confirmed and the 2021 one unresolved.
const meta = { type: IdentifierType.MIC_TICKER, domain: "XNAS", value: "META" };
const composite = "BBG000MM2P62";

function statement(orderFrom: string, orderBefore: string, on: string) {
  return {
    broker: Broker.IBKR,
    orderFrom,
    orderBefore,
    rows: [
      {
        key: {
          assetClass: AssetClass.STOCK,
          currency: "USD",
          identifiers: [meta],
        },
        orderDate: on,
        settlementDate: on,
        asAt: on,
        quantity: "10",
      },
    ],
  };
}

test("dates a key through its ticker and marks each transaction by its day", async ({
  signIn,
  seed,
  page,
}) => {
  const { session } = await signIn();
  const { session: adminSession } = await seed("admin");
  const statements = statementClient(session);
  const runs = runClient(session);
  const admin = adminRunClient(adminSession);

  const ingest = async (s: ReturnType<typeof statement>) => {
    const runId = (await statements.createStatement({ statement: s })).run!.id;
    await expect
      .poll(async () => (await runs.getRun({ runId })).run?.state)
      .toBe(RunState.COMPLETED);
    return runId;
  };
  const first = await ingest(
    statement("2024-01-01", "2024-02-01", "2024-01-16"),
  );
  const second = await ingest(
    statement("2021-06-01", "2021-07-01", "2021-06-01"),
  );

  const read = await Promise.all(
    [first, second].map((runId) => statements.getStatement({ runId })),
  );
  const keys = read.map((r) => r.keys);
  for (const k of keys) {
    expect(k).toHaveLength(1);
    expect(k[0].outcome).toBe(ResolutionOutcome.MATCHED);
  }
  expect(keys[1][0].statedKeyId).toBe(keys[0][0].statedKeyId);

  const marks = read.map((r) => r.transactions.map((t) => t.mark));
  expect(marks).toEqual([
    [TransactionMark.CONFIRMED],
    [TransactionMark.UNRESOLVED],
  ]);
  for (const [i, state] of ["confirmed", "unresolved"].entries()) {
    await page.goto(`/statements/${[first, second][i]}`);
    const row = page.getByTestId(
      `transaction-row-${read[i].transactions[0].id}`,
    );
    await expect(row.getByTestId("mark-chip")).toHaveAttribute(
      "data-state",
      state,
    );
  }

  // The identifier events fetch is the Massive run whose items name no key.
  const eventsOf = async (runId: string) => {
    const got = await admin.getRun({ runId });
    // EODHD's answer shares no stable identifier with OpenFIGI's, which is a
    // finding of the key. The facts of the value conflict nowhere.
    const conflicts = got.findings.filter(
      (f) =>
        f.kind === FindingKind.DISAGREEMENT ||
        f.kind === FindingKind.BROKEN_VALIDITY_BRACKET,
    );
    expect(conflicts).toHaveLength(0);
    const resolution = got.run!.children[0];
    const massive = resolution.children.filter(
      (c) => c.datasource === "massive",
    );
    const out = [];
    for (const run of massive) {
      const items = await fetchItems(admin, run.run!.id);
      if (items.every((i) => i.statedKey === undefined)) {
        out.push(items);
      }
    }
    return { resolution, events: out };
  };

  const one = await eventsOf(first);
  expect(one.resolution.children.map((c) => c.datasource).sort()).toEqual([
    "eodhd",
    "massive",
    "massive",
    "openfigi",
  ]);
  expect(one.events).toHaveLength(1);
  const sent = one.events[0].map((i) => [
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

  const two = await eventsOf(second);
  expect(two.resolution.children.map((c) => c.datasource)).toEqual(["massive"]);
  expect(two.events).toHaveLength(1);
  expect(
    two.events[0].map((i) => [i.outcome, i.sent?.type, i.sent?.value]),
  ).toEqual([[FetchOutcome.SERVED, IdentifierType.MIC_TICKER, "META"]]);

  const listed = await adminInstrumentClient(adminSession).listInstruments({
    query: "META",
  });
  const named = listed.instruments.filter((i) =>
    i.listings.some((l) =>
      l.identifiers.some(
        (id) =>
          id.type === IdentifierType.MIC_TICKER &&
          id.domain === "XNAS" &&
          id.value === "META",
      ),
    ),
  );
  expect(named).toHaveLength(1);
});
