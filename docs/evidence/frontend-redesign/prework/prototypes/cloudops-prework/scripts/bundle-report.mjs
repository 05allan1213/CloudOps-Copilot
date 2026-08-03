import { gzipSync } from "node:zlib";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const root = process.cwd();
const manifestPath = path.join(root, "dist/.vite/manifest.json");
const outputDir = path.resolve(root, "../../output/playwright/prototype/metrics");
const manifest = JSON.parse(await readFile(manifestPath, "utf8"));

const chunks = [];
for (const [source, entry] of Object.entries(manifest)) {
  if (!entry.file?.endsWith(".js")) continue;
  const bytes = await readFile(path.join(root, "dist", entry.file));
  chunks.push({
    source,
    file: entry.file,
    entry: Boolean(entry.isEntry),
    dynamicEntry: Boolean(entry.isDynamicEntry),
    rawBytes: bytes.byteLength,
    gzipBytes: gzipSync(bytes).byteLength,
  });
}

chunks.sort((left, right) => right.gzipBytes - left.gzipBytes);
const findChunk = (pattern) => chunks.find((chunk) => pattern.test(`${chunk.source} ${chunk.file}`));
const budgets = {
  main: { chunk: chunks.find((chunk) => chunk.entry), maxGzipBytes: 300 * 1024 },
  three: { chunk: findChunk(/three\.module/i), maxGzipBytes: 200 * 1024 },
  monitoring: { chunk: findChunk(/MonitoringLab/i), maxGzipBytes: 80 * 1024 },
  virtualization: { chunk: findChunk(/node_modules\/@tanstack\/virtual-core|\/esm-/i), maxGzipBytes: 80 * 1024 },
};

const checks = Object.fromEntries(Object.entries(budgets).map(([name, value]) => [name, {
  file: value.chunk?.file ?? null,
  gzipBytes: value.chunk?.gzipBytes ?? null,
  maxGzipBytes: value.maxGzipBytes,
  status: value.chunk && value.chunk.gzipBytes <= value.maxGzipBytes ? "PASS" : "FAIL",
}]));
const report = {
  generatedAt: new Date().toISOString(),
  status: Object.values(checks).every((check) => check.status === "PASS") ? "PASS" : "FAIL",
  checks,
  chunks,
};

await mkdir(outputDir, { recursive: true });
await writeFile(path.join(outputDir, "bundle-report.json"), `${JSON.stringify(report, null, 2)}\n`);
console.log(JSON.stringify(report, null, 2));
