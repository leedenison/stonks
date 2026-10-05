import path from "node:path";
import { FetchOutcome, FindingKind } from "../gen/admin/v1/admin_pb";
import { ResolutionOutcome } from "../gen/type/v1/type_pb";
import { adminClient, fetchItems, resolutionItems } from "../helpers/api";
import { uploadStatement } from "../helpers/upload";
import { expect, test } from "../helpers/test";

// One IBKR line, derived from the client's IBKR test export, which is
// modelled on a real export with its identifiers replaced. It states an ISIN
// whose format the provider refuses, so the fetch fails permanently, blocks
// the identifier and reports the block as a finding.
const fixture = path.resolve(__dirname, "..", "fixtures", "ibkr-blocked.qfx");
const isin = "ZZBLOCK01";

test("clears a block from the blocks page, clearing the finding reporting it", async ({
  signIn,
  page,
}) => {
  const { user } = await signIn();
  await page.goto("/transactions");
  const runId = await uploadStatement(page, fixture, { state: "completed" });

  // A permanent refusal blocks the identifier and reports it as a finding.
  const { session } = await signIn("admin");
  const admin = adminClient(session);
  const statement = await admin.getRun({ runId: runId });
  expect(statement.findings).toHaveLength(1);
  const finding = statement.findings[0];
  expect(finding.kind).toBe(FindingKind.BLOCK);
  expect(finding.blockId).toBeTruthy();
  expect(finding.clearedAt).toBeUndefined();
  const resolution = statement.run!.children[0];
  const fetch = resolution.children[0];
  const fetched = await fetchItems(admin, fetch.run!.id);
  expect(fetched).toHaveLength(1);
  expect(fetched[0].outcome).toBe(FetchOutcome.FAILED_PERMANENT);
  const resolved = await resolutionItems(admin, resolution.run!.id);
  const key = resolved.find((i) =>
    i.statedKey?.identifiers.some((id) => id.value === isin),
  );
  expect(key?.outcome).toBe(ResolutionOutcome.UNAVAILABLE);

  await page.goto(`/admin/runs?user=${user.id}`);
  await expect(page.getByTestId(`run-open-findings-${runId}`)).toHaveText("1");
  await page.goto("/admin/blocks");
  const row = page.getByTestId(`block-row-${finding.blockId}`);
  await expect(row).toContainText(isin);
  await row.getByTestId(`block-clear-${finding.blockId}`).click();
  await expect(row).toHaveCount(0);

  // Clearing a block clears the finding reporting it.
  await page.goto(`/admin/runs/${runId}`);
  await expect(page.getByTestId(`finding-row-${finding.id}`)).toBeVisible();
  await expect(
    page.getByTestId(`finding-clear-block-${finding.id}`),
  ).toHaveCount(0);
  const after = await admin.getRun({ runId: runId });
  expect(after.findings[0].clearedAt).toBeDefined();
  const blocks = await admin.listBlocks({ includeCleared: true });
  const block = blocks.blocks.find((b) => b.id === finding.blockId);
  expect(block?.clearedAt).toBeDefined();
  await page.goto(`/admin/runs?user=${user.id}`);
  await expect(page.getByTestId(`run-row-${runId}`)).toBeVisible();
  await expect(page.getByTestId(`run-open-findings-${runId}`)).toHaveCount(0);
});
