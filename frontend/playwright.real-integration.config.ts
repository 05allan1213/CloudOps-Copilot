import { defineConfig } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = path.resolve(frontendRoot, "..");
const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "run-id-required";
const invocationID = process.env.CLOUDOPS_REAL_INTEGRATION_INVOCATION_ID || "invocation-id-required";
const browserExecutable = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;

export default defineConfig({
  testDir: "./tests/real-integration",
  testMatch: "**/*.real.spec.ts",
  outputDir: path.join(repoRoot, ".cloudops", "integration", runID, "playwright", invocationID),
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 30 * 60_000,
  expect: { timeout: 30_000 },
  reporter: [["list"]],
  use: {
    baseURL: process.env.CLOUDOPS_REAL_INTEGRATION_BASE_URL || "http://127.0.0.1:18080",
    browserName: "chromium",
    locale: "zh-CN",
    timezoneId: "Asia/Shanghai",
    colorScheme: "dark",
    viewport: { width: 1440, height: 900 },
    actionTimeout: 30_000,
    navigationTimeout: 60_000,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
    launchOptions: {
      ...(browserExecutable ? { executablePath: browserExecutable } : {}),
      args: ["--no-sandbox"],
    },
  },
});
