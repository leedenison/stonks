import path from "node:path";
import { ResolutionOutcome, TickerRelation } from "../gen/type/v1/type_pb";
import {
  adminRunClient,
  coverageClient,
  holdingClient,
  stopped,
  stmtClient,
} from "../helpers/api";
import { deleteRuns } from "../helpers/db";
import { utcDay } from "../helpers/day";
import { expect, test } from "../helpers/test";
import { uploadStmt } from "../helpers/upload";

// The fixture states IBM and Oracle by ISIN, so each key is fixed and each
// holding is an instrument. No other spec states either, because an
// identifier attestation is a system fact that every user reads. An
// administrator attests that US:IBM named IBM on a day of 2024, and that it
// names Oracle from 2025 to today. IBM's own answers state its venue and
// currency, so its USD listing is still named by IBM. Oracle's own answers
// name it ORCL today too; the tie between the two open tickers goes to the
// lower value, so IBM names Oracle now.
const fixture = path.resolve(
  __dirname,
  "..",
  "fixtures",
  "standard-defunct.json",
);
const ibmISIN = "US4592001014";
const orclISIN = "US68389X1054";
const ibmUS = { market: "US", value: "IBM" };

// The attest identifier runs this test started.
const started: string[] = [];
test.afterEach(async () => {
  await deleteRuns(started.splice(0));
});

test("shows a ticker that now names another instrument as defunct on the holding it named before", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn("admin");
  const holdings = holdingClient(session);
  const coverage = coverageClient(session);
  const adminRuns = adminRunClient(session);

  await page.goto("/txns");
  const runId = await uploadStmt(page, fixture, { state: "completed" });
  const keys = (await stmtClient(session).getStmt({ runId })).keys;
  expect(keys.map((k) => k.outcome)).toEqual([
    ResolutionOutcome.MATCHED,
    ResolutionOutcome.MATCHED,
  ]);

  const held = (await holdings.listHoldings({})).instrs;
  const holdingOf = (isin: string) =>
    held.find((h) => h.identifiers.some((i) => i.value === isin))!;
  const ibm = holdingOf(ibmISIN);
  const orcl = holdingOf(orclISIN);

  const attest = async (instrId: string, from: string, to?: string) => {
    const run = (
      await coverage.attestIdentifier({
        ticker: ibmUS,
        names: { case: "instrId", value: instrId },
        from,
        to,
      })
    ).run!;
    started.push(run.id);
    await stopped(adminRuns, run.id);
    return run.id;
  };
  const first = await attest(ibm.instrId, "2024-01-10");
  // Today is the server's day, read from the run it just finished.
  const today = utcDay(
    (await adminRuns.getRun({ runId: first })).run!.run!.finishedAt!,
  );
  await attest(orcl.instrId, "2025-01-02", today);

  // Over Connect, IBM's USD listing is named by IBM, which is defunct.
  await expect
    .poll(async () => {
      const h = (await holdings.listHoldings({})).instrs.find(
        (i) => i.instrId === ibm.instrId,
      );
      const usd = h?.listings.find((l) => l.currency === "USD");
      return [usd?.ticker?.market, usd?.ticker?.value, usd?.relation];
    })
    .toEqual(["US", "IBM", TickerRelation.DEFUNCT]);

  // The holdings page marks it.
  await page.goto("/holdings");
  await expect(page.getByTestId(`holding-row-${ibm.instrId}`)).toContainText(
    "IBM (defunct)",
  );
});
