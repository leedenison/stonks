import path from "node:path";
import {
  Arbiter,
  IdentifierType,
  ResolutionOutcome,
} from "../gen/type/v1/type_pb";
import { holdingClient, statementClient } from "../helpers/api";
import { uploadStatement } from "../helpers/upload";
import { expect, test } from "../helpers/test";

// The fixture is one IBKR line modelled on a real IBKR export with its
// identifiers replaced. Its security has only a contract id and the ticker
// CSCO, which no other spec uses, so the key reaches the datasources by the
// ticker alone.
const fixture = path.resolve(__dirname, "..", "fixtures", "ibkr-choose.qfx");
const ticker = "CSCO";

test("chooses an instrument for an IBKR security stated by ticker", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn();
  await page.goto("/transactions");
  const runId = await uploadStatement(page, fixture, {
    state: "completed",
    rows: "3 rows",
  });
  await page.getByTestId("activity-sheet-close").click();

  const statements = statementClient(session);
  const key = (await statements.getStatement({ runId })).keys.find((k) =>
    k.statedKey?.identifiers.some(
      (i) => i.type === IdentifierType.MIC_TICKER && i.value === ticker,
    ),
  );
  expect(key?.outcome).toBe(ResolutionOutcome.UNRECOGNISED);
  const keyId = key!.statedKeyId;

  await page.goto(`/statements/${runId}`);
  const row = page.getByTestId(`key-row-${keyId}`);
  await page.getByTestId(`key-choose-${keyId}`).click();
  const dialog = page.getByTestId("candidates-dialog");
  const offered = dialog.getByTestId(/^candidate-row-openfigi-/).first();
  await expect(offered).toContainText(ticker);
  await offered.getByTestId(/^candidate-choose-openfigi-/).click();
  await expect(dialog).toBeHidden();
  await expect(row.getByTestId("resolution-chip")).toHaveAttribute(
    "data-state",
    "matched",
  );
  await expect(row.getByTestId(`key-arbiter-${keyId}`)).toHaveText("Chosen");

  const after = (await statements.getStatement({ runId })).keys.find(
    (k) => k.statedKeyId === keyId,
  );
  expect(after?.arbiter).toBe(Arbiter.USER);
  const holdings = await holdingClient(session).listHoldings({});
  expect(holdings.groups).toHaveLength(0);
  const shares = holdings.instruments.find((h) =>
    h.identifiers.some(
      (i) => i.type === IdentifierType.MIC_TICKER && i.value === ticker,
    ),
  );
  expect(shares?.quantity).toBe("10");
});
