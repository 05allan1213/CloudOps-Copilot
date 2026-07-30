import { expect, type Page, type TestInfo } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { PNG } from "pngjs";

export const evidenceRoot = path.resolve(process.cwd(), "../../output/playwright/prototype");

interface Diagnostics {
  errors: string[];
  warnings: string[];
}

export function observePage(page: Page): Diagnostics {
  const diagnostics: Diagnostics = { errors: [], warnings: [] };
  page.on("console", (message) => {
    if (message.type() === "error") diagnostics.errors.push(message.text());
    if (message.type() === "warning") diagnostics.warnings.push(message.text());
  });
  page.on("pageerror", (error) => diagnostics.errors.push(error.message));
  return diagnostics;
}

export async function expectCleanPage(diagnostics: Diagnostics, testInfo: TestInfo) {
  const unexpectedWarnings = diagnostics.warnings.filter((warning) => !warning.includes("GL Driver Message") || !warning.includes("ReadPixels"));
  await testInfo.attach("browser-diagnostics", {
    body: Buffer.from(JSON.stringify({ ...diagnostics, unexpectedWarnings }, null, 2)),
    contentType: "application/json",
  });
  expect(diagnostics.errors, "application Console/Page errors").toEqual([]);
  expect(unexpectedWarnings, "unexplained browser warnings").toEqual([]);
}

export async function expectNoPageOverflow(page: Page) {
  const dimensions = await page.evaluate(() => ({
    bodyClient: document.body.clientWidth,
    bodyScroll: document.body.scrollWidth,
    rootClient: document.documentElement.clientWidth,
    rootScroll: document.documentElement.scrollWidth,
  }));
  expect(dimensions.bodyScroll).toBeLessThanOrEqual(dimensions.bodyClient + 1);
  expect(dimensions.rootScroll).toBeLessThanOrEqual(dimensions.rootClient + 1);
  return dimensions;
}

export function analyzePng(buffer: Buffer) {
  const png = PNG.sync.read(buffer);
  const origin = [png.data[0], png.data[1], png.data[2]];
  let sampled = 0;
  let nonBackground = 0;
  let chromatic = 0;
  const colors = new Set<string>();

  for (let y = 0; y < png.height; y += 3) {
    for (let x = 0; x < png.width; x += 3) {
      const offset = (y * png.width + x) * 4;
      const red = png.data[offset];
      const green = png.data[offset + 1];
      const blue = png.data[offset + 2];
      const alpha = png.data[offset + 3];
      if (alpha === 0) continue;
      sampled += 1;
      if (Math.max(Math.abs(red - origin[0]), Math.abs(green - origin[1]), Math.abs(blue - origin[2])) > 10) nonBackground += 1;
      if (Math.max(red, green, blue) - Math.min(red, green, blue) > 14) chromatic += 1;
      colors.add(`${Math.round(red / 16)}:${Math.round(green / 16)}:${Math.round(blue / 16)}`);
    }
  }

  return {
    width: png.width,
    height: png.height,
    sampled,
    nonBackground,
    nonBackgroundRatio: Number((nonBackground / Math.max(1, sampled)).toFixed(4)),
    chromatic,
    chromaticRatio: Number((chromatic / Math.max(1, sampled)).toFixed(4)),
    quantizedColors: colors.size,
  };
}

export async function writeJson(relativePath: string, value: unknown) {
  const target = path.join(evidenceRoot, relativePath);
  await mkdir(path.dirname(target), { recursive: true });
  await writeFile(target, `${JSON.stringify(value, null, 2)}\n`);
  return target;
}

export async function captureReviewScreenshot(page: Page, name: string, metadata: Record<string, unknown>) {
  const target = path.join(evidenceRoot, "review", `${name}.png`);
  await mkdir(path.dirname(target), { recursive: true });
  await page.screenshot({ path: target, animations: "disabled", caret: "hide" });
  const sha = execFileSync("git", ["rev-parse", "HEAD"], { cwd: process.cwd(), encoding: "utf8" }).trim();
  await writeJson(`review/${name}.json`, {
    sha,
    url: page.url(),
    viewport: page.viewportSize(),
    browser: page.context().browser()?.browserType().name() ?? "unknown",
    dataSource: "deterministic isolated fixture",
    readOnly: true,
    ...metadata,
  });
  return target;
}
