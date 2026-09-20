import { expect, test } from "../helpers/test";

test("serves the shell through the edge", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("landing-page")).toBeVisible();
  await expect(page.getByTestId("app-header")).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-testmode", "");
});

test("navigates the sidebar and marks the current page", async ({
  signIn,
  page,
}) => {
  await signIn();
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
  signIn,
  page,
}) => {
  await signIn();
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

test("denies a user the admin area", async ({ signIn, page }) => {
  await signIn();
  await page.goto("/admin");
  await expect(page.getByTestId("access-denied")).toBeVisible();
  await expect(page.getByTestId("admin-nav")).toHaveCount(0);
});

test("shows an administrator the admin area", async ({ signIn, page }) => {
  await signIn("admin");
  await page.goto("/transactions");
  await page.getByTestId("user-email").click();
  await page.getByTestId("menu-admin").click();
  await expect(page).toHaveURL("/admin");
  await expect(page.getByTestId("admin-nav")).toBeVisible();
  await expect(page.getByTestId("admin-page")).toBeVisible();
});

test("keeps the chosen scheme across a reload", async ({ signIn, page }) => {
  await signIn();
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
  signIn,
  page,
}) => {
  await signIn();
  await page.goto("/transactions");
  await expect(page.getByTestId("sidebar")).not.toHaveAttribute(
    "data-collapsed",
  );
  await page.getByTestId("sidebar-collapse").click();
  await expect(page.getByTestId("sidebar")).toHaveAttribute("data-collapsed");
  await page.reload();
  await expect(page.getByTestId("sidebar")).toHaveAttribute("data-collapsed");
});

test("toggles the activity sheet from its icon", async ({ signIn, page }) => {
  await signIn();
  await page.goto("/transactions");
  await page.getByTestId("activity-icon").click();
  await expect(page.getByTestId("activity-sheet")).toBeVisible();
  await page.getByTestId("activity-icon").click();
  await expect(page.getByTestId("activity-sheet")).toBeHidden();
  await page.getByTestId("activity-icon").click();
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("activity-sheet")).toBeHidden();
});
