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
  // Extending validity and refreshing corporate events each reach every held
  // instrument in the database. Each spec runs alone, once every other spec
  // has removed its users.
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
      testIgnore: /(extend-validity|refresh-corporate-events)\.spec\.ts/,
    },
    {
      name: "system",
      use: { ...devices["Desktop Chrome"] },
      testMatch: /extend-validity\.spec\.ts/,
      dependencies: ["chromium"],
    },
    {
      name: "corporate-events",
      use: { ...devices["Desktop Chrome"] },
      testMatch: /refresh-corporate-events\.spec\.ts/,
      dependencies: ["system"],
    },
  ],
});
