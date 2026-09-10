import { authClient } from "../helpers/api";
import {
  closeRedis,
  deleteSession,
  injectSession,
  seedSession,
} from "../helpers/auth";
import { closeDB, deleteUser, seedUser, type Role } from "../helpers/db";
import { expect, test } from "../helpers/test";

// Each test owns the user it seeds; the session goes with the user's rows.
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

test("offers sign-in to a visitor", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("landing-page")).toBeVisible();
  await expect(page.getByTestId("sign-in")).toBeVisible();
  await expect(page.getByTestId("user-area")).toHaveText("Not signed in");
});

test("sends a visitor from the profile to the landing page", async ({
  page,
}) => {
  await page.goto("/profile");
  await expect(page).toHaveURL("/");
  await expect(page.getByTestId("landing-page")).toBeVisible();
});

test("lands a signed-in user on the profile", async ({ context, page }) => {
  const { user, session } = await seed();
  await injectSession(context, session);
  await page.goto("/");
  await expect(page).toHaveURL("/profile");
  await expect(page.getByTestId("profile-email")).toHaveText(user.email);
  await expect(page.getByTestId("profile-role")).toHaveText("user");
  await expect(page.getByTestId("user-email")).toHaveText(user.email);
});

test("shows the admin role", async ({ context, page }) => {
  const { session } = await seed("admin");
  await injectSession(context, session);
  await page.goto("/profile");
  await expect(page.getByTestId("profile-role")).toHaveText("admin");
});

test("returns to the landing page once the session is gone", async ({
  context,
  page,
}) => {
  const { session } = await seed();
  await injectSession(context, session);
  await page.goto("/profile");
  await expect(page.getByTestId("profile-page")).toBeVisible();

  await deleteSession(session);
  await page.reload();
  await expect(page).toHaveURL("/");
  await expect(page.getByTestId("sign-in")).toBeVisible();
});

test("signs out", async ({ context, page }) => {
  const { session } = await seed();
  await injectSession(context, session);
  await page.goto("/profile");
  await expect(page.getByTestId("profile-page")).toBeVisible();

  await page.getByTestId("sign-out").click();
  await expect(page).toHaveURL("/");
  await expect(page.getByTestId("user-area")).toHaveText("Not signed in");

  // The session is gone on the server, not only in the browser.
  const res = await authClient(session).getSession({});
  expect(res.user).toBeUndefined();
});
