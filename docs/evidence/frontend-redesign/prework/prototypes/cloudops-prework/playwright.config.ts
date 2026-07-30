import { defineConfig, devices } from "@playwright/test";
import { fileURLToPath } from "node:url";

const outputRoot = fileURLToPath(new URL("../../output/playwright/prototype/", import.meta.url));
const chromiumExecutable = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
const networkIsolated = process.env.PLAYWRIGHT_NETWORK_ISOLATED === "1";
const artifactSet = process.env.PLAYWRIGHT_ARTIFACT_SET ?? "test-results";

export default defineConfig({
  testDir: "./tests",
  outputDir: `${outputRoot}/${artifactSet}`,
  fullyParallel: false,
  workers: 1,
  timeout: 45_000,
  expect: { timeout: 8_000 },
  reporter: [
    ["list"],
    ["json", { outputFile: `${outputRoot}/results.json` }],
    ["html", { outputFolder: `${outputRoot}/html-report`, open: "never" }],
  ],
  use: {
    baseURL: "http://127.0.0.1:4187",
    locale: "zh-CN",
    timezoneId: "UTC",
    colorScheme: "light",
    contextOptions: { reducedMotion: "no-preference" },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
  },
  projects: [
    {
      name: "chromium",
      testMatch: "**/*.spec.ts",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: {
          ...(chromiumExecutable ? { executablePath: chromiumExecutable } : {}),
          ...(networkIsolated ? { args: ["--no-sandbox"] } : {}),
        },
      },
    },
    {
      name: "firefox",
      testMatch: "**/cross-browser.spec.ts",
      use: { ...devices["Desktop Firefox"] },
    },
    {
      name: "webkit",
      testMatch: "**/cross-browser.spec.ts",
      use: { ...devices["Desktop Safari"] },
    },
  ],
  webServer: {
    command: "npm run dev -- --port 4187 --strictPort",
    url: "http://127.0.0.1:4187",
    reuseExistingServer: true,
    timeout: 30_000,
  },
});
