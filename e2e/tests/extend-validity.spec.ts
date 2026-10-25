import { timestampDate } from "@bufbuild/protobuf/wkt";
import { FetchOutcome } from "../gen/admin/run/v1/run_pb";
import { RunState } from "../gen/run/v1/run_pb";
import {
  AssetClass,
  Broker,
  IdentifierType,
  ResolutionOutcome,
} from "../gen/type/v1/type_pb";
import {
  adminInstrumentClient,
  adminRunClient,
  assertionItems,
  coverageClient,
  extendValidityClient,
  fetchItems,
  runClient,
  statementClient,
} from "../helpers/api";
import { expect, test } from "../helpers/test";

// The ticker the spec states. No other spec states PEP, so no other user
// holds it.
const pep = { type: IdentifierType.MIC_TICKER, domain: "XNAS", value: "PEP" };

// utcDay is the UTC day of a moment, as YYYY-MM-DD.
const utcDay = (d: Date) => d.toISOString().slice(0, 10);

test("extends the validity of a held ticker on the day it runs", async ({
  seed,
}) => {
  const { session } = await seed();
  const { session: adminSession } = await seed("admin");
  const statements = statementClient(session);
  const runs = runClient(session);
  const adminRuns = adminRunClient(adminSession);

  const on = "2024-01-16";
  // Built through the API, since no broker export states a ticker with its
  // venue.
  const runId = (
    await statements.createStatement({
      statement: {
        broker: Broker.IBKR,
        orderFrom: "2024-01-01",
        orderBefore: "2024-02-01",
        rows: [
          {
            key: {
              assetClass: AssetClass.STOCK,
              currency: "USD",
              identifiers: [pep],
            },
            orderDate: on,
            settlementDate: on,
            asAt: on,
            quantity: "10",
          },
        ],
      },
    })
  ).run!.id;
  await expect
    .poll(async () => (await runs.getRun({ runId })).run?.state)
    .toBe(RunState.COMPLETED);
  const keys = (await statements.getStatement({ runId })).keys;
  expect(keys.map((k) => k.outcome)).toEqual([ResolutionOutcome.MATCHED]);

  const listed = await adminInstrumentClient(adminSession).listInstruments({
    query: "PEP",
  });
  const named = listed.instruments.filter((i) =>
    i.listings.some((l) =>
      l.identifiers.some(
        (id) =>
          id.type === pep.type &&
          id.domain === pep.domain &&
          id.value === pep.value,
      ),
    ),
  );
  expect(named).toHaveLength(1);
  // A past attestation gives the run a bracket to extend. No surface shows
  // validity, so the resolve dbtest asserts the bracket.
  await coverageClient(adminSession).attest({
    ticker: pep,
    names: { case: "instrumentId", value: named[0].id },
    from: "2025-01-02",
  });

  const started = (
    await extendValidityClient(adminSession).startExtendValidity({})
  ).run!;
  await expect
    .poll(
      async () =>
        (await adminRuns.getRun({ runId: started.id })).run?.run?.state,
    )
    .toBe(RunState.COMPLETED);
  const got = (await adminRuns.getRun({ runId: started.id })).run!;
  const from = utcDay(timestampDate(got.run!.startedAt!));
  const to = utcDay(timestampDate(got.run!.finishedAt!));

  // A ticker history also asserts the ticker on the day it began naming the
  // instrument, so the present assertions are those dated while the run ran.
  const present = (await assertionItems(adminRuns, started.id)).filter(
    (a) =>
      a.ticker?.domain === pep.domain &&
      a.ticker?.value === pep.value &&
      a.assertedOn >= from &&
      a.assertedOn <= to,
  );
  expect(present.length).toBeGreaterThan(0);
  for (const a of present) {
    expect(a.instrumentId, a.source.value).toBe(named[0].id);
  }

  // The statement's fetches filled the cache. Each served key here has
  // attempts, so the run called the datasource instead of reading
  // the cache.
  expect(got.children.length).toBeGreaterThan(0);
  for (const child of got.children) {
    const served = (await fetchItems(adminRuns, child.run!.id)).filter(
      (i) => i.outcome === FetchOutcome.SERVED,
    );
    for (const item of served) {
      expect(item.attempts, child.datasource).toBeGreaterThan(0);
    }
  }
});
