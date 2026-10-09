import { defineConfig, devices } from "@playwright/test";
import { baseURL, recording } from "./helpers/config";

export default defineConfig({
  testDir: "./tests",
  // When recording, the proxy spaces each provider's upstream calls by the
  // provider's interval, and Massive's is 12s. A key can queue behind every
  // other key the suite sends Massive, so the waits stretch to cover the
  // whole queue and one run records everything.
  timeout: recording ? 600_000 : 30_000,
  expect: { timeout: recording ? 300_000 : 5_000 },
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
