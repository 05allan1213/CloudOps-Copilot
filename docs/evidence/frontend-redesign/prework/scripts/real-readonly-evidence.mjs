import { mkdir, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

import { chromium } from "../../../../../frontend/node_modules/playwright/index.mjs";

const baseURL = process.env.CLOUDOPS_REAL_BASE_URL || "http://127.0.0.1:18080";
const outputDirectory = fileURLToPath(new URL("../output/playwright/production-real/", import.meta.url));
const readMethods = new Set(["GET", "HEAD", "OPTIONS"]);

await mkdir(outputDirectory, { recursive: true });

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({
  colorScheme: "light",
  locale: "zh-CN",
  timezoneId: "Asia/Shanghai",
  viewport: { width: 1440, height: 900 },
});
const page = await context.newPage();
const consoleErrors = [];
const browserWarnings = [];
const pageErrors = [];
const failedRequests = [];
const failedResponses = [];
const mutations = [];

page.on("console", (message) => {
  const value = message.text();
  if (message.type() === "warning" && /^\[\.WebGL-.+GPU stall due to ReadPixels$/.test(value)) {
    browserWarnings.push(value);
  } else if (message.type() === "error" || message.type() === "warning") {
    consoleErrors.push(value);
  }
});
page.on("pageerror", (error) => pageErrors.push(error.message));
page.on("request", (request) => {
  if (!readMethods.has(request.method())) mutations.push(`${request.method()} ${request.url()}`);
});
page.on("requestfailed", (request) => {
  const error = request.failure()?.errorText || "failed";
  if (!/notification-events/.test(request.url()) || !/(?:ERR_ABORTED|NS_BINDING_ABORTED)/.test(error)) {
    failedRequests.push(`${request.method()} ${request.url()}: ${error}`);
  }
});
page.on("response", (response) => {
  if (response.status() >= 400) failedResponses.push(`${response.status()} ${response.url()}`);
});

try {
  const overviewResponsePromise = page.waitForResponse((response) =>
    response.request().method() === "GET"
      && new URL(response.url()).pathname === "/api/v1/overview"
      && response.status() === 200);

  await page.goto(`${baseURL}/overview`, { waitUntil: "domcontentloaded" });
  const overviewResponse = await overviewResponsePromise;
  const overview = await overviewResponse.json();
  await page.getByRole("heading", { level: 1, name: "集群运行图谱" }).waitFor();
  await page.locator("[data-testid='overview-operations-atlas']").waitFor();
  await page.waitForTimeout(800);

  const atlas = overview.atlas;
  if (overview.bootstrap?.product !== "CloudOps" || overview.bootstrap?.contract !== "V1") {
    throw new Error("Overview did not return the current CloudOps V1 contract");
  }
  if (atlas?.provider_state !== "available" || atlas?.source?.provider !== "kubernetes") {
    throw new Error("Overview Atlas did not return an available Kubernetes Provider projection");
  }
  if (!atlas.source.identity || !atlas.source.collected_at || !Array.isArray(atlas.nodes) || atlas.nodes.length === 0) {
    throw new Error("Overview Atlas did not expose Provider identity, collection time, and real nodes");
  }

  await page.screenshot({
    path: `${outputDirectory}/overview-real-1440x900-light.png`,
    fullPage: false,
  });

  await page.getByRole("button", { name: "结构化" }).click();
  await page.locator("[data-testid='atlas-structured-view'] .resource-list button").first().click();
  await page.getByRole("heading", { level: 2 }).waitFor();
  await page.screenshot({
    path: `${outputDirectory}/overview-real-structured-selected-1440x900-light.png`,
    fullPage: false,
  });

  const theme = await page.evaluate(() => document.documentElement.dataset.theme || "");
  const result = {
    status: "PASS",
    recorded_at: new Date().toISOString(),
    base_url: baseURL,
    route: page.url(),
    browser: "chromium",
    viewport: "1440x900",
    theme,
    data_source: "real local CloudOps API backed by the Kubernetes typed Provider",
    request: {
      method: overviewResponse.request().method(),
      path: new URL(overviewResponse.url()).pathname,
      status: overviewResponse.status(),
      request_id: overviewResponse.headers()["x-request-id"] || "",
      trace_id: overviewResponse.headers()["x-trace-id"] || "",
    },
    contract: {
      product: overview.bootstrap.product,
      version: overview.bootstrap.contract,
      scope_id: overview.bootstrap.active_scope?.id || "",
      cluster_id: overview.bootstrap.active_scope?.cluster_id || "",
    },
    provider: {
      state: atlas.provider_state,
      identity: atlas.source.identity,
      server_version: atlas.source.server_version || "",
      collected_at: atlas.source.collected_at,
      freshness: atlas.freshness?.state || "",
      nodes: atlas.nodes.length,
      edges: Array.isArray(atlas.edges) ? atlas.edges.length : 0,
      issues: Array.isArray(atlas.issues) ? atlas.issues.length : 0,
      partial: atlas.partial === true,
      truncated: atlas.truncated === true,
    },
    mutations,
    console_errors: consoleErrors,
    browser_warnings: browserWarnings,
    page_errors: pageErrors,
    failed_requests: failedRequests,
    failed_responses: failedResponses,
  };

  if (theme !== "light" || mutations.length || consoleErrors.length || pageErrors.length || failedRequests.length || failedResponses.length) {
    result.status = "FAIL";
  }
  await writeFile(`${outputDirectory}/overview-real-readonly.json`, `${JSON.stringify(result, null, 2)}\n`);
  if (result.status !== "PASS") throw new Error(`Real read-only browser evidence failed: ${JSON.stringify(result)}`);
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
} finally {
  await browser.close();
}
