import path from "node:path";
import type { ListHoldingsResponse } from "../gen/holding/v1/holding_pb";
import { holdingClient } from "../helpers/api";
import { expect, test } from "../helpers/test";

// The fixture is a copy of the client's Fidelity UK test export, modelled on
// a real export with its identifiers replaced. Its eleven rows sum to these
// holdings, listed as the page orders them: cash first, then by name. The
// cash keys state a currency identifier, which names the currency
// instrument. Each security key states a description and at most a ticker
// with no venue, which names nothing, so each is unresolved and is a holding
// of its keys alone. The API states each quantity exactly and the page shows
// it to two places.
const fixture = path.resolve(__dirname, "..", "fixtures", "fidelity-uk.csv");
const expected = [
  {
    name: "GBP",
    quantity: "12092.79",
    shown: "12092.79",
    className: "Cash",
    kind: "instrument",
  },
  {
    name: "BAE SYSTEMS, ORD GBP0.025 (BA.)",
    quantity: "120",
    shown: "120.00",
    className: "Security",
    kind: "group",
  },
  {
    name: "Baillie Gifford Responsible Global Equity Income B Inc",
    quantity: "19.26",
    shown: "19.26",
    className: "Security",
    kind: "group",
  },
  {
    name: "VANGUARD FUNDS PLC, S&P 500 UCITS ETF USD DIS (VUSA)",
    quantity: "-141",
    shown: "-141.00",
    className: "Security",
    kind: "group",
  },
];

// rowOf finds the holding named name and the id its row carries: an
// instrument holding by an identifier naming it, a group by a description
// one of its brokers gave the line.
function rowOf(
  res: ListHoldingsResponse,
  name: string,
): { id: string; quantity: string } {
  const instrument = res.instruments.find((h) =>
    h.identifiers.some((i) => i.value === name),
  );
  if (instrument) {
    return { id: instrument.instrumentId, quantity: instrument.quantity };
  }
  const group = res.groups.find((g) =>
    g.descriptions.some((d) => d.text === name),
  );
  if (!group) {
    throw new Error(`no holding named ${name}`);
  }
  return { id: group.groupId, quantity: group.quantity };
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
  const res = await holdingClient(session).listHoldings({});
  expect(res.instruments).toHaveLength(
    expected.filter((e) => e.kind === "instrument").length,
  );
  expect(res.groups).toHaveLength(
    expected.filter((e) => e.kind === "group").length,
  );
  for (const e of expected) {
    expect(rowOf(res, e.name).quantity, e.name).toBe(e.quantity);
  }

  // The page shows each holding, in order. The sheet covers the sidebar
  // until it is closed.
  await page.getByTestId("activity-sheet-close").click();
  await page.getByTestId("nav-holdings").click();
  await expect(page).toHaveURL("/holdings");
  const table = page.getByTestId("holdings-table");
  for (const e of expected) {
    const { id } = rowOf(res, e.name);
    const row = table.getByTestId(`holding-row-${id}`);
    await expect(row).toContainText(e.name);
    await expect(row).toContainText(e.className);
    await expect(row.getByTestId(`holding-qty-${id}`)).toHaveText(e.shown);
  }
  const ids = await table
    .getByTestId(/^holding-row-/)
    .evaluateAll((rows) => rows.map((r) => r.getAttribute("data-testid")));
  expect(ids).toEqual(
    expected.map((e) => `holding-row-${rowOf(res, e.name).id}`),
  );

  // Another user holds none of them.
  const other = await seed();
  const theirs = await holdingClient(other.session).listHoldings({});
  expect(theirs.instruments).toHaveLength(0);
  expect(theirs.groups).toHaveLength(0);
});
