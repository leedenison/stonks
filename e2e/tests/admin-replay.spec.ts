import path from "node:path";
import { Code, ConnectError } from "@connectrpc/connect";
import { adminClient } from "../helpers/api";
import { expect, test } from "../helpers/test";

// The client's Fidelity UK test export, modelled on a real export with its
// identifiers replaced.
const fixture = path.resolve(__dirname, "..", "fixtures", "fidelity-uk.csv");

// Fidelity's keys state a ticker with no venue, which the datasource answers
// but which never associates, so every key resolves unrecognised and a replay
// over the keys left unavailable is refused. The refusal is the path
// exercised here.
test("offers a replay on a statement's page and shows its refusal", async ({
  signIn,
  page,
}) => {
  const { session: userSession } = await signIn();
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

  // The RPC refuses the user, and refuses a datasource that is not enabled.
  const refused = await adminClient(userSession)
    .startReplay({ runId: runId!, scope: { case: "unavailable", value: true } })
    .then(
      () => undefined,
      (e: unknown) => e,
    );
  expect(ConnectError.from(refused).code).toBe(Code.PermissionDenied);
  const { session } = await signIn("admin");
  const disabled = await adminClient(session)
    .startReplay({
      runId: runId!,
      scope: { case: "datasource", value: "nothing" },
    })
    .then(
      () => undefined,
      (e: unknown) => e,
    );
  expect(ConnectError.from(disabled).code).toBe(Code.FailedPrecondition);

  // The pages: the statement and its resolution carry the action, and the
  // dialog reports that nothing is left unavailable.
  await page.goto(`/admin/runs/${runId}`);
  const run = page.getByTestId("admin-run-page");
  await run.getByTestId("run-replay").click();
  const dialog = page.getByTestId("replay-dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByTestId("replay-start").click();
  await expect(dialog.getByTestId("replay-error")).toHaveText(
    "no key to replay",
  );
  await expect(page).toHaveURL(`/admin/runs/${runId}`);

  const statement = await adminClient(session).getRun({ runId: runId! });
  const child = statement.run!.children[0].run!;
  await page.goto(`/admin/runs/${child.id}`);
  await expect(page.getByTestId("run-replay")).toBeVisible();
});
