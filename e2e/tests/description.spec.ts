import path from "node:path";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import { holdingClient, statementClient } from "../helpers/api";
import { seedDescribedInstrument } from "../helpers/db";
import { uploadStatement } from "../helpers/upload";
import { expect, test } from "../helpers/test";

// Modelled on a real Fidelity UK export with its identifiers replaced. The
// two fund lines carry no symbol, so each key states only a description. The
// first fund is seeded as reference data and matches; the second is held by
// nobody and stays a group holding. No key reaches a datasource, so the spec
// needs no recording. The ZZ prefix keeps these descriptions unique, because
// the seeded row outlives the test.
const fixture = path.resolve(
  __dirname,
  "..",
  "fixtures",
  "fidelity-description.csv",
);
const described = "ZZ DESCRIBED GLOBAL EQUITY FUND ACC";
const undescribed = "ZZ UNDESCRIBED GLOBAL BOND FUND INC";
const isin = "ZZ0000000091";
const ticker = "ZZDESC";

let instrumentId: string;

test.beforeAll(async () => {
  instrumentId = await seedDescribedInstrument(
    "fidelity_uk",
    described,
    isin,
    ticker,
  );
});

test("resolves a line through the description the broker gave it", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn();
  await page.goto("/transactions");
  const runId = await uploadStatement(page, fixture, {
    state: "completed",
    rows: "5 rows",
  });
  await page.getByTestId("activity-sheet-close").click();

  const keys = (await statementClient(session).getStatement({ runId })).keys;
  const states = (description: string) =>
    keys.find((k) =>
      k.statedKey?.identifiers.some(
        (i) =>
          i.type === IdentifierType.BROKER_DESCRIPTION &&
          i.value === description,
      ),
    );
  expect(states(described)?.outcome).toBe(ResolutionOutcome.MATCHED);
  const unknown = states(undescribed);
  expect(unknown?.outcome).toBe(ResolutionOutcome.UNRECOGNISED);
  expect(unknown?.reason).toBe("failed to match broker description");

  const holdings = await holdingClient(session).listHoldings({});
  const group = holdings.groups.find((g) =>
    g.identifiers.some((i) => i.value === undescribed),
  );
  expect(group).toBeDefined();
  await page.getByTestId("nav-holdings").click();
  await expect(page).toHaveURL("/holdings");
  const resolved = page.getByTestId(`holding-row-${instrumentId}`);
  await expect(resolved).toHaveAttribute("data-kind", "instrument");
  await expect(resolved).toContainText(ticker);
  await expect(resolved).toContainText(isin);
  await expect(resolved.getByTestId("holding-basis")).toHaveCount(0);
  await expect(resolved.getByTestId(`holding-qty-${instrumentId}`)).toHaveText(
    "80.00",
  );
  const unresolved = page.getByTestId(`holding-row-${group!.groupId}`);
  await expect(unresolved).toHaveAttribute("data-kind", "group");
  await expect(unresolved).toContainText(undescribed);
  await expect(unresolved.getByTestId("holding-basis")).toHaveAttribute(
    "data-state",
    "statements",
  );
});
