import { defineConfig, devices } from "@playwright/test";

// The stack under test is already running (see deploy/compose/compose.e2e.yaml),
// so there is no webServer. Nothing is mocked: the specs drive the real chain
// from the browser through Next, torii, kagami and Postgres.
export default defineConfig({
  testDir: "./e2e-compose",
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: "http://localhost:3000",
    trace: "retain-on-failure",
    launchOptions: {
      // Torii and the browser must see the stub identity provider under the
      // same name, because the issuer URL has to match exactly.
      args: ["--host-resolver-rules=MAP mock-oauth2 127.0.0.1"],
    },
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
