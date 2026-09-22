import path from "node:path";
import type { Holding } from "../gen/holding/v1/holding_pb";
import { AssetClass } from "../gen/type/v1/type_pb";
import { holdingClient } from "../helpers/api";
import { expect, test } from "../helpers/test";

// The fixture is a copy of the client's Fidelity UK test export, modelled on
// a real export with its identifiers replaced. Its eleven rows state three
// securities and the cash that paid for them. A security key states no
// identifier resolution admits, so it is answered by nothing and is no
// holding of an instrument; the cash keys state a currency identifier, which
// names the currency instrument. The API states the quantity exactly and the
// page shows it to two places.
const fixture = path.resolve(__dirname, "..", "fixtures", "fidelity-uk.csv");
const expected = [
  {
    name: "GBP",
    quantity: "12092.79",
    shown: "12092.79",
    assetClass: AssetClass.CASH,
    className: "Cash",
  },
];

// byName finds the holding one of whose identifiers is name.
function byName(holdings: Holding[], name: string): Holding {
  const found = holdings.find((h) =>
    h.identifiers.some((i) => i.value === name),
  );
  if (!found) {
    throw new Error(`no holding named ${name}`);
  }
  return found;
}

test("uploads a statement and lists the holdings it produces", async ({
  signIn,
  seed,
  page,
}) => {
  const { session } = await signIn();
  await page.goto("/transactions");
  await page.getByTestId("upload-statement").click();
  await page.getByTestId("upload-file").setInputFiles(fixture);
  await expect(page.getByTestId("upload-rows")).toHaveText("11 rows");
  await page.getByTestId("upload-submit").click();

  // The run completes in the transaction that writes the rows.
  const item = page
    .getByTestId("activity-sheet")
    .getByTestId(/^activity-item-/)
    .first();
  await expect(item.getByTestId("state-chip")).toHaveAttribute(
    "data-state",
    "completed",
  );

  // The data behind the page.
  const { holdings } = await holdingClient(session).listHoldings({});
  expect(holdings).toHaveLength(expected.length);
  for (const e of expected) {
    const h = byName(holdings, e.name);
    expect(h.quantity, e.name).toBe(e.quantity);
    expect(h.assetClass, e.name).toBe(e.assetClass);
  }

  // The page shows each holding, in order. The sheet covers the sidebar
  // until it is closed.
  await page.getByTestId("activity-sheet-close").click();
  await page.getByTestId("nav-holdings").click();
  await expect(page).toHaveURL("/holdings");
  const table = page.getByTestId("holdings-table");
  for (const e of expected) {
    const id = byName(holdings, e.name).instrumentId;
    const row = table.getByTestId(`holding-row-${id}`);
    await expect(row).toContainText(e.name);
    await expect(row).toContainText(e.className);
    await expect(row.getByTestId(`holding-qty-${id}`)).toHaveText(e.shown);
  }
  const ids = await table
    .getByTestId(/^holding-row-/)
    .evaluateAll((rows) => rows.map((r) => r.getAttribute("data-testid")));
  expect(ids).toEqual(
    expected.map((e) => `holding-row-${byName(holdings, e.name).instrumentId}`),
  );

  // Another user holds none of them.
  const other = await seed();
  const theirs = await holdingClient(other.session).listHoldings({});
  expect(theirs.holdings).toHaveLength(0);
});
