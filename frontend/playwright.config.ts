import { defineConfig } from "@playwright/test";

const browserExecutable = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
const networkIsolated = process.env.PLAYWRIGHT_NETWORK_ISOLATED === "1";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  workers: 1,
  timeout: 30_000,
  expect: {
    timeout: 8_000,
    toHaveScreenshot: {
      animations: "disabled",
      caret: "hide",
      maxDiffPixelRatio: 0.01,
    },
  },
  reporter: [["list"]],
  use: {
    baseURL: "http://127.0.0.1:4173",
    browserName: "chromium",
    colorScheme: "dark",
    locale: "en-US",
    timezoneId: "UTC",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    launchOptions: {
      ...(browserExecutable ? { executablePath: browserExecutable } : {}),
      ...(networkIsolated ? { args: ["--no-sandbox"] } : {}),
    },
  },
  webServer: [
    {
      command: "node tests/e2e/fixture-server.mjs",
      url: "http://127.0.0.1:18082/fixture/state",
      reuseExistingServer: false,
      timeout: 20_000,
    },
    {
      command: "VITE_API_PROXY_TARGET=http://127.0.0.1:18082 npm run dev -- --host 127.0.0.1 --port 4173 --strictPort",
      url: "http://127.0.0.1:4173",
      reuseExistingServer: false,
      timeout: 30_000,
    },
  ],
});
