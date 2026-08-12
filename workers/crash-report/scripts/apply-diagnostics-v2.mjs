#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const columnSets = {
  reports: ["webview2", "web_runtime"],
  pings: [
    "os_build", "os_revision", "channel", "distro_id", "distro_version",
    "kernel_version", "session_type", "runtime_engine", "runtime_version", "gpu_mode",
  ],
  cli_pings: [
    "os_build", "os_revision", "channel", "distro_id", "distro_version",
    "kernel_version", "session_type", "runtime_engine", "runtime_version", "gpu_mode",
  ],
  metric_users: [
    "arch", "os_build", "os_revision", "channel", "distro_id", "distro_version",
    "kernel_version", "session_type", "runtime_engine", "runtime_version", "gpu_mode", "event_count",
  ],
  cli_metric_users: [
    "arch", "os_build", "os_revision", "channel", "distro_id", "distro_version",
    "kernel_version", "session_type", "runtime_engine", "runtime_version", "gpu_mode", "event_count",
  ],
};

const requiredObjects = {
  table: ["report_daily", "report_installations", "report_event_dimensions", "diagnostics_meta"],
  index: [
    "report_installations_fingerprint_date",
    "report_event_dimensions_fingerprint_date",
    "pings_diagnostics_window",
  ],
};

export const diagnosticsV2SchemaEntries = Object.freeze([
  ...Object.entries(columnSets).flatMap(([table, columns]) =>
    columns.map((column) => `column:${table}.${column}`)),
  ...Object.entries(requiredObjects).flatMap(([kind, names]) =>
    names.map((name) => `${kind}:${name}`)),
]);

const tableNames = Object.keys(columnSets).map((name) => `'${name}'`).join(", ");
const objectNames = Object.values(requiredObjects).flat().map((name) => `'${name}'`).join(", ");

export const diagnosticsV2SchemaQuery = `
SELECT 'column' AS kind, m.name || '.' || p.name AS name
FROM sqlite_master AS m, pragma_table_info(m.name) AS p
WHERE m.type = 'table' AND m.name IN (${tableNames})
UNION ALL
SELECT type AS kind, name
FROM sqlite_master
WHERE type IN ('table', 'index') AND name IN (${objectNames})
ORDER BY kind, name;
`.trim();

export function parseWranglerRows(output) {
  const payload = JSON.parse(output);
  const rows = [];
  let sawResults = false;
  const visit = (value) => {
    if (Array.isArray(value)) {
      for (const item of value) visit(item);
      return;
    }
    if (!value || typeof value !== "object") return;
    if (Array.isArray(value.results)) {
      sawResults = true;
      rows.push(...value.results);
    }
    if (Array.isArray(value.result)) visit(value.result);
  };
  visit(payload);
  if (!sawResults) throw new Error("Wrangler returned no D1 result set");
  return rows;
}

export function classifyDiagnosticsV2Schema(rows) {
  const present = new Set(rows.map((row) => `${String(row.kind)}:${String(row.name)}`));
  const missing = diagnosticsV2SchemaEntries.filter((entry) => !present.has(entry));
  if (missing.length === 0) return { state: "complete", missing };
  if (missing.length === diagnosticsV2SchemaEntries.length) return { state: "absent", missing };
  return { state: "partial", missing };
}

function runWrangler(projectDir, args, captureOutput = false) {
  const executable = process.platform === "win32" ? "wrangler.cmd" : "wrangler";
  const wrangler = path.join(projectDir, "node_modules", ".bin", executable);
  const result = spawnSync(wrangler, args, {
    cwd: projectDir,
    encoding: "utf8",
    env: process.env,
    stdio: captureOutput ? ["ignore", "pipe", "inherit"] : "inherit",
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`wrangler exited with status ${result.status}`);
  return result.stdout ?? "";
}

function inspectRemoteSchema(projectDir, database) {
  const output = runWrangler(projectDir, [
    "d1", "execute", database, "--remote", "--json", "--command", diagnosticsV2SchemaQuery,
  ], true);
  return classifyDiagnosticsV2Schema(parseWranglerRows(output));
}

function main() {
  const projectDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const database = process.env.DIAGNOSTICS_D1_DATABASE || "reasonix-crash";
  const before = inspectRemoteSchema(projectDir, database);
  if (before.state === "complete") {
    console.log("Diagnostics v2 D1 schema is already complete; migration skipped.");
    return;
  }
  if (before.state === "partial") {
    throw new Error(
      `Diagnostics v2 D1 schema is partially applied; refusing the all-or-nothing migration. Missing: ${before.missing.join(", ")}`,
    );
  }

  console.log("Recording the current D1 Time Travel bookmark before migration.");
  runWrangler(projectDir, ["d1", "time-travel", "info", database]);
  console.log(`Applying diagnostics v2 migration to ${database}.`);
  runWrangler(projectDir, [
    "d1", "execute", database, "--remote", "--yes", "--file", "migrate-diagnostics-v2.sql",
  ]);
  const after = inspectRemoteSchema(projectDir, database);
  if (after.state !== "complete") {
    throw new Error(`Diagnostics v2 D1 schema verification failed. Missing: ${after.missing.join(", ")}`);
  }
  console.log("Diagnostics v2 D1 schema migration and verification completed.");
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : "";
if (invokedPath === fileURLToPath(import.meta.url)) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
