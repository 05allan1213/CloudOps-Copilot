import { readFile } from "node:fs/promises";
import { gzipSync } from "node:zlib";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const BUDGETS = Object.freeze({
  main: 300 * 1024,
  three: 200 * 1024,
  specialist: 80 * 1024,
});

export function collectStaticChunkFiles(manifest, entryKey) {
  const files = new Set();
  const visited = new Set();

  function visit(key) {
    if (visited.has(key)) return;
    visited.add(key);
    const chunk = manifest[key];
    if (!chunk) throw new Error(`Manifest chunk not found: ${key}`);
    if (chunk.file?.endsWith(".js")) files.add(chunk.file);
    for (const importedKey of chunk.imports ?? []) visit(importedKey);
  }

  visit(entryKey);
  return [...files].sort();
}

export function classifySpecialistChunks(manifest) {
  const result = { three: [], monitoring: [], virtualization: [] };
  for (const [key, chunk] of Object.entries(manifest)) {
    const identity = [key, chunk.file, chunk.name, chunk.src]
      .filter(Boolean)
      .join(" ")
      .toLowerCase();
    if (!chunk.file?.endsWith(".js")) continue;
    if (identity.includes("three")) result.three.push(chunk.file);
    if (identity.includes("uplot")) result.monitoring.push(chunk.file);
    if (identity.includes("virtual")) result.virtualization.push(chunk.file);
  }
  return Object.fromEntries(
    Object.entries(result).map(([key, files]) => [key, [...new Set(files)].sort()]),
  );
}

export function evaluateBudget(name, gzipBytes, limitBytes) {
  return {
    name,
    gzipBytes,
    limitBytes,
    status: gzipBytes <= limitBytes ? "PASS" : "FAIL",
  };
}

export async function measureGzipFiles(distDir, files) {
  let gzipBytes = 0;
  for (const file of files) {
    gzipBytes += gzipSync(await readFile(resolve(distDir, file))).byteLength;
  }
  return gzipBytes;
}

export async function buildBudgetReport(distDir) {
  const manifestPath = resolve(distDir, ".vite/manifest.json");
  const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  const entry = Object.entries(manifest).find(([, chunk]) => chunk.isEntry);
  if (!entry) throw new Error("Vite manifest has no entry chunk");

  const mainFiles = collectStaticChunkFiles(manifest, entry[0]);
  const specialists = classifySpecialistChunks(manifest);
  const mainBytes = await measureGzipFiles(distDir, mainFiles);
  const checks = [evaluateBudget("main", mainBytes, BUDGETS.main)];

  const specialistLimits = {
    three: BUDGETS.three,
    monitoring: BUDGETS.specialist,
    virtualization: BUDGETS.specialist,
  };
  for (const [name, files] of Object.entries(specialists)) {
    if (files.length === 0) continue;
    checks.push(
      evaluateBudget(name, await measureGzipFiles(distDir, files), specialistLimits[name]),
    );
  }

  return {
    generatedAt: new Date().toISOString(),
    entry: entry[0],
    mainFiles,
    specialists,
    checks,
    status: checks.every((check) => check.status === "PASS") ? "PASS" : "FAIL",
  };
}

async function main() {
  const distDir = resolve(process.cwd(), process.argv[2] ?? "dist");
  const report = await buildBudgetReport(distDir);
  process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
  if (report.status !== "PASS") process.exitCode = 1;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  });
}
