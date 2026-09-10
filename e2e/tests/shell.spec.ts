import { expect, test } from "../helpers/test";

test("serves the shell through the edge", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("landing-page")).toBeVisible();
  await expect(page.getByTestId("app-header")).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-testmode", "");
});
