#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { parseWranglerRows } from "./apply-diagnostics-v2.mjs";

export const firebaseCrashV1SchemaEntries = Object.freeze([
  "table:firebase_crash_outbox",
  "table:firebase_crash_receipts",
  "index:firebase_crash_outbox_retry",
  "index:firebase_crash_receipts_projected",
  "table:firebase_crash_group_leases",
]);

export const firebaseCrashV2SchemaEntries = Object.freeze([
  "table:firebase_crash_group_state",
  "index:firebase_crash_group_state_lifecycle",
  "index:firebase_crash_group_state_lease",
]);

export const firebaseCrashSchemaEntries = Object.freeze([
  ...firebaseCrashV1SchemaEntries,
  ...firebaseCrashV2SchemaEntries,
]);

export const firebaseCrashSchemaQuery = `
SELECT type AS kind, name
FROM sqlite_master
WHERE type IN ('table', 'index') AND name IN (
  'firebase_crash_outbox', 'firebase_crash_receipts', 'firebase_crash_group_leases',
  'firebase_crash_outbox_retry', 'firebase_crash_receipts_projected',
  'firebase_crash_group_state', 'firebase_crash_group_state_lifecycle',
  'firebase_crash_group_state_lease'
)
ORDER BY kind, name;
`.trim();

export const firebaseCrashCapacityQuery = `
SELECT COALESCE(SUM(CASE
  WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-60 days') THEN 0
  WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-30 days') THEN 131072
  ELSE 655360
END), 0) AS reserved_bytes
FROM groups;
`.trim();

function classifyEntries(rows, entries) {
  const present = new Set(rows.map((row) => `${String(row.kind)}:${String(row.name)}`));
  const missing = entries.filter((entry) => !present.has(entry));
  if (missing.length === 0) return { state: "complete", missing };
  if (missing.length === entries.length) return { state: "absent", missing };
  return { state: "partial", missing };
}

export function classifyFirebaseCrashSchema(rows) {
  const v1 = classifyEntries(rows, firebaseCrashV1SchemaEntries);
  const v2 = classifyEntries(rows, firebaseCrashV2SchemaEntries);
  const missing = [...v1.missing, ...v2.missing];
  const state = v1.state === "complete" && v2.state === "complete"
    ? "complete"
    : v1.state === "absent" && v2.state === "absent" ? "absent" : "partial";
  return { state, missing, v1, v2 };
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

function inspect(projectDir, database) {
  const output = runWrangler(projectDir, [
    "d1", "execute", database, "--remote", "--json", "--command", firebaseCrashSchemaQuery,
  ], true);
  return classifyFirebaseCrashSchema(parseWranglerRows(output));
}

function assertSparkCapacity(projectDir, database) {
  const output = runWrangler(projectDir, [
    "d1", "execute", database, "--remote", "--json", "--command", firebaseCrashCapacityQuery,
  ], true);
  const rows = parseWranglerRows(output);
  const reserved = Number(rows[0]?.reserved_bytes ?? 0);
  if (!Number.isFinite(reserved) || reserved > 700 * 1024 * 1024) {
    throw new Error(`Firebase crash reservation preflight exceeds 700 MiB (${reserved} bytes)`);
  }
}

function main() {
  const projectDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const database = process.env.DIAGNOSTICS_D1_DATABASE || "reasonix-crash";
  const before = inspect(projectDir, database);
  if (before.state === "complete") {
    console.log("Firebase crash D1 schema is already complete; migration skipped.");
    return;
  }
  if (before.v1.state === "partial" || before.v2.state === "partial" ||
      (before.v1.state === "absent" && before.v2.state === "complete")) {
    throw new Error(`Firebase crash D1 schema is partial; missing: ${before.missing.join(", ")}`);
  }
  console.log("Recording the current D1 Time Travel bookmark before migration.");
  runWrangler(projectDir, ["d1", "time-travel", "info", database]);
  if (before.v1.state === "absent") {
    runWrangler(projectDir, [
      "d1", "execute", database, "--remote", "--yes", "--file", "migrate-firebase-crash.sql",
    ]);
  }
  if (before.v2.state === "absent") {
    assertSparkCapacity(projectDir, database);
    runWrangler(projectDir, [
      "d1", "execute", database, "--remote", "--yes", "--file", "migrate-firebase-crash-capacity.sql",
    ]);
  }
  const after = inspect(projectDir, database);
  if (after.state !== "complete") {
    throw new Error(`Firebase crash D1 migration failed; missing: ${after.missing.join(", ")}`);
  }
  console.log("Firebase crash D1 migration and verification completed.");
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
