import { authClient } from "../helpers/api";
import { deleteSession } from "../helpers/auth";
import { expect, test } from "../helpers/test";

test("offers sign-in to a visitor", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("landing-page")).toBeVisible();
  await expect(page.getByTestId("sign-in")).toBeVisible();
  await expect(page.getByTestId("top-bar-sign-in")).toBeVisible();
});

test("sends a visitor from the profile to the landing page", async ({
  page,
}) => {
  await page.goto("/profile");
  await expect(page).toHaveURL("/");
  await expect(page.getByTestId("landing-page")).toBeVisible();
});

test("lands a signed-in user on the transactions", async ({ signIn, page }) => {
  const { user } = await signIn();
  await page.goto("/");
  await expect(page).toHaveURL("/transactions");
  await expect(page.getByTestId("transactions-page")).toBeVisible();
  await expect(page.getByTestId("user-email")).toHaveText(user.email);
});

test("shows the profile", async ({ signIn, page }) => {
  const { user } = await signIn();
  await page.goto("/profile");
  await expect(page.getByTestId("profile-email")).toHaveText(user.email);
  await expect(page.getByTestId("profile-role")).toHaveText("user");
});

test("shows the admin role", async ({ signIn, page }) => {
  await signIn("admin");
  await page.goto("/profile");
  await expect(page.getByTestId("profile-role")).toHaveText("admin");
});

test("returns to the landing page once the session is gone", async ({
  signIn,
  page,
}) => {
  const { session } = await signIn();
  await page.goto("/profile");
  await expect(page.getByTestId("profile-page")).toBeVisible();

  await deleteSession(session);
  await page.reload();
  await expect(page).toHaveURL("/");
  await expect(page.getByTestId("sign-in")).toBeVisible();
});

test("signs out", async ({ signIn, page }) => {
  const { session } = await signIn();
  await page.goto("/profile");
  await expect(page.getByTestId("profile-page")).toBeVisible();

  await page.getByTestId("user-email").click();
  await page.getByTestId("sign-out").click();
  await expect(page).toHaveURL("/");
  await expect(page.getByTestId("top-bar-sign-in")).toBeVisible();

  // The session is gone on the server, not only in the browser.
  const res = await authClient(session).getSession({});
  expect(res.user).toBeUndefined();
});
