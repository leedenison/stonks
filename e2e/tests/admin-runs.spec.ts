import path from "node:path";
import { Code, ConnectError } from "@connectrpc/connect";
import { RunKind } from "../gen/run/v1/run_pb";
import { adminClient, resolutionItems, statementItems } from "../helpers/api";
import { expect, test } from "../helpers/test";

// The client's Fidelity UK test export, modelled on a real export with its
// identifiers replaced. Narrowing its period to February rejects the four
// January rows.
const fixture = path.resolve(__dirname, "..", "fixtures", "fidelity-uk.csv");
const januaryRows = 4;

test("shows an administrator the runs a user's upload produced", async ({
  signIn,
  page,
}) => {
  const { user, session: userSession } = await signIn();
  await page.goto("/transactions");
  await page.getByTestId("upload-statement").click();
  await page.getByTestId("upload-file").setInputFiles(fixture);
  await page.getByTestId("upload-from").fill("2025-02-01");
  await page.getByTestId("upload-submit").click();
  const item = page
    .getByTestId("activity-sheet")
    .getByTestId(/^activity-item-/)
    .first();
  await expect(item.getByTestId("state-chip")).toHaveAttribute(
    "data-state",
    "rejections",
  );
  const runId = (await item.getAttribute("data-testid"))?.replace(
    "activity-item-",
    "",
  );
  expect(runId).toBeTruthy();

  // The admin RPCs refuse the user.
  const refused = await adminClient(userSession)
    .listRuns({})
    .then(
      () => undefined,
      (e: unknown) => e,
    );
  expect(ConnectError.from(refused).code).toBe(Code.PermissionDenied);

  // The record: the statement run, the resolution it started, the rows it
  // rejected, and no findings, since every fetch was answered.
  const { session } = await signIn("admin");
  const admin = adminClient(session);
  const statement = await admin.getRun({ runId: runId! });
  expect(statement.run?.userEmail).toBe(user.email);
  expect(await statementItems(admin, runId!)).toHaveLength(januaryRows);
  expect(statement.run?.children).toHaveLength(1);
  const child = statement.run!.children[0].run!;
  expect(child.kind).toBe(RunKind.RESOLUTION);
  const resolution = await admin.getRun({ runId: child.id });
  const resolved = await resolutionItems(admin, child.id);
  expect(resolved.length).toBeGreaterThan(0);
  expect(statement.findings).toHaveLength(0);
  expect(resolution.findings).toHaveLength(0);

  // The pages: the user's runs with the resolution under its statement,
  // the statement run, then its resolution.
  await page.goto(`/admin/runs?user=${user.id}`);
  const row = page.getByTestId(`run-row-${runId}`);
  await expect(row.getByTestId("state-chip")).toHaveAttribute(
    "data-state",
    "completed",
  );
  await expect(page.getByTestId(`run-row-${child.id}`)).toHaveCount(0);
  await row.getByTestId(`run-toggle-${runId}`).click();
  await expect(page.getByTestId(`run-row-${child.id}`)).toBeVisible();
  await expect(page.getByTestId("runs-filter-user")).toContainText(user.email);

  await row.getByRole("link").first().click();
  await expect(page).toHaveURL(`/admin/runs/${runId}`);
  const run = page.getByTestId("admin-run-page");
  await expect(run.getByTestId("admin-run-lineage")).toContainText(user.email);
  await expect(run.getByTestId("rejection-count-0")).toHaveText(
    String(januaryRows),
  );
  await expect(run.getByTestId("empty-state")).toHaveText(
    "The run recorded no findings.",
  );

  await run.getByTestId(`run-toggle-${runId}`).click();
  await run
    .getByTestId("admin-run-lineage")
    .getByTestId(`run-row-${child.id}`)
    .getByRole("link")
    .first()
    .click();
  await expect(page).toHaveURL(`/admin/runs/${child.id}`);
  const lineage = page.getByTestId("admin-run-lineage");
  await expect(lineage.getByTestId(`run-row-${child.id}`)).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(
    lineage.getByTestId(`run-row-${runId}`).getByRole("link").first(),
  ).toHaveAttribute("href", `/admin/runs/${runId}`);
  await expect(page.getByTestId(/^item-row-/)).toHaveCount(resolved.length);
});
