import { defineConfig } from "@playwright/test";

const fixturePort = 18147;
const appPort = 4317;
const fixtureOrigin = `http://127.0.0.1:${fixturePort}`;
const appOrigin = `http://127.0.0.1:${appPort}`;

process.env.CLOUDOPS_E2E_FIXTURE_ORIGIN = fixtureOrigin;

export default defineConfig({
  testDir: ".",
  fullyParallel: false,
  workers: 1,
  timeout: 240_000,
  expect: { timeout: 150_000 },
  reporter: [["list"]],
  use: {
    baseURL: appOrigin,
    browserName: "chromium",
    colorScheme: "light",
    locale: "zh-CN",
    timezoneId: "Asia/Shanghai",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: [
    {
      command: `CLOUDOPS_E2E_FIXTURE_PORT=${fixturePort} CLOUDOPS_E2E_APP_ORIGIN=${appOrigin} node fixture-server.mjs`,
      url: `${fixtureOrigin}/fixture/state`,
      reuseExistingServer: false,
      timeout: 20_000,
    },
    {
      command: `VITE_API_PROXY_TARGET=${fixtureOrigin} npm run dev -- --host 127.0.0.1 --port ${appPort} --strictPort`,
      url: appOrigin,
      reuseExistingServer: false,
      timeout: 60_000,
    },
  ],
});
