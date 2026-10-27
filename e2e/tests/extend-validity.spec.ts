import path from "node:path";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { FetchKind } from "../gen/admin/block/v1/block_pb";
import { FetchOutcome } from "../gen/admin/run/v1/run_pb";
import { RunState } from "../gen/run/v1/run_pb";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  instrumentListedAs,
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
// The facts of PEP are keyed by its market.
const pepUS = { market: "US", value: pep.value };

// utcDay is the UTC day of a moment, as YYYY-MM-DD.
const utcDay = (d: Date) => d.toISOString().slice(0, 10);

test("extends the validity of a held ticker on the day it runs", async ({
  signIn,
  page,
}) => {
  // The signed-in user is an administrator, so the spec can also read the
  // instrument's tickers tab.
  const { session } = await signIn("admin");
  const adminSession = session;
  const statements = statementClient(session);
  const adminRuns = adminRunClient(adminSession);

  await page.goto("/transactions");
  const runId = await uploadStatement(page, fixture, { state: "completed" });
  const keys = (await statements.getStatement({ runId })).keys;
  expect(keys.map((k) => k.outcome)).toEqual([ResolutionOutcome.MATCHED]);

  const instrument = await instrumentListedAs(adminSession, pep);
  // A past attestation gives the run a bracket to extend.
  await coverageClient(adminSession).attest({
    ticker: pepUS,
    names: { case: "instrumentId", value: instrument.id },
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

  // Validity runs from the attested day through the run day.
  await page.goto(`/admin/instruments?q=PEP&id=${instrument.id}&tab=tickers`);
  await expect(page.getByTestId("ticker-select")).toHaveValue("US:PEP");
  const confirmed = page.locator(
    '[data-lane="validity"] rect[data-validity="confirmed"] title',
  );
  await expect
    .poll(async () =>
      (await confirmed.allTextContents()).some((t) => {
        const [since, before] = t.replace("Confirmed ", "").split(" to ");
        return since <= "2025-01-02" && before > to;
      }),
    )
    .toBe(true);

  // A ticker history also asserts the ticker on the day it began naming the
  // instrument, so the present assertions are those dated while the run ran.
  const present = (await assertionItems(adminRuns, started.id)).filter(
    (a) =>
      a.ticker?.market === pepUS.market &&
      a.ticker?.value === pepUS.value &&
      a.assertedOn >= from &&
      a.assertedOn <= to,
  );
  expect(present.length).toBeGreaterThan(0);
  for (const a of present) {
    expect(a.instrumentId, a.source.value).toBe(instrument.id);
  }

  // The statement's fetches filled the cache. Each served key of the run's
  // identity and identifier events fetches has attempts, so the run called
  // the datasource. The run's new identifier facts start corporate actions
  // fetches. Those fetches read the cache.
  const extending = got.children.filter(
    (c) => c.fetch?.kind !== FetchKind.CORP_ACTS,
  );
  expect(extending.length).toBeGreaterThan(0);
  for (const child of extending) {
    const served = (await fetchItems(adminRuns, child.run!.id)).filter(
      (i) => i.outcome === FetchOutcome.SERVED,
    );
    for (const item of served) {
      expect(item.attempts, child.fetch?.datasource).toBeGreaterThan(0);
    }
  }
});
