import path from "node:path";
import { Code } from "@connectrpc/connect";
import { adminRunClient, replayClient } from "../helpers/api";
import { uploadStatement } from "../helpers/upload";
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
  const runId = await uploadStatement(page, fixture, {
    state: "completed",
    holds: false,
  });

  // The RPC refuses the user, and refuses a datasource that is not enabled.
  await expect(
    replayClient(userSession).startReplay({
      runId,
      scope: { case: "unavailable", value: true },
    }),
  ).rejects.toMatchObject({ code: Code.PermissionDenied });
  const { session } = await signIn("admin");
  await expect(
    replayClient(session).startReplay({
      runId,
      scope: { case: "datasource", value: "nothing" },
    }),
  ).rejects.toMatchObject({ code: Code.FailedPrecondition });

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

  const statement = await adminRunClient(session).getRun({ runId: runId });
  const child = statement.run!.children[0].run!;
  await page.goto(`/admin/runs/${child.id}`);
  await expect(page.getByTestId("run-replay")).toBeVisible();
});
