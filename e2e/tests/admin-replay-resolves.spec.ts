import path from "node:path";
import { FetchOutcome } from "../gen/admin/v1/admin_pb";
import { RunKind, RunTrigger } from "../gen/run/v1/run_pb";
import { ResolutionOutcome } from "../gen/type/v1/type_pb";
import { adminClient, holdingClient, statementClient } from "../helpers/api";
import { expect, test } from "../helpers/test";

// One IBKR line, derived from the client's IBKR test export, which is
// modelled on a real export with its identifiers replaced. It states Apple's
// ISIN, which the provider answers with a rate limit on every try of the
// first fetch and serves on the next, so the key stays unavailable until an
// administrator replays it.
const fixture = path.resolve(__dirname, "..", "fixtures", "ibkr-retry.qfx");
const isin = "US0378331005";

test("replays a statement's unavailable key from the runs page and resolves it", async ({
  signIn,
  page,
}) => {
  const { user, session: userSession } = await signIn();
  await page.goto("/transactions");
  await page.getByTestId("upload-statement").click();
  await page.getByTestId("upload-file").setInputFiles(fixture);
  await page.getByTestId("upload-submit").click();
  const item = page
    .getByTestId("activity-sheet")
    .getByTestId(/^activity-item-/)
    .first();
  await expect(item.getByTestId("state-chip")).toHaveAttribute(
    "data-state",
    "completed",
  );
  const runId = (await item.getAttribute("data-testid"))?.replace(
    "activity-item-",
    "",
  );
  expect(runId).toBeTruthy();

  // Unavailable, not unrecognised: the refusal is temporary.
  await page.getByTestId("activity-sheet-close").click();
  const keys = (
    await statementClient(userSession).getStatement({ runId: runId! })
  ).keys;
  const key = keys.find((k) =>
    k.statedKey?.identifiers.some((i) => i.value === isin),
  );
  expect(key).toBeTruthy();
  await page.goto(`/statements/${runId}`);
  await expect(
    page
      .getByTestId(`key-row-${key!.statedKeyId}`)
      .getByTestId("resolution-chip"),
  ).toHaveAttribute("data-state", "unavailable");

  // A temporary refusal leaves no block, so the holding rests on the key alone.
  const { session } = await signIn("admin");
  const admin = adminClient(session);
  const statement = await admin.getRun({ runId: runId! });
  expect(statement.findings).toHaveLength(0);
  const resolution = statement.run!.children[0];
  const fetched = await admin.getRun({ runId: resolution.children[0].run!.id });
  expect(fetched.fetchItems[0].outcome).toBe(FetchOutcome.FAILED_TEMPORARY);
  expect(fetched.fetchItems[0].attempts).toBe(3);
  const resolved = await admin.getRun({ runId: resolution.run!.id });
  expect(resolved.resolutionItems[0].outcome).toBe(
    ResolutionOutcome.UNAVAILABLE,
  );
  const before = await holdingClient(userSession).listHoldings({});
  expect(before.groups).toHaveLength(1);
  expect(before.groups[0].identifiers.map((i) => i.value)).toEqual([isin]);
  expect(before.groups[0].quantity).toBe("100");
  expect(before.instruments).toHaveLength(1);

  await page.goto(`/admin/runs?user=${user.id}`);
  await page.getByTestId(`run-row-${runId}`).getByRole("link").first().click();
  await expect(page).toHaveURL(`/admin/runs/${runId}`);
  await page.getByTestId("run-replay").click();
  const dialog = page.getByTestId("replay-dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog.getByTestId("replay-scope-unavailable")).toBeChecked();
  await dialog.getByTestId("replay-start").click();
  await expect(page).toHaveURL(
    new RegExp(`/admin/runs/(?!${runId}$)[0-9a-f-]+$`),
  );
  const replayId = page.url().split("/").pop()!;

  const lineage = page.getByTestId("admin-run-lineage");
  await expect(lineage).toContainText(user.email);
  await expect(
    lineage.getByTestId(`run-row-${replayId}`).getByTestId("state-chip"),
  ).toHaveAttribute("data-state", "completed");
  const replay = await admin.getRun({ runId: replayId });
  expect(replay.run?.run?.kind).toBe(RunKind.REPLAY);
  expect(replay.run?.run?.trigger).toBe(RunTrigger.ADMINISTRATOR);
  expect(replay.run?.userEmail).toBe(user.email);
  expect(replay.replay?.sourceRunId).toBe(runId);
  expect(replay.replay?.datasource).toBe("");
  const replayed = await admin.getRun({
    runId: replay.run!.children[0].run!.id,
  });
  expect(replayed.resolutionItems[0].outcome).toBe(ResolutionOutcome.MATCHED);
  await page.goto(`/admin/runs?user=${user.id}`);
  await expect(page.getByTestId(`run-row-${replayId}`)).toBeVisible();

  // The holding is now the instrument's, named by the identifier stated.
  const after = await holdingClient(userSession).listHoldings({});
  expect(after.groups).toHaveLength(0);
  const holding = after.instruments.find((h) =>
    h.identifiers.some((i) => i.value === isin),
  );
  expect(holding?.quantity).toBe("100");
});
