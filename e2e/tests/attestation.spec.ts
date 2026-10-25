import { RunKind, RunState } from "../gen/run/v1/run_pb";
import { TransactionMark } from "../gen/statement/v1/statement_pb";
import {
  AssetClass,
  Broker,
  IdentifierType,
  ResolutionOutcome,
} from "../gen/type/v1/type_pb";
import {
  adminRunClient,
  assertionItems,
  runClient,
  statementClient,
} from "../helpers/api";
import { deleteRuns } from "../helpers/db";
import { expect, test } from "../helpers/test";

// WMT has named one instrument since before Massive's history begins on
// 2003-09-10. No other spec states WMT, because an attestation is a system
// fact that every user reads.
const wmt = { type: IdentifierType.MIC_TICKER, domain: "XNYS", value: "WMT" };
// The transaction's day falls before the history window, so the ticker's
// validity on it is provisional and the day lies in a gap.
const on = "2003-01-15";

// The attestation runs this test started.
const started: string[] = [];
test.afterEach(async () => {
  await deleteRuns(started.splice(0));
});

test("attests a gap from the coverage page and withdraws it", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn("admin");
  const statements = statementClient(session);
  const runs = runClient(session);
  const adminRuns = adminRunClient(session);

  // Built through the API, since no broker export states a ticker with its
  // venue.
  const runId = (
    await statements.createStatement({
      statement: {
        broker: Broker.IBKR,
        orderFrom: "2003-01-01",
        orderBefore: "2003-02-01",
        rows: [
          {
            key: {
              assetClass: AssetClass.STOCK,
              currency: "USD",
              identifiers: [wmt],
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
  const read = await statements.getStatement({ runId });
  expect(read.keys.map((k) => k.outcome)).toEqual([ResolutionOutcome.MATCHED]);
  expect(read.keys[0].ticker).toMatchObject(wmt);
  expect(read.transactions.map((t) => t.mark)).toEqual([
    TransactionMark.PROVISIONAL,
  ]);
  const txId = read.transactions[0].id;

  // The statement page offers the attestation beside the provisional
  // transaction.
  await page.goto(`/statements/${runId}`);
  const row = page.getByTestId(`transaction-row-${txId}`);
  await expect(row.getByTestId("mark-chip")).toHaveAttribute(
    "data-state",
    "provisional",
  );
  await expect(row.getByTestId(`transaction-attest-${txId}`)).toBeVisible();

  // The coverage page lists the gap before the window, and attests from it
  // with the day and the instrument preselected. Other gaps follow the
  // clock, so the spec addresses rows by id alone.
  await page.goto(`/admin/coverage?domain=XNYS&value=WMT`);
  const gap = page.getByTestId(`gap-row-WMT-${on}`);
  await expect(gap).toHaveAttribute("aria-selected", "true");
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

  // The attestation run is the one whose assertion names WMT, which no other
  // spec attests.
  const listed = await adminRuns.listRuns({ kind: RunKind.ATTESTATION });
  for (const r of listed.runs) {
    const items = await assertionItems(adminRuns, r.run!.id);
    if (items.some((a) => a.ticker?.value === wmt.value)) {
      started.push(r.run!.id);
    }
  }
  expect(started).toHaveLength(1);

  // The attestation brackets the provider's first assertion, so the
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
