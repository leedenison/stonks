import path from "node:path";
import { FetchKind } from "../gen/admin/block/v1/block_pb";
import { FetchOutcome, FindingKind } from "../gen/admin/run/v1/run_pb";
import { RunState } from "../gen/run/v1/run_pb";
import {
  adminRunClient,
  fetchItems,
  refreshCorpActsClient,
  stmtClient,
} from "../helpers/api";
import { expect, test } from "../helpers/test";
import { uploadStmt } from "../helpers/upload";

// The fixture buys KO after its 2012 split and states a 2 for 1 split on
// 2015-06-01, a day on which KO did not split. No other spec states KO.
const fixture = path.resolve(__dirname, "..", "fixtures", "standard-ko.json");

test("finds a stated split the calendar lacks, and refreshes the calendar", async ({
  signIn,
  page,
}) => {
  // The signed-in user is an administrator, so the spec can also open the
  // runs pages.
  const { session } = await signIn("admin");
  const admin = adminRunClient(session);

  await page.goto("/txns");
  const runId = await uploadStmt(page, fixture, { state: "completed" });
  const [key] = (await stmtClient(session).getStmt({ runId })).keys;

  // The statement's corporate actions fetch checks the stated split.
  const unmatched = (await admin.getRun({ runId })).findings.filter(
    (f) => f.kind === FindingKind.UNMATCHED_SPLIT,
  );
  expect(unmatched).toHaveLength(1);
  expect(unmatched[0].statedKeyId).toBe(key.statedKeyId);

  await page.goto(`/admin/runs/${runId}`);
  await expect(
    page.getByTestId(`finding-row-${unmatched[0].id}`),
  ).toContainText("unmatched split");

  // A refresh asks each datasource that serves corporate actions again,
  // reading past the cache that the statement's fetch filled.
  const started = (
    await refreshCorpActsClient(session).startRefreshCorpActs({})
  ).run!;
  await expect
    .poll(
      async () => (await admin.getRun({ runId: started.id })).run?.run?.state,
    )
    .toBe(RunState.COMPLETED);
  const children = (await admin.getRun({ runId: started.id })).run!.children;
  expect(children.map((c) => c.fetch?.datasource).sort()).toEqual([
    "eodhd",
    "massive",
  ]);
  for (const child of children) {
    expect(child.fetch?.kind).toBe(FetchKind.CORP_ACTS);
    const ko = (await fetchItems(admin, child.run!.id)).filter(
      (i) =>
        i.outcome === FetchOutcome.SERVED &&
        (i.sent?.value === "KO" || i.sent?.value === "KO.US"),
    );
    expect(ko, child.fetch?.datasource).toHaveLength(1);
    expect(ko[0].attempts, child.fetch?.datasource).toBeGreaterThan(0);
  }
});
