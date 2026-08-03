import { expect, test, type Page } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

import { analyzePng, captureReviewScreenshot, evidenceRoot, expectCleanPage, expectNoPageOverflow, observePage, writeJson } from "./evidence";

test.use({ video: "on", trace: "on" });

async function setTheme(page: Page, theme: "light" | "dark") {
  if (await page.locator("html").getAttribute("data-theme") !== theme) await page.getByTestId("theme-toggle").click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
}

function commonMetadata(theme: string, surface: string) {
  return {
    theme,
    surface,
    candidate: "Nuxt UI 4.10.0 + Tailwind CSS 4.3.3",
    rendererVersions: "uPlot 1.6.32; Three.js 0.185.1; TanStack Vue Virtual 3.13.35",
  };
}

test.describe("specialist renderers and adaptive workspace", () => {
  test("Monitoring renders 7,200 points with keyboard, zoom range, null gaps and empty truth", async ({ page }, testInfo) => {
    const diagnostics = observePage(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/monitoring");
    await expect(page.getByText("7,200 点", { exact: true })).toBeVisible();
    await expect(page.getByTestId("monitor-partial")).toContainText("Partial Provider");
    await expect(page.getByTestId("monitor-chart")).toBeVisible();

    const chartCanvas = page.getByTestId("monitor-chart").locator("canvas").first();
    await expect(chartCanvas).toBeVisible();
    const canvasBuffer = await chartCanvas.screenshot({ animations: "disabled" });
    const pixels = analyzePng(canvasBuffer);
    expect(pixels.nonBackgroundRatio).toBeGreaterThan(0.01);
    expect(pixels.quantizedColors).toBeGreaterThan(12);
    await writeJson("metrics/monitoring-canvas-pixels.json", pixels);

    const before = await page.locator("#synchronized-title").textContent();
    await page.getByTestId("monitor-chart-surface").focus();
    await page.keyboard.press("ArrowLeft");
    await expect.poll(async () => page.locator("#synchronized-title").textContent()).not.toBe(before);
    await page.getByRole("button", { name: "15m", exact: true }).click();
    await expect(page.getByText(/视窗 \d[\d,]* 点/)).not.toContainText("7,200");
    await page.getByTestId("toggle-monitor-empty").click();
    await expect(page.getByTestId("monitor-empty")).toBeVisible();
    await expect(page.getByTestId("monitor-partial")).toBeVisible();
    await page.getByTestId("toggle-monitor-empty").click();
    await expect(page.getByTestId("monitor-chart")).toBeVisible();
    await expectCleanPage(diagnostics, testInfo);
  });

  test("Atlas canvas is nonblank and lifecycle, resize, fallback and disposal paths are explicit", async ({ page }, testInfo) => {
    const diagnostics = observePage(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/atlas");
    await expect(page.getByTestId("atlas-fps")).toContainText(/[1-9]\d* FPS/);
    const canvas = page.getByTestId("atlas-canvas-host").locator("canvas");
    await expect(canvas).toBeVisible();
    const buffer = await canvas.screenshot({ animations: "disabled" });
    const pixels = analyzePng(buffer);
    expect(pixels.nonBackgroundRatio).toBeGreaterThan(0.06);
    expect(pixels.chromaticRatio).toBeGreaterThan(0.002);
    expect(pixels.quantizedColors).toBeGreaterThan(16);
    await writeJson("metrics/atlas-canvas-pixels.json", pixels);

    const widthWithInspector = (await canvas.boundingBox())?.width ?? 0;
    await page.getByRole("button", { name: "关闭 Atlas Inspector" }).click();
    const widthWithoutInspector = (await canvas.boundingBox())?.width ?? 0;
    expect(widthWithoutInspector).toBeGreaterThan(widthWithInspector + 180);
    await page.getByRole("button", { name: "定位关键节点" }).click();
    await expect(page.getByTestId("atlas-inspector")).toBeVisible();

    await page.evaluate(() => {
      Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await expect(page.getByTestId("atlas-fps")).toContainText("hidden/paused");
    await page.evaluate(() => {
      Reflect.deleteProperty(document, "hidden");
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await expect(page.getByTestId("atlas-fps")).toContainText(/[1-9]\d* FPS/);
    await writeJson("metrics/atlas-visibility-injection.json", { status: "PASS", method: "browser visibilitychange fault injection" });

    await page.getByTestId("atlas-context-loss").click();
    await expect(page.getByTestId("atlas-context-lost")).toBeVisible();
    await expect(page.getByTestId("atlas-structured")).toBeVisible();

    await page.goto("/atlas?webgl=fail");
    await expect(page.getByTestId("atlas-webgl-failed")).toBeVisible();
    await expect(page.getByTestId("atlas-structured")).toContainText("200 / 200 资源");

    await page.goto("/atlas");
    await expect(page.getByTestId("atlas-fps")).toContainText(/[1-9]\d* FPS/);
    const disposableCanvas = await page.getByTestId("atlas-canvas-host").locator("canvas").elementHandle();
    expect(disposableCanvas).not.toBeNull();
    await page.getByTestId("atlas-dispose").click();
    await expect(page.getByTestId("atlas-disposed")).toContainText("dispose 已执行");
    await expect(page.getByTestId("atlas-canvas-host").locator("canvas")).toHaveCount(0);
    const contextLostOnDispose = await disposableCanvas?.evaluate((element) => {
      const canvasElement = element as HTMLCanvasElement;
      const context = canvasElement.getContext("webgl2") ?? canvasElement.getContext("webgl");
      return context?.isContextLost() ?? false;
    });
    expect(contextLostOnDispose).toBe(true);
    await writeJson("metrics/atlas-disposal.json", { status: "PASS", contextLostOnDispose, canvasRemoved: true });
    await page.goto("/incidents");
    await expect(page.locator('canvas[data-testid="atlas-canvas"]')).toHaveCount(0);
    await expectCleanPage(diagnostics, testInfo);
  });

  test("Agent retains operational context while secondary rails progressively collapse", async ({ page }, testInfo) => {
    const diagnostics = observePage(page);
    const results: Record<string, unknown> = {};
    for (const viewport of [
      { width: 1920, height: 1080 },
      { width: 1440, height: 900 },
      { width: 1280, height: 800 },
      { width: 1024, height: 768 },
    ]) {
      await page.setViewportSize(viewport);
      await page.goto("/agent");
      await expect(page.getByTestId("agent-context-strip")).toContainText("Incident");
      await expect(page.getByTestId("agent-context-strip")).toContainText("Evidence");
      const contextDisplay = await page.getByTestId("agent-context-rail").evaluate((element) => getComputedStyle(element).display);
      const evidenceDisplay = await page.getByTestId("agent-evidence-rail").evaluate((element) => getComputedStyle(element).display);
      results[`${viewport.width}x${viewport.height}`] = { contextDisplay, evidenceDisplay, overflow: await expectNoPageOverflow(page) };
      if (viewport.width === 1920) {
        expect(contextDisplay).not.toBe("none");
        expect(evidenceDisplay).not.toBe("none");
      }
      if (viewport.width === 1024) {
        expect(contextDisplay).toBe("none");
        expect(evidenceDisplay).toBe("none");
        await expect(page.getByRole("button", { name: "打开调查上下文" })).toBeVisible();
        await expect(page.getByRole("button", { name: "打开 Evidence" })).toBeVisible();
      }
    }
    await writeJson("metrics/agent-collapse.json", results);
    await expectCleanPage(diagnostics, testInfo);
  });
});

test.describe("Owner review evidence", () => {
  test("capture deterministic Light/Dark and desktop-degradation review set", async ({ page }, testInfo) => {
    test.setTimeout(150_000);
    const diagnostics = observePage(page);
    const shots: string[] = [];

    for (const viewport of [{ width: 1920, height: 1080 }, { width: 1440, height: 900 }]) {
      await page.setViewportSize(viewport);
      for (const theme of ["light", "dark"] as const) {
        await page.goto("/incidents");
        await setTheme(page, theme);
        await page.locator('[data-testid^="inspect-inc-"]').first().click();
        shots.push(await captureReviewScreenshot(page, `incident-inspector-${viewport.width}x${viewport.height}-${theme}`, commonMetadata(theme, "Incident dense table and Inspector")));
        await page.getByRole("button", { name: "关闭", exact: true }).click();

        await page.goto("/monitoring");
        await setTheme(page, theme);
        await expect(page.getByTestId("monitor-chart")).toBeVisible();
        shots.push(await captureReviewScreenshot(page, `monitoring-${viewport.width}x${viewport.height}-${theme}`, commonMetadata(theme, "Monitoring uPlot and synchronized table")));

        await page.goto("/atlas");
        await setTheme(page, theme);
        await expect(page.getByTestId("atlas-fps")).toContainText(/[1-9]\d* FPS/);
        shots.push(await captureReviewScreenshot(page, `atlas-${viewport.width}x${viewport.height}-${theme}`, commonMetadata(theme, "Three.js 200-node Atlas and Inspector")));
      }
    }

    await page.setViewportSize({ width: 1440, height: 900 });
    for (const theme of ["light", "dark"] as const) {
      await page.goto("/settings");
      await setTheme(page, theme);
      await page.getByTestId("revision-summary").fill("Owner review partial Provider result");
      await page.getByTestId("apply-settings").click();
      await expect(page.getByTestId("partial-result")).toBeVisible();
      shots.push(await captureReviewScreenshot(page, `settings-partial-1440x900-${theme}`, commonMetadata(theme, "Settings draft, revision and partial result")));

      await page.goto("/states");
      await setTheme(page, theme);
      await page.getByRole("button", { name: "Verification Failed", exact: true }).click();
      shots.push(await captureReviewScreenshot(page, `exceptional-states-1440x900-${theme}`, commonMetadata(theme, "Exceptional domain state and SSE lifecycle")));
    }

    for (const viewport of [{ width: 1280, height: 800 }, { width: 1024, height: 768 }]) {
      await page.setViewportSize(viewport);
      await page.goto("/agent");
      await setTheme(page, "light");
      shots.push(await captureReviewScreenshot(page, `agent-${viewport.width}x${viewport.height}-light`, commonMetadata("light", "Agent progressive desktop collapse")));
      await expectNoPageOverflow(page);
    }

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/incidents");
    await setTheme(page, "light");
    const cdp = await page.context().newCDPSession(page);
    for (const zoom of [1.25, 1.5]) {
      const cssWidth = Math.round(1440 / zoom);
      const cssHeight = Math.round(900 / zoom);
      await cdp.send("Emulation.setDeviceMetricsOverride", {
        width: cssWidth,
        height: cssHeight,
        screenWidth: 1440,
        screenHeight: 900,
        deviceScaleFactor: zoom,
        mobile: false,
      });
      await expectNoPageOverflow(page);
      shots.push(await captureReviewScreenshot(page, `incident-browser-zoom-${Math.round(zoom * 100)}-light`, {
        ...commonMetadata("light", "Chromium DevTools browser-zoom equivalent"),
        zoom,
        cssViewport: { width: cssWidth, height: cssHeight },
        physicalViewport: { width: 1440, height: 900 },
      }));
    }
    await cdp.send("Emulation.clearDeviceMetricsOverride");

    await page.setViewportSize({ width: 1024, height: 768 });
    await page.goto("/settings");
    await page.evaluate(() => { document.documentElement.style.fontSize = "200%"; });
    await expectNoPageOverflow(page);
    shots.push(await captureReviewScreenshot(page, "settings-1024x768-text-200-light", {
      ...commonMetadata("light", "Practical 200 percent root text enlargement"),
      textScale: 2,
    }));
    await page.evaluate(() => { document.documentElement.style.removeProperty("font-size"); });

    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.goto("/agent");
    const motion = await page.locator(".agent-workspace").evaluate((element) => ({
      animationDuration: getComputedStyle(element).animationDuration,
      transitionDuration: getComputedStyle(element).transitionDuration,
    }));
    expect(Number.parseFloat(motion.animationDuration)).toBeLessThanOrEqual(0.001);
    await writeJson("metrics/reduced-motion.json", motion);

    await mkdir(path.join(evidenceRoot, "review"), { recursive: true });
    await writeFile(path.join(evidenceRoot, "review", "index.json"), `${JSON.stringify({ screenshots: shots.map((shot) => path.relative(evidenceRoot, shot)) }, null, 2)}\n`);
    await testInfo.attach("owner-review-index", { path: path.join(evidenceRoot, "review", "index.json"), contentType: "application/json" });
    await expectCleanPage(diagnostics, testInfo);
  });
});
