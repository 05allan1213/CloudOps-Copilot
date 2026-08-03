#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import ts from "../frontend/node_modules/typescript/lib/typescript.js";
import yaml from "../frontend/node_modules/js-yaml/index.js";

const repoRoot = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..");
const args = new Map();
for (let index = 2; index < process.argv.length; index += 1) {
  const value = process.argv[index];
  if (!value.startsWith("--")) continue;
  const [name, inline] = value.split("=", 2);
  if (inline !== undefined) {
    args.set(name, inline);
  } else if (process.argv[index + 1] && !process.argv[index + 1].startsWith("--")) {
    args.set(name, process.argv[index + 1]);
    index += 1;
  } else {
    args.set(name, true);
  }
}

function listFiles(root, extensions) {
  const files = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const absolute = path.join(root, entry.name);
    if (entry.isDirectory()) {
      files.push(...listFiles(absolute, extensions));
    } else if (extensions.some((extension) => entry.name.endsWith(extension))) {
      files.push(absolute);
    }
  }
  return files;
}

function relative(file) {
  return path.relative(repoRoot, file).split(path.sep).join("/");
}

function sourceText(file) {
  const raw = fs.readFileSync(file, "utf8");
  if (!file.endsWith(".vue")) return raw;
  const match = raw.match(/<script\s+setup(?:\s+lang=["']ts["'])?[^>]*>([\s\S]*?)<\/script>/i);
  return match?.[1] ?? "";
}

function parseTypeScript(file) {
  return ts.createSourceFile(file, sourceText(file), ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
}

function isExported(node) {
  return Boolean(node.modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword));
}

function walk(node, visitor) {
  visitor(node);
  ts.forEachChild(node, (child) => walk(child, visitor));
}

const apiRoot = path.join(repoRoot, "frontend/src/api");
const apiFiles = listFiles(apiRoot, [".ts"]).filter((file) => !file.endsWith(".test.ts") && !file.endsWith("/client.ts"));
const clientFunctions = [];
const networkCallNames = new Set(["getJSON", "getJSONWithCache", "postJSON", "postJSONWithMeta", "patchJSON", "deleteJSON", "apiURL"]);

for (const file of apiFiles) {
  const source = parseTypeScript(file);
  for (const statement of source.statements) {
    if (!ts.isFunctionDeclaration(statement) || !statement.name || !isExported(statement)) continue;
    const calls = new Set();
    walk(statement, (node) => {
      if (ts.isNewExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === "EventSource") {
        calls.add("EventSource");
        return;
      }
      if (!ts.isCallExpression(node)) return;
      const expression = node.expression;
      const name = ts.isIdentifier(expression) ? expression.text : "";
      if (networkCallNames.has(name)) calls.add(name);
    });
    const body = statement.getText(source);
    if (calls.size === 0 && !/EventSource|postAlertCommand|postCommand|listResources|listTypedResources|getTypedResource/.test(body)) continue;
    clientFunctions.push({
      name: statement.name.text,
      file: relative(file),
      calls: [...calls].sort(),
    });
  }
}

const consumerFiles = listFiles(path.join(repoRoot, "frontend/src"), [".ts", ".vue"])
  .filter((file) => !file.includes(`${path.sep}api${path.sep}`) && !file.endsWith(".test.ts"));
const consumersByFunction = new Map();

for (const file of consumerFiles) {
  const source = parseTypeScript(file);
  for (const statement of source.statements) {
    if (!ts.isImportDeclaration(statement) || !ts.isStringLiteral(statement.moduleSpecifier)) continue;
    const modulePath = statement.moduleSpecifier.text;
    if (!/(^|\/)api\//.test(modulePath)) continue;
    const bindings = statement.importClause?.namedBindings;
    if (!bindings || !ts.isNamedImports(bindings)) continue;
    for (const element of bindings.elements) {
      if (statement.importClause?.isTypeOnly || element.isTypeOnly) continue;
      const imported = element.propertyName?.text ?? element.name.text;
      const values = consumersByFunction.get(imported) ?? [];
      values.push(relative(file));
      consumersByFunction.set(imported, values);
    }
  }
}

const routerFile = path.join(repoRoot, "frontend/src/router/routes.ts");
const routerAst = parseTypeScript(routerFile);
const routes = [];
walk(routerAst, (node) => {
  if (!ts.isObjectLiteralExpression(node)) return;
  let routePath = "";
  let routeName = "";
  for (const property of node.properties) {
    if (!ts.isPropertyAssignment(property) || !ts.isIdentifier(property.name)) continue;
    if (property.name.text === "path" && ts.isStringLiteral(property.initializer)) routePath = property.initializer.text;
    if (property.name.text === "name" && ts.isStringLiteral(property.initializer)) routeName = property.initializer.text;
  }
  if (routePath) routes.push({ path: routePath, name: routeName || "redirect" });
});

const openapi = yaml.load(fs.readFileSync(path.join(repoRoot, "docs/api-v1-openapi.yaml"), "utf8"));
const methods = new Set(["get", "post", "put", "patch", "delete", "options", "head"]);
const openapiOperations = [];
for (const [apiPath, definition] of Object.entries(openapi.paths ?? {})) {
  for (const [method, operation] of Object.entries(definition ?? {})) {
    if (!methods.has(method)) continue;
    openapiOperations.push({
      key: `${method.toUpperCase()} ${apiPath}`,
      method: method.toUpperCase(),
      path: apiPath,
      operationId: operation.operationId ?? "",
    });
  }
}

const handlerSource = fs.readFileSync(path.join(repoRoot, "internal/api/handler.go"), "utf8");
const runtimeOperations = [];
for (const match of handlerSource.matchAll(/(?:queries|commands)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]+)"/g)) {
  runtimeOperations.push({
    key: `${match[1]} /api/v1${match[2]}`,
    method: match[1],
    path: `/api/v1${match[2]}`,
  });
}

function pathShape(value) {
  return value
    .replace(/\?.*$/, "")
    .replace(/\{[^}]+\}/g, ":param")
    .replace(/:[A-Za-z0-9_]+/g, ":param");
}

function operationShape(operation) {
  const [method, ...parts] = operation.trim().split(/\s+/);
  return `${method.toUpperCase()} ${pathShape(parts.join(" "))}`;
}

const openapiShapes = new Map(openapiOperations.map((operation) => [operationShape(operation.key), operation]));
const runtimeShapes = new Map(runtimeOperations.map((operation) => [operationShape(operation.key), operation]));
const sourceCorpus = consumerFiles.map((file) => sourceText(file)).join("\n");
const inventory = {
  generated_at: new Date().toISOString(),
  source_head: process.env.SOURCE_HEAD ?? "",
  routes,
  client_functions: clientFunctions.map((entry) => ({
    ...entry,
    consumers: [...new Set(consumersByFunction.get(entry.name) ?? [])].sort(),
  })).sort((left, right) => left.name.localeCompare(right.name)),
  openapi_operations: openapiOperations.sort((left, right) => left.key.localeCompare(right.key)),
  runtime_operations: runtimeOperations.sort((left, right) => left.key.localeCompare(right.key)),
};

const manifestPath = args.get("--manifest");
if (!manifestPath) {
  process.stdout.write(`${JSON.stringify(inventory, null, 2)}\n`);
  process.exit(0);
}

const manifest = JSON.parse(fs.readFileSync(path.resolve(repoRoot, manifestPath), "utf8"));
const errors = [];
const capabilityOperations = new Set();
const capabilityFunctions = new Set();
const capabilityRoutes = new Set();
const functionNames = new Set(clientFunctions.map((entry) => entry.name));
const routePaths = new Set(routes.map((route) => route.path));

for (const capability of manifest.capabilities ?? []) {
  if (!/^[a-z0-9][a-z0-9._-]+$/.test(capability.capability_id ?? "")) {
    errors.push(`invalid capability_id: ${capability.capability_id ?? "<missing>"}`);
  }
  for (const route of capability.routes ?? []) {
    capabilityRoutes.add(route);
    if (!routePaths.has(route)) errors.push(`${capability.capability_id}: unknown route ${route}`);
  }
  for (const functionName of capability.client_functions ?? []) {
    capabilityFunctions.add(functionName);
    if (!functionNames.has(functionName)) errors.push(`${capability.capability_id}: unknown client function ${functionName}`);
    if (!(consumersByFunction.get(functionName)?.length)) errors.push(`${capability.capability_id}: client function has no production consumer ${functionName}`);
  }
  for (const operation of capability.api_operations ?? []) {
    const shape = operationShape(operation);
    capabilityOperations.add(shape);
    if (!openapiShapes.has(shape)) errors.push(`${capability.capability_id}: operation absent from OpenAPI ${operation}`);
    if (!runtimeShapes.has(shape)) errors.push(`${capability.capability_id}: operation absent from runtime ${operation}`);
  }
  if (capability.control && !sourceCorpus.includes(capability.control)) {
    errors.push(`${capability.capability_id}: control text not found in production UI: ${capability.control}`);
  }
}

const exclusions = manifest.exclusions ?? {};
for (const operation of exclusions.api_operations ?? []) capabilityOperations.add(operationShape(operation.operation));
for (const functionEntry of exclusions.client_functions ?? []) capabilityFunctions.add(functionEntry.name);
for (const routeEntry of exclusions.routes ?? []) capabilityRoutes.add(routeEntry.path);

for (const shape of openapiShapes.keys()) {
  if (!runtimeShapes.has(shape)) errors.push(`OpenAPI operation missing at runtime: ${openapiShapes.get(shape).key}`);
  if (!capabilityOperations.has(shape)) errors.push(`OpenAPI operation not classified: ${openapiShapes.get(shape).key}`);
}
for (const shape of runtimeShapes.keys()) {
  if (!openapiShapes.has(shape)) errors.push(`runtime operation missing from OpenAPI: ${runtimeShapes.get(shape).key}`);
}
for (const entry of clientFunctions) {
  if (!capabilityFunctions.has(entry.name)) errors.push(`typed client function not classified: ${entry.name}`);
}
for (const route of routes) {
  if (route.path === "/" || route.path.includes("pathMatch")) continue;
  if (!capabilityRoutes.has(route.path)) errors.push(`public route not classified: ${route.path}`);
}

const result = {
  ...inventory,
  run_id: manifest.run_id,
  capabilities: manifest.capabilities,
  exclusions,
  validation: {
    status: errors.length === 0 ? "PASS" : "FAIL",
    errors,
    counts: {
      routes: routes.length,
      client_functions: clientFunctions.length,
      openapi_operations: openapiOperations.length,
      runtime_operations: runtimeOperations.length,
      capabilities: manifest.capabilities?.length ?? 0,
    },
  },
};

const outputPath = args.get("--output");
if (outputPath) {
  const absoluteOutput = path.resolve(repoRoot, outputPath);
  fs.mkdirSync(path.dirname(absoluteOutput), { recursive: true });
  fs.writeFileSync(absoluteOutput, `${JSON.stringify(result, null, 2)}\n`);
}
process.stdout.write(`${JSON.stringify(result.validation, null, 2)}\n`);
if (errors.length > 0) process.exitCode = 1;
