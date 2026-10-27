import path from "node:path";
import { RunKind } from "../gen/run/v1/run_pb";
import { TransactionMark } from "../gen/statement/v1/statement_pb";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  instrumentListedAs,
  adminRunClient,
  assertionItems,
  statementClient,
} from "../helpers/api";
import { deleteRuns } from "../helpers/db";
import { expect, test } from "../helpers/test";
import { uploadStatement } from "../helpers/upload";

// WMT has named one instrument since before Massive's history begins on
// 2003-09-10. No other spec states WMT, because an identifier attestation is a
// system fact that every user reads.
const fixture = path.resolve(__dirname, "..", "fixtures", "standard-wmt.json");
const wmt = { type: IdentifierType.MIC_TICKER, domain: "XNYS", value: "WMT" };
// The transaction's day falls before the history window, so the ticker's
// validity on it is provisional and the day lies in a gap.
const on = "2003-01-15";

// The attest identifier runs this test started.
const started: string[] = [];
test.afterEach(async () => {
  await deleteRuns(started.splice(0));
});

test("attests a gap from the instrument's tickers tab and withdraws it", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn("admin");
  const statements = statementClient(session);
  const adminRuns = adminRunClient(session);

  await page.goto("/transactions");
  const runId = await uploadStatement(page, fixture, { state: "completed" });
  const read = await statements.getStatement({ runId });
  expect(read.keys.map((k) => k.outcome)).toEqual([ResolutionOutcome.MATCHED]);
  expect(read.keys[0].ticker).toMatchObject(wmt);
  expect(read.transactions.map((t) => t.mark)).toEqual([
    TransactionMark.PROVISIONAL,
  ]);
  const txId = read.transactions[0].id;

  // The statement page marks the transaction provisional.
  await page.goto(`/statements/${runId}`);
  const row = page.getByTestId(`transaction-row-${txId}`);
  await expect(row.getByTestId("mark-chip")).toHaveAttribute(
    "data-state",
    "provisional",
  );

  // The instrument's tickers tab opens on WMT, lists the gap before the
  // window, and attests from it with the day and the instrument
  // preselected. Other gaps follow the clock, so the spec addresses rows by
  // id alone.
  const instrument = await instrumentListedAs(session, wmt);
  await page.goto(`/admin/instruments?q=WMT&id=${instrument.id}&tab=tickers`);
  await expect(page.getByTestId("ticker-select")).toHaveValue("US:WMT");
  const gap = page.getByTestId(`pane-gap-${on}`);
  await expect(gap).toContainText("2003-09-10");
  await page.getByTestId(`gap-attest-${on}`).click();
  const dialog = page.getByTestId("attest-dialog");
  await expect(dialog.getByTestId("attest-from")).toHaveValue(on);
  await expect(dialog.getByTestId("attest-names")).toBeChecked();
  await expect(dialog.getByTestId("attest-start")).toBeEnabled();
  await dialog.getByTestId("attest-start").click();
  await expect(dialog).toBeHidden();

  const attested = page.locator(
    "[data-testid^='assertion-row-'][data-attested='true']",
  );
  await expect(attested).toHaveCount(1);
  await expect(attested).toContainText(on);
  await expect(attested).toContainText("@example.com");

  // The attest identifier run is the one whose assertion names WMT, which no
  // other spec attests.
  const listed = await adminRuns.listRuns({ kind: RunKind.ATTEST_IDENTIFIER });
  for (const r of listed.runs) {
    const items = await assertionItems(adminRuns, r.run!.id);
    if (items.some((a) => a.ticker?.value === wmt.value)) {
      started.push(r.run!.id);
    }
  }
  expect(started).toHaveLength(1);

  // The identifier attestation brackets the provider's first assertion, so the
  // transaction is confirmed.
  expect((await statements.getStatement({ runId })).transactions[0].mark).toBe(
    TransactionMark.CONFIRMED,
  );

  const id = (await attested.getAttribute("data-testid"))!.replace(
    "assertion-row-",
    "",
  );
  await page.getByTestId(`assertion-withdraw-${id}`).click();
  await page.getByTestId("withdraw-confirm").click();
  await expect(page.getByTestId("withdraw-dialog")).toBeHidden();
  await expect(attested).toHaveCount(0);
  expect((await statements.getStatement({ runId })).transactions[0].mark).toBe(
    TransactionMark.PROVISIONAL,
  );
});
