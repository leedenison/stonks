import path from "node:path";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  holdingClient,
  instrumentClient,
  statementClient,
} from "../helpers/api";
import { uploadStatement } from "../helpers/upload";
import { expect, test } from "../helpers/test";

// One line at each of two brokers, derived from the client's IBKR and Schwab
// test exports, which are modelled on real exports with their identifiers
// replaced. Both buy ten Microsoft shares at 400 with a commission of 1.
// IBKR states the CUSIP, which the provider resolves to the instrument, so
// that key's holding is the instrument's. Schwab states a ticker with no
// venue, so nothing associates it with an instrument and that key stays a
// holding of its own resting on the statement alone. The dollar cash of both
// is one instrument holding summed across the brokers.
const ibkr = path.resolve(__dirname, "..", "fixtures", "ibkr-cross.qfx");
const schwab = path.resolve(__dirname, "..", "fixtures", "schwab-cross.csv");
const cusip = "594918104";
const ticker = "MSFT";
const description = "MICROSOFT CORPORATION";

test("shows a resolved holding and an unresolved one from two brokers", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn();
  await page.goto("/transactions");

  const runs: string[] = [];
  for (const [n, fixture] of [ibkr, schwab].entries()) {
    runs.push(
      await uploadStatement(page, fixture, {
        state: "completed",
        earlier: n,
        rows: "3 rows",
      }),
    );
    await page.getByTestId("activity-sheet-close").click();
  }
  const [ibkrRun, schwabRun] = runs;

  // The data behind the pages.
  const holdings = await holdingClient(session).listHoldings({});
  expect(holdings.instruments).toHaveLength(2);
  const cash = holdings.instruments.find((h) =>
    h.identifiers.some((i) => i.value === "USD"),
  );
  expect(cash?.quantity).toBe("-8002");
  const shares = holdings.instruments.find((h) =>
    h.identifiers.some((i) => i.value === cusip),
  );
  expect(shares?.quantity).toBe("10");
  const types = shares!.identifiers.map((i) => i.type);
  expect(types).toContain(IdentifierType.OPENFIGI_SHARE_CLASS);
  expect(
    shares!.identifiers.some(
      (i) => i.type === IdentifierType.MIC_TICKER && i.value === ticker,
    ),
  ).toBe(true);
  expect(holdings.groups).toHaveLength(1);
  const group = holdings.groups[0];
  expect(group.quantity).toBe("10");
  expect(group.identifiers.map((i) => [i.type, i.value])).toEqual([
    [IdentifierType.BROKER_DESCRIPTION, description],
    [IdentifierType.MIC_TICKER, ticker],
  ]);

  const instruments = await instrumentClient(session).listInstruments({});
  const instrument = instruments.instruments.find((i) =>
    i.identifiers.some((x) => x.value === cusip),
  );
  expect(instrument?.identifiers.map((i) => i.type)).toContain(
    IdentifierType.OPENFIGI_SHARE_CLASS,
  );
  const usd = instrument!.listings.find((l) => l.currency === "USD");
  expect(usd?.identifiers.map((i) => i.type)).toContain(
    IdentifierType.OPENFIGI_COMPOSITE,
  );

  // What became of each key.
  const statements = statementClient(session);
  const ibkrKeys = (await statements.getStatement({ runId: ibkrRun })).keys;
  expect(ibkrKeys).toHaveLength(2);
  expect(ibkrKeys.map((k) => k.outcome)).toEqual([
    ResolutionOutcome.MATCHED,
    ResolutionOutcome.MATCHED,
  ]);
  const schwabKeys = (await statements.getStatement({ runId: schwabRun })).keys;
  expect(schwabKeys).toHaveLength(2);
  const tickerKey = schwabKeys.find((k) =>
    k.statedKey?.identifiers.some((i) => i.value === ticker),
  );
  expect(tickerKey?.outcome).toBe(ResolutionOutcome.UNRECOGNISED);
  const dollarKey = schwabKeys.find((k) => k !== tickerKey);
  expect(dollarKey?.outcome).toBe(ResolutionOutcome.MATCHED);

  // The pages.
  await page.getByTestId("nav-holdings").click();
  await expect(page).toHaveURL("/holdings");
  const resolved = page.getByTestId(`holding-row-${shares!.instrumentId}`);
  await expect(resolved).toHaveAttribute("data-kind", "instrument");
  await expect(resolved).toContainText(ticker);
  await expect(resolved).toContainText(cusip);
  await expect(resolved.getByTestId("holding-basis")).toHaveCount(0);
  const unresolved = page.getByTestId(`holding-row-${group.groupId}`);
  await expect(unresolved).toHaveAttribute("data-kind", "group");
  await expect(unresolved).toContainText(description);
  await expect(unresolved).toContainText(ticker);
  await expect(unresolved.getByTestId("holding-basis")).toHaveAttribute(
    "data-state",
    "unidentified",
  );
  await expect(
    page.getByTestId(`holding-qty-${cash!.instrumentId}`),
  ).toHaveText("-8002.00");

  await page.getByTestId("nav-instruments").click();
  await expect(page).toHaveURL("/instruments");
  const row = page.getByTestId(`instrument-row-${instrument!.id}`);
  await expect(row).toContainText(ticker);
  await expect(row).toContainText(cusip);
  await expect(row).toContainText("USD");

  await page.goto(`/statements/${schwabRun}`);
  await expect(
    page
      .getByTestId(`key-row-${tickerKey!.statedKeyId}`)
      .getByTestId("resolution-chip"),
  ).toHaveAttribute("data-state", "unrecognised");
});
