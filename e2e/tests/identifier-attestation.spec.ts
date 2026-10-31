import path from "node:path";
import { Code, ConnectError } from "@connectrpc/connect";
import { AlertKind } from "../gen/admin/alert/v1/alert_pb";
import { RunKind } from "../gen/run/v1/run_pb";
import { TxnMark } from "../gen/stmt/v1/stmt_pb";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  instrListedAs,
  adminRunClient,
  alertClient,
  alertPageToken,
  alertsAddress,
  assertionItems,
  holdingClient,
  stmtClient,
} from "../helpers/api";
import { deleteRuns } from "../helpers/db";
import { expect, test } from "../helpers/test";
import { uploadStmt } from "../helpers/upload";

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
  const stmts = stmtClient(session);
  const adminRuns = adminRunClient(session);

  await page.goto("/txns");
  const runId = await uploadStmt(page, fixture, { state: "completed" });
  const read = await stmts.getStmt({ runId });
  expect(read.keys.map((k) => k.outcome)).toEqual([ResolutionOutcome.MATCHED]);
  // The key is dated through WMT, so its holding sums it through the
  // listing that WMT names.
  const holdings = await holdingClient(session).listHoldings({});
  const through = holdings.instrs.flatMap((h) =>
    h.keys.map((k) => h.listings.find((l) => l.id === k.listingId)?.ticker),
  );
  expect(through).toContainEqual(
    expect.objectContaining({ market: "US", value: wmt.value }),
  );
  expect(read.txns.map((t) => t.mark)).toEqual([TxnMark.PROVISIONAL]);
  const txnId = read.txns[0].id;

  // The statement page marks the transaction provisional.
  await page.goto(`/stmts/${runId}`);
  const row = page.getByTestId(`txn-row-${txnId}`);
  await expect(row.getByTestId("mark-chip")).toHaveAttribute(
    "data-state",
    "provisional",
  );

  // The instrument's tickers tab opens on WMT and lists the gap before the
  // window. Other gaps follow the clock, so the spec addresses rows by id
  // alone.
  const instr = await instrListedAs(session, wmt);
  await page.goto(`/admin/instrs?q=WMT&id=${instr.id}&tab=tickers`);
  await expect(page.getByTestId("ticker-select")).toHaveValue("US:WMT");
  const gap = page.getByTestId(`pane-gap-${on}`);
  await expect(gap).toContainText("2003-09-10");

  // Other specs change the alerts, so the spec polls for the page that holds this one.
  const alerts = alertClient(session);
  let token: string | undefined;
  await expect
    .poll(async () => {
      token = await alertPageToken(
        alerts,
        AlertKind.TICKER_VALIDITY_GAP,
        (a) =>
          a.subject.case === "validityGap" &&
          a.subject.value.ticker?.value === wmt.value &&
          a.subject.value.gap?.period?.from === on,
      );
      return token;
    })
    .not.toBeUndefined();
  await page.goto(alertsAddress("ticker_validity_gap", token!));
  const alert = page.getByTestId(
    `alert-row-ticker_validity_gap-US-${wmt.value}-${on}`,
  );
  await alert
    .getByTestId(`alert-act-ticker_validity_gap-US-${wmt.value}-${on}`)
    .click();
  const dialog = page.getByTestId("attest-dialog");
  await expect(dialog.getByTestId("attest-from")).toHaveValue(on);
  await expect(dialog.getByTestId("attest-to")).toHaveValue("2003-09-09");
  await expect(dialog.getByTestId("attest-names")).toBeChecked();
  await expect(dialog.getByTestId("attest-start")).toBeEnabled();
  await dialog.getByTestId("attest-start").click();
  await expect(dialog).toBeHidden();
  await expect(alert).toHaveCount(0);

  await page.goto(`/admin/instrs?q=WMT&id=${instr.id}&tab=tickers`);
  const attested = page.locator(
    "[data-testid^='assertion-row-'][data-attested='true']",
  );
  await expect(attested).toHaveCount(1);
  await expect(attested).toContainText(on);
  await expect(attested).toContainText("@example.com");

  // The attest identifier run is the one whose assertion names WMT, which no
  // other spec attests. Another spec may delete its own runs between the
  // listing and the read, so a run that is gone is skipped.
  const listed = await adminRuns.listRuns({ kind: RunKind.ATTEST_IDENTIFIER });
  for (const r of listed.runs) {
    const items = await assertionItems(adminRuns, r.run!.id).catch((e) => {
      if (ConnectError.from(e).code === Code.NotFound) {
        return [];
      }
      throw e;
    });
    if (items.some((a) => a.ticker?.value === wmt.value)) {
      started.push(r.run!.id);
    }
  }
  expect(started).toHaveLength(1);

  // The identifier attestation and the provider's first assertion bracket the
  // days between, so the transaction is confirmed.
  expect((await stmts.getStmt({ runId })).txns[0].mark).toBe(TxnMark.CONFIRMED);

  const id = (await attested.getAttribute("data-testid"))!.replace(
    "assertion-row-",
    "",
  );
  await page.getByTestId(`assertion-withdraw-${id}`).click();
  await page.getByTestId("withdraw-confirm").click();
  await expect(page.getByTestId("withdraw-dialog")).toBeHidden();
  await expect(attested).toHaveCount(0);
  expect((await stmts.getStmt({ runId })).txns[0].mark).toBe(
    TxnMark.PROVISIONAL,
  );
});
