import path from "node:path";
import { Code } from "@connectrpc/connect";
import { RunKind } from "../gen/run/v1/run_pb";
import {
  adminRunClient,
  resolutionFindings,
  resolutionItems,
  stmtItems,
} from "../helpers/api";
import { uploadStmt } from "../helpers/upload";
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
  await page.goto("/txns");
  const runId = await uploadStmt(page, fixture, {
    state: "rejections",
    from: "2025-02-01",
  });

  // The admin RPCs refuse the user.
  await expect(adminRunClient(userSession).listRuns({})).rejects.toMatchObject({
    code: Code.PermissionDenied,
  });

  // The record: the ingest statement run, the resolution it started, the rows
  // it rejected, and no findings, since every fetch was answered.
  const { session } = await signIn("admin");
  const admin = adminRunClient(session);
  const stmt = await admin.getRun({ runId: runId });
  expect(stmt.run?.userEmail).toBe(user.email);
  expect(await stmtItems(admin, runId)).toHaveLength(januaryRows);
  const resolutions = stmt.run!.children.filter(
    (c) => c.run?.kind === RunKind.RESOLVE_STMT_KEYS,
  );
  expect(resolutions).toHaveLength(1);
  const child = resolutions[0].run!;
  const resolution = await admin.getRun({ runId: child.id });
  const resolved = await resolutionItems(admin, child.id);
  expect(resolved.length).toBeGreaterThan(0);
  expect(resolutionFindings(stmt.findings)).toHaveLength(0);
  expect(resolutionFindings(resolution.findings)).toHaveLength(0);

  // The pages: the user's runs with the resolution under its statement,
  // the ingest statement run, then its resolution.
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

  await row.getByTestId(`run-link-${runId}`).click();
  await expect(page).toHaveURL(`/admin/runs/${runId}`);
  const run = page.getByTestId("admin-run-page");
  await expect(run.getByTestId("admin-run-lineage")).toContainText(user.email);
  await expect(run.getByTestId("rejection-count-0")).toHaveText(
    String(januaryRows),
  );
  await expect(run.locator('[data-testid^="finding-row-"]')).toHaveCount(
    stmt.findings.length,
  );

  await run.getByTestId(`run-toggle-${runId}`).click();
  await run
    .getByTestId("admin-run-lineage")
    .getByTestId(`run-row-${child.id}`)
    .getByTestId(`run-link-${child.id}`)
    .click();
  await expect(page).toHaveURL(`/admin/runs/${child.id}`);
  const lineage = page.getByTestId("admin-run-lineage");
  await expect(lineage.getByTestId(`run-row-${child.id}`)).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(lineage.getByTestId(`run-link-${runId}`)).toHaveAttribute(
    "href",
    `/admin/runs/${runId}`,
  );
  await expect(page.getByTestId(/^item-row-/)).toHaveCount(resolved.length);
});
