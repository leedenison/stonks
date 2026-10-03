import { defineConfig, devices } from "@playwright/test";
import { baseURL } from "./helpers/config";

export default defineConfig({
  testDir: "./tests",
  globalSetup: "./helpers/setup.ts",
  fullyParallel: true,
  retries: 0,
  reporter: "list",
  use: {
    baseURL,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
