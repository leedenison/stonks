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

// utcDay is the UTC day of a moment, as YYYY-MM-DD.
const utcDay = (d: Date) => d.toISOString().slice(0, 10);

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

  for (const req of [
    { instrId: ibm.instrId, from: "2024-01-10" },
    { instrId: orcl.instrId, from: "2025-01-02", to: utcDay(new Date()) },
  ]) {
    const run = (
      await coverage.attestIdentifier({
        ticker: ibmUS,
        names: { case: "instrId", value: req.instrId },
        from: req.from,
        to: req.to,
      })
    ).run!;
    started.push(run.id);
    await stopped(adminRuns, run.id);
  }

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
