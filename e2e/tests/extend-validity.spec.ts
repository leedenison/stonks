import path from "node:path";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { FetchOutcome } from "../gen/admin/run/v1/run_pb";
import { RunState } from "../gen/run/v1/run_pb";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  adminInstrumentClient,
  adminRunClient,
  assertionItems,
  coverageClient,
  extendValidityClient,
  fetchItems,
  statementClient,
} from "../helpers/api";
import { expect, test } from "../helpers/test";
import { uploadStatement } from "../helpers/upload";

// No other spec states PEP, so no other user holds it.
const fixture = path.resolve(__dirname, "..", "fixtures", "standard-pep.json");
const pep = { type: IdentifierType.MIC_TICKER, domain: "XNAS", value: "PEP" };

// utcDay is the UTC day of a moment, as YYYY-MM-DD.
const utcDay = (d: Date) => d.toISOString().slice(0, 10);

test("extends the validity of a held ticker on the day it runs", async ({
  signIn,
  seed,
  page,
}) => {
  const { session } = await signIn();
  const { session: adminSession } = await seed("admin");
  const statements = statementClient(session);
  const adminRuns = adminRunClient(adminSession);

  await page.goto("/transactions");
  const runId = await uploadStatement(page, fixture, { state: "completed" });
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
