import path from "node:path";
import { holdingClient } from "../helpers/api";
import { uploadStatement } from "../helpers/upload";
import { expect, test } from "../helpers/test";

// Two IBKR exports over disjoint periods, derived from the client's IBKR
// test export, which is modelled on a real export with its identifiers
// replaced. Both state one ISIN under a description the broker changed
// between them, and the ticker AMD. The datasource knows nothing of the
// ISIN and declines the bare ticker beside it, so both keys are unresolved
// and the shared ISIN makes them one holding. The periods are disjoint
// because a statement replaces the period it claims for its broker.
const january = path.resolve(__dirname, "..", "fixtures", "ibkr-january.qfx");
const february = path.resolve(__dirname, "..", "fixtures", "ibkr-february.qfx");
const descriptions = [
  "AMD ADVANCED MICRO DEVICES",
  "ADVANCED MICRO DEVICES INC",
];

test("gathers the keys of two statements that share an identifier", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn();
  await page.goto("/transactions");

  const uploads = [january, february];
  for (const [n, fixture] of uploads.entries()) {
    await uploadStatement(page, fixture, {
      state: "completed",
      earlier: n,
      rows: "3 rows",
    });
    await page.getByTestId("activity-sheet-close").click();
  }

  // One holding of the shares, summed across both statements and naming
  // every description the broker gave the line, and one of the cash.
  const res = await holdingClient(session).listHoldings({});
  expect(res.groups).toHaveLength(1);
  const group = res.groups[0];
  expect(group.quantity).toBe("250");
  expect(group.identifiers.map((i) => i.value).sort()).toEqual(
    ["US0000000002", "AMD", ...descriptions].sort(),
  );
  expect(res.instruments).toHaveLength(1);
  expect(res.instruments[0].quantity).toBe("-18852.84710536");

  // The page shows the holding once, named by the ticker, and opens to both
  // descriptions.
  await page.getByTestId("nav-holdings").click();
  await expect(page).toHaveURL("/holdings");
  const row = page.getByTestId(`holding-row-${group.groupId}`);
  await expect(row).toContainText("AMD");
  await expect(row.getByTestId(`holding-qty-${group.groupId}`)).toHaveText(
    "250.00",
  );
  await row.click();
  const detail = page.getByTestId(`holding-detail-${group.groupId}`);
  for (const d of descriptions) {
    await expect(detail).toContainText(d);
  }
});
