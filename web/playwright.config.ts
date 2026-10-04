import { defineConfig, devices } from "@playwright/test";

const PORT = 3100;
const SERVER_START_TIMEOUT_MS = 180_000;

// The specs run against the production build with /api mocked in the browser
// (see e2e/fixtures.ts). The stack-wide run against compose is a separate CI
// job (P5.2c).
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: "on-first-retry",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `npm run build && npm run start -- --port ${PORT}`,
    url: `http://localhost:${PORT}/login`,
    reuseExistingServer: !process.env.CI,
    timeout: SERVER_START_TIMEOUT_MS,
  },
});
