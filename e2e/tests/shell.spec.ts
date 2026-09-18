import {
  closeRedis,
  deleteSession,
  injectSession,
  seedSession,
} from "../helpers/auth";
import { closeDB, deleteUser, seedUser, type Role } from "../helpers/db";
import { expect, test } from "../helpers/test";

const users: string[] = [];
const sessions: string[] = [];

async function seed(role: Role = "user") {
  const user = await seedUser(role);
  users.push(user.id);
  const session = await seedSession(user);
  sessions.push(session);
  return { user, session };
}

test.afterEach(async () => {
  await Promise.all(sessions.splice(0).map(deleteSession));
  await Promise.all(users.splice(0).map(deleteUser));
});

test.afterAll(async () => {
  await closeDB();
  await closeRedis();
});

test("serves the shell through the edge", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("landing-page")).toBeVisible();
  await expect(page.getByTestId("app-header")).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-testmode", "");
});

test("navigates the sidebar and marks the current page", async ({
  context,
  page,
}) => {
  const { session } = await seed();
  await injectSession(context, session);
  await page.goto("/transactions");
  await expect(page.getByTestId("nav-transactions")).toHaveAttribute(
    "aria-current",
    "page",
  );
  await page.getByTestId("nav-holdings").click();
  await expect(page).toHaveURL("/holdings");
  await expect(page.getByTestId("holdings-page")).toBeVisible();
  await expect(page.getByTestId("nav-holdings")).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(page.getByTestId("nav-transactions")).not.toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("reaches the statements and the profile from the menu", async ({
  context,
  page,
}) => {
  const { session } = await seed();
  await injectSession(context, session);
  await page.goto("/transactions");
  await page.getByTestId("user-email").click();
  await page.getByTestId("menu-statements").click();
  await expect(page).toHaveURL("/statements");
  await expect(page.getByTestId("statements-page")).toBeVisible();
  await page.getByTestId("user-email").click();
  await page.getByTestId("menu-profile").click();
  await expect(page).toHaveURL("/profile");
  await expect(page.getByTestId("profile-page")).toBeVisible();
});

test("denies a user the admin area", async ({ context, page }) => {
  const { session } = await seed();
  await injectSession(context, session);
  await page.goto("/admin");
  await expect(page.getByTestId("access-denied")).toBeVisible();
  await expect(page.getByTestId("admin-nav")).toHaveCount(0);
});

test("shows an administrator the admin area", async ({ context, page }) => {
  const { session } = await seed("admin");
  await injectSession(context, session);
  await page.goto("/transactions");
  await page.getByTestId("user-email").click();
  await page.getByTestId("menu-admin").click();
  await expect(page).toHaveURL("/admin");
  await expect(page.getByTestId("admin-nav")).toBeVisible();
  await expect(page.getByTestId("admin-page")).toBeVisible();
});

test("keeps the chosen scheme across a reload", async ({ context, page }) => {
  const { session } = await seed();
  await injectSession(context, session);
  await page.goto("/transactions");
  await expect(page.locator("html")).not.toHaveAttribute("data-theme");
  await page.getByTestId("user-email").click();
  await page.getByTestId("scheme-dark").click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByTestId("user-email").click();
  await page.getByTestId("scheme-system").click();
  await expect(page.locator("html")).not.toHaveAttribute("data-theme");
});

test("keeps the sidebar collapsed across a reload", async ({
  context,
  page,
}) => {
  const { session } = await seed();
  await injectSession(context, session);
  await page.goto("/transactions");
  await expect(page.getByTestId("sidebar")).not.toHaveAttribute(
    "data-collapsed",
  );
  await page.getByTestId("sidebar-collapse").click();
  await expect(page.getByTestId("sidebar")).toHaveAttribute("data-collapsed");
  await page.reload();
  await expect(page.getByTestId("sidebar")).toHaveAttribute("data-collapsed");
});
