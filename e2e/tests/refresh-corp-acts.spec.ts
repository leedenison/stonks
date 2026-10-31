import path from "node:path";
import { AlertKind } from "../gen/admin/alert/v1/alert_pb";
import { FetchKind } from "../gen/admin/block/v1/block_pb";
import { FetchOutcome, FindingKind } from "../gen/admin/run/v1/run_pb";
import { RunKind, RunState } from "../gen/run/v1/run_pb";
import {
  adminRunClient,
  alertClient,
  alertPageToken,
  alertsAddress,
  fetchItems,
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

  // Other specs change the alerts, so the spec polls for the page that holds this one.
  const alerts = alertClient(session);
  let token: string | undefined;
  await expect
    .poll(async () => {
      token = await alertPageToken(
        alerts,
        AlertKind.FINDING,
        (a) =>
          a.subject.case === "finding" &&
          a.subject.value.id === unmatched[0].id,
      );
      return token;
    })
    .not.toBeUndefined();
  await page.goto(alertsAddress("finding", token!));
  const alert = page.getByTestId(`alert-row-finding-${unmatched[0].id}`);
  await alert.getByTestId(`alert-act-finding-${unmatched[0].id}`).click();
  await expect(alert).toHaveCount(0);
  const cleared = (await admin.getRun({ runId })).findings.find(
    (f) => f.id === unmatched[0].id,
  );
  expect(cleared?.clearedAt).toBeDefined();

  // A refresh asks the corporate actions source of the US again, reading
  // past the cache that the statement's fetch filled. EODHD serves corporate
  // actions too, and is not asked.
  const started = (
    await admin.startSystemRun({ kind: RunKind.REFRESH_CORP_ACTS })
  ).run!;
  expect(started.kind).toBe(RunKind.REFRESH_CORP_ACTS);
  await expect
    .poll(
      async () => (await admin.getRun({ runId: started.id })).run?.run?.state,
    )
    .toBe(RunState.COMPLETED);
  const children = (await admin.getRun({ runId: started.id })).run!.children;
  expect(children.map((c) => c.fetch?.datasource)).toEqual(["massive"]);
  const [child] = children;
  expect(child.fetch?.kind).toBe(FetchKind.CORP_ACTS);
  const ko = (await fetchItems(admin, child.run!.id)).filter(
    (i) => i.outcome === FetchOutcome.SERVED && i.sent?.value === "KO",
  );
  expect(ko).toHaveLength(1);
  expect(ko[0].attempts).toBeGreaterThan(0);
});
