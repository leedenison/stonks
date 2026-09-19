import path from "node:path";
import type { Holding } from "../gen/holding/v1/holding_pb";
import { AssetClass } from "../gen/type/v1/type_pb";
import { holdingClient } from "../helpers/api";
import {
  closeRedis,
  deleteSession,
  injectSession,
  seedSession,
} from "../helpers/auth";
import { closeDB, deleteUser, seedUser } from "../helpers/db";
import { expect, test } from "../helpers/test";

// The fixture is a copy of the client's Fidelity UK test export, modelled on
// a real export with its identifiers replaced. Uploaded over the period it
// states, its eleven rows sum to these holdings, listed as the page orders
// them: cash first, then by name. The API states each quantity exactly and
// the page shows it to two places.
const fixture = path.resolve(__dirname, "..", "fixtures", "fidelity-uk.csv");
const expected = [
  {
    name: "GBP",
    quantity: "12092.79",
    shown: "12092.79",
    assetClass: AssetClass.CASH,
    className: "Cash",
  },
  {
    name: "BAE SYSTEMS, ORD GBP0.025 (BA.)",
    quantity: "120",
    shown: "120.00",
    assetClass: AssetClass.SECURITY,
    className: "Security",
  },
  {
    name: "Baillie Gifford Responsible Global Equity Income B Inc",
    quantity: "19.26",
    shown: "19.26",
    assetClass: AssetClass.SECURITY,
    className: "Security",
  },
  {
    name: "VANGUARD FUNDS PLC, S&P 500 UCITS ETF USD DIS (VUSA)",
    quantity: "-141",
    shown: "-141.00",
    assetClass: AssetClass.SECURITY,
    className: "Security",
  },
];

const users: string[] = [];
const sessions: string[] = [];

test.afterEach(async () => {
  await Promise.all(sessions.splice(0).map(deleteSession));
  await Promise.all(users.splice(0).map(deleteUser));
});

test.afterAll(async () => {
  await closeDB();
  await closeRedis();
});

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
  context,
  page,
}) => {
  const user = await seedUser();
  users.push(user.id);
  const session = await seedSession(user);
  sessions.push(session);
  await injectSession(context, session);

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
  const other = await seedUser();
  users.push(other.id);
  const otherSession = await seedSession(other);
  sessions.push(otherSession);
  const theirs = await holdingClient(otherSession).listHoldings({});
  expect(theirs.holdings).toHaveLength(0);
});
