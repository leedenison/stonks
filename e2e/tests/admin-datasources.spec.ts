import type { Page } from "@playwright/test";
import { adminClient } from "../helpers/api";
import { deleteDatasource, seedDatasource } from "../helpers/db";
import { expect, test } from "../helpers/test";

// The global setup seeds the suite's enabled datasources, and every spec that
// resolves a key depends on them. This spec leaves those rows alone. It works
// on its own row, seeded disabled below them and named for an integration the
// build does not carry. The service refuses to enable that row, so the
// registry's enabled entries never change. The drag moves it only past the
// last enabled row, so the enabled rows keep their order among themselves.
const served = "openfigi";
const middle = "massive";
const last = "eodhd";
const unserved = "unserved";
const endpoint = "http://unserved.invalid";

// Both tests share the seeded row, so they run in order.
test.describe.configure({ mode: "serial" });

test.beforeAll(async () => {
  await seedDatasource({
    name: unserved,
    endpoint,
    credential: null,
    enabled: false,
    precedence: 4,
  });
});

test.afterAll(async () => {
  await deleteDatasource(unserved);
});

test("lists the datasources, refuses enabling one the build does not serve, and edits the endpoint, credential and config", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn("admin");
  const admin = adminClient(session);
  await page.goto("/admin/datasources");
  const rows = page
    .getByTestId("admin-datasources-table")
    .getByTestId(/^datasource-row-/);
  await expect(rows).toHaveCount(4);
  await expect(rows.nth(0)).toHaveAttribute(
    "data-testid",
    `datasource-row-${served}`,
  );
  await expect(rows.nth(1)).toHaveAttribute(
    "data-testid",
    `datasource-row-${middle}`,
  );
  await expect(rows.nth(2)).toHaveAttribute(
    "data-testid",
    `datasource-row-${last}`,
  );
  await expect(rows.nth(3)).toHaveAttribute(
    "data-testid",
    `datasource-row-${unserved}`,
  );
  const row = page.getByTestId(`datasource-row-${unserved}`);
  await expect(page.getByTestId(`datasource-row-${served}`)).toContainText(
    "enabled",
  );
  await expect(page.getByTestId(`datasource-row-${served}`)).toContainText(
    "http://openfigi.vcr:8080",
  );
  await expect(page.getByTestId(`datasource-row-${served}`)).toContainText(
    "held",
  );
  await expect(row).toContainText("disabled");
  await expect(row).toContainText("none");

  // The build carries no integration named unserved, so enabling is refused.
  await page.getByTestId(`datasource-toggle-${unserved}`).click();
  await expect(page.getByTestId("datasource-update-error")).toBeVisible();
  await expect(row).toContainText("disabled");
  const refused = await admin.listDatasources({});
  expect(refused.datasources.find((d) => d.name === unserved)?.enabled).toBe(
    false,
  );

  await page.getByTestId(`datasource-edit-${unserved}`).click();
  await page.getByTestId("datasource-endpoint").fill(`${endpoint}/v2`);
  await page.getByTestId("datasource-credential").fill("secret");
  await page.getByTestId("datasource-config").fill('{"plan": "basic"}');
  await page.getByTestId("datasource-save").click();
  await expect(page.getByTestId("datasource-dialog")).toHaveCount(0);
  await expect(row).toContainText(`${endpoint}/v2`);
  await expect(row).toContainText("held");
  const edited = await admin.listDatasources({});
  expect(edited.datasources.find((d) => d.name === unserved)).toMatchObject({
    enabled: false,
    endpoint: `${endpoint}/v2`,
    hasCredential: true,
    config: { plan: "basic" },
  });

  await page.getByTestId(`datasource-edit-${unserved}`).click();
  await page.getByTestId("datasource-clear-credential").check();
  await page.getByTestId("datasource-save").click();
  await expect(page.getByTestId("datasource-dialog")).toHaveCount(0);
  await expect(row).toContainText("none");
  const cleared = await admin.listDatasources({});
  expect(
    cleared.datasources.find((d) => d.name === unserved)?.hasCredential,
  ).toBe(false);
});

test("reorders the datasources by dragging a row", async ({ signIn, page }) => {
  const { session } = await signIn("admin");
  const admin = adminClient(session);
  await page.goto("/admin/datasources");
  const rows = page
    .getByTestId("admin-datasources-table")
    .getByTestId(/^datasource-row-/);
  await expect(rows.nth(3)).toHaveAttribute(
    "data-testid",
    `datasource-row-${unserved}`,
  );

  await move(page, unserved, last, "ArrowUp");
  await expect(rows.nth(2)).toHaveAttribute(
    "data-testid",
    `datasource-row-${unserved}`,
  );
  await expect(
    page.getByTestId(`datasource-precedence-${unserved}`),
  ).toHaveText("3");
  await expect(page.getByTestId(`datasource-precedence-${last}`)).toHaveText(
    "4",
  );
  const moved = await admin.listDatasources({});
  expect(moved.datasources.map((d) => [d.name, d.precedence])).toEqual([
    [served, 1],
    [middle, 2],
    [unserved, 3],
    [last, 4],
  ]);

  // Moving it back restores the seeded order.
  await move(page, unserved, last, "ArrowDown");
  await expect(rows.nth(3)).toHaveAttribute(
    "data-testid",
    `datasource-row-${unserved}`,
  );
  await expect(page.getByTestId(`datasource-precedence-${last}`)).toHaveText(
    "3",
  );
  const restored = await admin.listDatasources({});
  expect(restored.datasources.map((d) => [d.name, d.precedence])).toEqual([
    [served, 1],
    [middle, 2],
    [last, 3],
    [unserved, 4],
  ]);
});

// move drags the named row one place by keyboard, past its neighbour. A
// keyboard drag is one key event per step, where a pointer drag has to cross
// the sensor's activation distance. The sensor listens for the arrow keys
// from the task after the pick-up, so one task is yielded first. The drop
// waits until the page marks the neighbour as the drop target.
async function move(
  page: Page,
  name: string,
  neighbour: string,
  key: "ArrowUp" | "ArrowDown",
) {
  const grip = page.getByTestId(`datasource-grip-${name}`);
  const target = page.getByTestId(`datasource-row-${neighbour}`);
  await grip.press("Space");
  await expect(grip).toHaveAttribute("aria-pressed", "true");
  await page.evaluate(() => new Promise((resolve) => setTimeout(resolve)));
  await page.keyboard.press(key);
  await expect(target).toHaveAttribute("data-over", "true");
  await page.keyboard.press("Space");
  await expect(grip).not.toHaveAttribute("aria-pressed", "true");
}
