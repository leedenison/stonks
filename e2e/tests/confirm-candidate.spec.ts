import path from "node:path";
import { FetchOutcome } from "../gen/admin/v1/admin_pb";
import { RunKind, RunTrigger } from "../gen/run/v1/run_pb";
import { Arbiter, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  adminClient,
  fetchItems,
  fetchRun,
  holdingClient,
  statementClient,
} from "../helpers/api";
import { dropFetchCache, fetchCacheKey } from "../helpers/cache";
import { uploadStatement } from "../helpers/upload";
import { expect, test } from "../helpers/test";

// The fixture is one Schwab line modelled on a real Schwab export. It
// states the bare ticker INTC, which no other spec uses. A bare ticker
// cannot be associated automatically, so the user chooses among the
// instruments the datasources offer. The upload fills the fetch cache, so
// listing the candidates reads OpenFIGI from the cache.
const fixture = path.resolve(__dirname, "..", "fixtures", "schwab-confirm.csv");
const ticker = "INTC";
const cacheKey = fetchCacheKey(
  "openfigi",
  "identity",
  { type: "mic_ticker", domain: "", value: ticker },
  ["USD"],
);

test("chooses an instrument for a bare ticker", async ({ signIn, page }) => {
  const { user, session } = await signIn();
  await page.goto("/transactions");
  const runId = await uploadStatement(page, fixture, {
    state: "completed",
    rows: "3 rows",
  });
  await page.getByTestId("activity-sheet-close").click();

  const statements = statementClient(session);
  const keys = (await statements.getStatement({ runId })).keys;
  const key = keys.find((k) =>
    k.statedKey?.identifiers.some((i) => i.value === ticker),
  );
  expect(key?.outcome).toBe(ResolutionOutcome.UNRECOGNISED);
  const keyId = key!.statedKeyId;

  // Opening the dialog resolves the key from the cache. The spec then drops
  // the entry, so the choice resolves the key through OpenFIGI again.
  await page.goto(`/statements/${runId}`);
  const row = page.getByTestId(`key-row-${keyId}`);
  await expect(row.getByTestId("resolution-chip")).toHaveAttribute(
    "data-state",
    "unrecognised",
  );
  await expect(page.getByTestId(/^key-choose-/)).toHaveCount(1);
  await page.getByTestId(`key-choose-${keyId}`).click();
  const dialog = page.getByTestId("candidates-dialog");
  const offered = dialog.getByTestId(/^candidate-row-openfigi-/).first();
  await expect(offered).toContainText(ticker);
  await dropFetchCache(cacheKey);
  await offered.getByTestId(/^candidate-choose-openfigi-/).click();
  await expect(dialog).toBeHidden();
  await expect(row.getByTestId("resolution-chip")).toHaveAttribute(
    "data-state",
    "matched",
  );
  await expect(row.getByTestId(`key-arbiter-${keyId}`)).toHaveText("Chosen");
  await expect(page.getByTestId(/^key-choose-/)).toHaveCount(0);

  // Re-read through the API to check the stored state, not just the page.
  const after = (await statements.getStatement({ runId })).keys.find(
    (k) => k.statedKeyId === keyId,
  );
  expect(after?.outcome).toBe(ResolutionOutcome.MATCHED);
  expect(after?.arbiter).toBe(Arbiter.USER);
  const holdings = await holdingClient(session).listHoldings({});
  expect(holdings.groups).toHaveLength(0);
  const shares = holdings.instruments.find((h) =>
    h.identifiers.some((i) => i.value === ticker),
  );
  expect(shares?.quantity).toBe("20");
  await page.getByTestId("nav-holdings").click();
  await expect(page).toHaveURL("/holdings");
  await expect(
    page.getByTestId(`holding-row-${shares!.instrumentId}`),
  ).toHaveAttribute("data-kind", "instrument");

  // Two user-triggered resolutions ran, newest first: the choice, then the
  // listing. The listing read OpenFIGI from the cache, so its fetch made 0
  // attempts. The choice called OpenFIGI once, because the spec dropped the
  // cache entry. The list is filtered to resolutions, so each run's fetches
  // come from its tree.
  const admin = adminClient((await signIn("admin")).session);
  const listed = await admin.listRuns({
    userId: user.id,
    kind: RunKind.RESOLUTION,
    trigger: RunTrigger.USER,
  });
  expect(listed.runs).toHaveLength(2);
  const [choice, listing] = listed.runs;
  for (const [run, attempts] of [
    [listing, 0],
    [choice, 1],
  ] as const) {
    const tree = (await admin.getRun({ runId: run.run!.id })).run!;
    const items = await fetchItems(admin, fetchRun(tree, "openfigi").run!.id);
    expect(items).toHaveLength(1);
    expect(items[0].outcome).toBe(FetchOutcome.SERVED);
    expect(items[0].attempts).toBe(attempts);
  }
});
