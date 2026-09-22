import path from "node:path";
import { holdingClient } from "../helpers/api";
import { expect, test } from "../helpers/test";

// Two IBKR exports over disjoint periods, derived from the client's IBKR
// test export, which is modelled on a real export with its identifiers
// replaced. Both state one CUSIP under a description the broker changed
// between them, and neither states an identifier resolution admits, so both
// keys are unresolved and the shared CUSIP makes them one holding. The
// periods are disjoint because a statement replaces the period it claims for
// its broker.
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

  for (const fixture of [january, february]) {
    await page.getByTestId("upload-statement").click();
    await page.getByTestId("upload-file").setInputFiles(fixture);
    await expect(page.getByTestId("upload-rows")).toHaveText("3 rows");
    await page.getByTestId("upload-submit").click();
    const item = page
      .getByTestId("activity-sheet")
      .getByTestId(/^activity-item-/)
      .first();
    await expect(item.getByTestId("state-chip")).toHaveAttribute(
      "data-state",
      "completed",
    );
    await page.getByTestId("activity-sheet-close").click();
  }

  // One holding of the shares, summed across both statements and naming
  // every description the broker gave the line, and one of the cash.
  const res = await holdingClient(session).listHoldings({});
  expect(res.groups).toHaveLength(1);
  const group = res.groups[0];
  expect(group.quantity).toBe("250");
  expect(group.identifiers.map((i) => i.value)).toEqual(["007903107"]);
  expect(group.descriptions.map((d) => d.text).sort()).toEqual(
    [...descriptions].sort(),
  );
  expect(res.instruments).toHaveLength(1);
  expect(res.instruments[0].quantity).toBe("-18852.84710536");

  // The page shows the holding once, named by a description of the line.
  await page.getByTestId("nav-holdings").click();
  await expect(page).toHaveURL("/holdings");
  const row = page.getByTestId(`holding-row-${group.groupId}`);
  await expect(row).toContainText(descriptions[0]);
  await expect(row.getByTestId(`holding-qty-${group.groupId}`)).toHaveText(
    "250.00",
  );
});
