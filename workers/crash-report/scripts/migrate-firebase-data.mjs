#!/usr/bin/env node

import { createHash, createPrivateKey, createSign } from "node:crypto";
import { chmodSync, readFileSync, renameSync, unlinkSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { parseWranglerRows } from "./apply-diagnostics-v2.mjs";
import {
  assessMigrationCapacity,
  accumulateMigrationCapacityAssessment,
  createMigrationCapacityAssessment,
  finalizeMigrationCapacityAssessment,
} from "./firebase-migration-assessment.mjs";

export { assessMigrationCapacity };

const tokenURL = "https://oauth2.googleapis.com/token";
const scope = "https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/firebase.database";
const PAGE_SIZE = 200;
const MAX_PASSES = 3;
const STORAGE_BUDGET = 700 * 1024 * 1024;
const RESERVATIONS = { active: 640 * 1024, compacted: 128 * 1024, archived: 0 };
// One page can contain six retained 96 KiB reports for each of 200 groups.
// Keep the capture bounded while leaving room for Wrangler's JSON envelope
// and escaping; Node's default child-process buffer is too small for this path.
export const wranglerD1MaxBufferBytes = 192 * 1024 * 1024;
export const firebaseOAuthGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer";

function base64url(value) {
  return Buffer.from(value).toString("base64url");
}

function text(value) {
  return typeof value === "string" ? value : value == null ? "" : String(value);
}

function json(value, fallback) {
  if (typeof value !== "string" || value === "") return fallback;
  try { return JSON.parse(value); } catch { return fallback; }
}

function eventID(fingerprint, id) {
  return createHash("sha256").update(`firebase-migration\n${fingerprint}\n${id}`).digest("hex").slice(0, 32);
}

function sample(row, groupCount, sampleEpoch = 1) {
  return {
    eventId: eventID(text(row.fingerprint), text(row.id)),
    receivedAt: text(row.created_at),
    groupCount,
    writerGeneration: 0,
    sampleEpoch,
    kind: text(row.kind),
    version: text(row.version),
    os: text(row.os),
    arch: text(row.arch),
    message: text(row.message),
    device: json(row.device, {}),
    source: text(row.source),
    label: text(row.label),
    errorType: text(row.error_type),
    errorMessage: text(row.error_message),
    topFrame: text(row.top_frame),
    buildCommit: text(row.build_commit),
    channel: text(row.channel),
    language: text(row.language),
    view: text(row.view),
    breadcrumbs: json(row.breadcrumbs, []),
    componentStack: text(row.component_stack),
    stack: text(row.stack),
    occurredAt: text(row.occurred_at),
    webview2: json(row.webview2, undefined),
    webRuntime: json(row.web_runtime, undefined),
  };
}

export function classifyMigrationGroup(row, now = new Date()) {
  if (row.status !== "resolved" && row.status !== "ignored") return "active";
  const age = now.getTime() - new Date(text(row.last_seen)).getTime();
  if (!Number.isFinite(age) || age < 30 * 86400_000) return "active";
  return age >= 60 * 86400_000 ? "archived" : "compacted";
}

export function canonicalJSONString(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalJSONString).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalJSONString(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

export function contentDigest(value) {
  return createHash("sha256").update(canonicalJSONString(value)).digest("hex");
}

export function buildFirebaseGroups(groupRows, reportRows, now = new Date()) {
  const reports = new Map();
  for (const row of reportRows) {
    const fingerprint = text(row.fingerprint);
    const values = reports.get(fingerprint) ?? [];
    values.push(row);
    reports.set(fingerprint, values);
  }
  const output = new Map();
  for (const row of groupRows) {
    const fingerprint = text(row.fingerprint);
    const count = Number(row.count) || 0;
    const state = classifyMigrationGroup(row, now);
    if (state === "archived") {
      output.set(fingerprint, { state, value: null, firstEventId: "", reservedBytes: 0 });
      continue;
    }
    const retained = (reports.get(fingerprint) ?? []).sort((a, b) => Number(a.id) - Number(b.id));
    const first = retained[0];
    const latestRows = state === "active" ? retained.slice(-5) : [];
    const latest = {};
    latestRows.forEach((report, index) => {
      const sampleCount = count - latestRows.length + index + 1;
      latest[(sampleCount - 1) % 5] = sample(report, sampleCount);
    });
    const samples = {
      ...(first ? { first: sample(first, 1) } : {}),
      ...(latestRows.length ? { latest } : {}),
    };
    const value = {
      meta: {
        fingerprint,
        kind: text(row.kind),
        count,
        firstSeen: text(row.first_seen),
        lastSeen: text(row.last_seen),
        firstVersion: text(row.first_version),
        lastVersion: text(row.last_version),
        status: text(row.status),
        title: text(row.title),
        source: text(row.source),
        label: text(row.label),
        errorType: text(row.error_type),
        topFrame: text(row.top_frame),
        severity: text(row.severity),
        lastOS: text(row.last_os),
        lastArch: text(row.last_arch),
        lastBuildCommit: text(row.last_build_commit),
        lastChannel: text(row.last_channel),
        regressedAt: text(row.regressed_at),
        writerGeneration: 0,
        sampleEpoch: 1,
        sampleState: state,
      },
      ...(Object.keys(samples).length ? { samples } : {}),
    };
    output.set(fingerprint, {
      state,
      value,
      firstEventId: first ? eventID(fingerprint, text(first.id)) : "",
      reservedBytes: RESERVATIONS[state],
    });
  }
  return output;
}

export function runWrangler(projectDir, database, query, spawn = spawnSync) {
  const executable = process.platform === "win32" ? "wrangler.cmd" : "wrangler";
  const wrangler = path.join(projectDir, "node_modules", ".bin", executable);
  const result = spawn(wrangler, ["d1", "execute", database, "--remote", "--json", "--command", query], {
    cwd: projectDir,
    encoding: "utf8",
    env: process.env,
    maxBuffer: wranglerD1MaxBufferBytes,
    stdio: ["ignore", "pipe", "inherit"],
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`wrangler exited with status ${result.status}`);
  return parseWranglerRows(result.stdout ?? "");
}

function sqlText(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

function validateFingerprint(value) {
  if (!/^(?:dev:)?[0-9a-f]{64}$/.test(value)) throw new Error("D1 returned an invalid fingerprint");
  return value;
}

function readPage(projectDir, database, cursor) {
  const rows = runWrangler(
    projectDir,
    database,
    `SELECT * FROM groups WHERE fingerprint > ${sqlText(cursor)} ORDER BY fingerprint LIMIT ${PAGE_SIZE}`,
  );
  if (!rows.length) return { groups: [], reports: [], states: [] };
  const fingerprints = rows.map((row) => validateFingerprint(text(row.fingerprint)));
  const reports = runWrangler(
    projectDir,
    database,
    `SELECT * FROM reports WHERE fingerprint IN (${fingerprints.map(sqlText).join(",")}) ORDER BY fingerprint, id`,
  );
  const states = runWrangler(
    projectDir,
    database,
    `SELECT fingerprint, sample_state, sample_epoch, epoch_first_event_id, reserved_bytes, last_seen
     FROM firebase_crash_group_state WHERE fingerprint IN (${fingerprints.map(sqlText).join(",")})`,
  );
  return { groups: rows, reports, states };
}

async function accessToken(email, privateKey) {
  const now = Math.floor(Date.now() / 1000);
  const header = base64url(JSON.stringify({ alg: "RS256", typ: "JWT" }));
  const claims = base64url(JSON.stringify({ iss: email, scope, aud: tokenURL, iat: now, exp: now + 3600 }));
  const unsigned = `${header}.${claims}`;
  const signer = createSign("RSA-SHA256");
  signer.update(unsigned);
  signer.end();
  const assertion = `${unsigned}.${signer.sign(createPrivateKey(privateKey.replace(/\\n/g, "\n")), "base64url")}`;
  const response = await fetch(tokenURL, {
    method: "POST",
    headers: { "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ grant_type: firebaseOAuthGrantType, assertion }),
    signal: AbortSignal.timeout(10_000),
  });
  if (!response.ok) throw new Error(`Firebase OAuth failed with ${response.status}`);
  const body = await response.json();
  if (typeof body.access_token !== "string") throw new Error("Firebase OAuth response omitted access_token");
  return body.access_token;
}

function databaseURL(raw) {
  const url = new URL(raw);
  if (url.protocol !== "https:" || !(url.hostname.endsWith(".firebaseio.com") || url.hostname.endsWith(".firebasedatabase.app"))) {
    throw new Error("FIREBASE_DATABASE_URL must be an approved Realtime Database host");
  }
  url.search = "";
  url.hash = "";
  return url.toString().replace(/\/$/, "");
}

async function firebaseRequest(baseURL, token, fingerprint, init = {}) {
  const silent = init.method && init.method !== "GET" ? "?print=silent" : "";
  return fetch(`${baseURL}/groups/${encodeURIComponent(fingerprint)}.json${silent}`, {
    ...init,
    headers: { authorization: `Bearer ${token}`, "content-type": "application/json", ...init.headers },
    signal: AbortSignal.timeout(10_000),
  });
}

async function readAndVerify(baseURL, token, fingerprint, expected) {
  const response = await firebaseRequest(baseURL, token, fingerprint, { method: "GET" });
  if (!response.ok) throw new Error(`Firebase migration readback failed with ${response.status}`);
  const actual = await response.json();
  const actualHash = contentDigest(actual);
  const expectedHash = contentDigest(expected);
  if (actualHash !== expectedHash) {
    throw new Error(`Firebase readback digest mismatch for ${fingerprint.slice(0, 8)} (${actualHash.slice(0, 12)})`);
  }
  return actualHash;
}

async function reconcileGroup(baseURL, token, fingerprint, entry, apply) {
  if (apply) {
    const response = await firebaseRequest(baseURL, token, fingerprint, entry.value === null
      ? { method: "DELETE" }
      : { method: "PUT", body: JSON.stringify(entry.value) });
    if (!response.ok) throw new Error(`Firebase migration write failed with ${response.status}`);
  }
  return readAndVerify(baseURL, token, fingerprint, entry.value);
}

function stateSQL(fingerprint, row, entry, now) {
  const compactedAt = entry.state === "compacted" ? now : "";
  const archivedAt = entry.state === "archived" ? now : "";
  const reason = entry.state === "archived" ? "retention" : "";
  return `INSERT INTO firebase_crash_group_state (
    fingerprint, sample_state, sample_epoch, epoch_first_event_id, reserved_bytes,
    last_seen, compacted_at, archived_at, archive_reason
  ) VALUES (
    ${sqlText(fingerprint)}, ${sqlText(entry.state)}, 1, ${sqlText(entry.firstEventId)},
    ${entry.reservedBytes}, ${sqlText(text(row.last_seen))}, ${sqlText(compactedAt)},
    ${sqlText(archivedAt)}, ${sqlText(reason)}
  ) ON CONFLICT (fingerprint) DO UPDATE SET
    sample_state = excluded.sample_state,
    sample_epoch = excluded.sample_epoch,
    epoch_first_event_id = excluded.epoch_first_event_id,
    reserved_bytes = excluded.reserved_bytes,
    last_seen = excluded.last_seen,
    compacted_at = excluded.compacted_at,
    archived_at = excluded.archived_at,
    archive_reason = excluded.archive_reason`;
}

function verifyD1State(row, entry, states) {
  const state = states.find((candidate) => candidate.fingerprint === row.fingerprint);
  if (!state || state.sample_state !== entry.state || Number(state.sample_epoch) !== 1 ||
      text(state.epoch_first_event_id) !== entry.firstEventId ||
      Number(state.reserved_bytes) !== entry.reservedBytes || text(state.last_seen) !== text(row.last_seen)) {
    throw new Error(`D1 migration state mismatch for ${text(row.fingerprint).slice(0, 8)}`);
  }
}

function emptyCheckpoint(database, targetHash, startedAt) {
  return { version: 1, database, targetHash, startedAt, pass: 1, cursor: "", changed: 0, groups: {} };
}

function loadCheckpoint(file, expected) {
  try {
    const value = JSON.parse(readFileSync(file, "utf8"));
    if (value.version !== 1 || value.database !== expected.database || value.targetHash !== expected.targetHash) {
      throw new Error("checkpoint target does not match this migration");
    }
    return value;
  } catch (error) {
    if (error?.code === "ENOENT") return expected;
    throw error;
  }
}

function saveCheckpoint(file, value) {
  const temporary = `${file}.tmp-${process.pid}`;
  writeFileSync(temporary, `${JSON.stringify(value)}\n`, { mode: 0o600 });
  chmodSync(temporary, 0o600);
  renameSync(temporary, file);
  chmodSync(file, 0o600);
}

function parseArgs(argv, projectDir) {
  const apply = argv.includes("--apply");
  const verifyOnly = argv.includes("--verify-only");
  if (apply && verifyOnly) throw new Error("--apply and --verify-only are mutually exclusive");
  const checkpointArg = argv.find((arg) => arg.startsWith("--checkpoint="));
  return {
    mode: apply ? "apply" : verifyOnly ? "verify" : "dry-run",
    reset: argv.includes("--reset-checkpoint"),
    checkpoint: checkpointArg ? path.resolve(checkpointArg.slice("--checkpoint=".length))
      : path.join(projectDir, ".firebase-crash-migration-state.json"),
  };
}

async function dryRun(projectDir, database, now) {
  let cursor = "";
  const counts = { active: 0, compacted: 0, archived: 0 };
  const assessmentCounts = createMigrationCapacityAssessment();
  let estimatedBytes = 0;
  let contentBytes = 0;
  while (true) {
    const page = readPage(projectDir, database, cursor);
    if (!page.groups.length) break;
    accumulateMigrationCapacityAssessment(assessmentCounts, page.groups, now);
    const entries = buildFirebaseGroups(page.groups, page.reports, now);
    for (const entry of entries.values()) {
      counts[entry.state]++;
      estimatedBytes += entry.reservedBytes;
      if (entry.value !== null) contentBytes += Buffer.byteLength(canonicalJSONString(entry.value));
    }
    cursor = text(page.groups.at(-1).fingerprint);
  }
  const assessment = finalizeMigrationCapacityAssessment(assessmentCounts);
  const explainedActive = Object.values(assessment.activeReasons).reduce((sum, value) => sum + value, 0);
  if (explainedActive !== counts.active || assessment.automaticRetention.compacted !== counts.compacted ||
      assessment.automaticRetention.archived !== counts.archived) {
    throw new Error("Firebase capacity assessment disagrees with lifecycle classification");
  }
  const ageLine = (status) => {
    const value = assessment.ageByStatus[status];
    return `${status}=${value.under30d}/${value.days30to59d}/${value.days60plus}/${value.invalid}`;
  };
  const manualSavings = assessment.manualReview.open30to59d * (RESERVATIONS.active - RESERVATIONS.compacted) +
    assessment.manualReview.open60dPlus * RESERVATIONS.active;
  const statusTotals = `open=${assessment.statusTotals.open}, resolved=${assessment.statusTotals.resolved}, ` +
    `ignored=${assessment.statusTotals.ignored}, other=${assessment.statusTotals.other}`;
  const activeReasons = `open=${assessment.activeReasons.open}, other_status=${assessment.activeReasons.otherStatus}, ` +
    `recent_resolved_or_ignored=${assessment.activeReasons.recentResolvedOrIgnored}, ` +
    `invalid_resolved_or_ignored=${assessment.activeReasons.invalidResolvedOrIgnored}`;
  const manualReview = `open_30_to_59d=${assessment.manualReview.open30to59d}, ` +
    `open_60d_plus=${assessment.manualReview.open60dPlus}, ` +
    `other_status_30d_plus=${assessment.manualReview.otherStatus30dPlus}, ` +
    `potential_reduction=${(manualSavings / 1048576).toFixed(1)} MiB`;
  console.log(`Groups: active=${counts.active}, compacted=${counts.compacted}, archived=${counts.archived}.`);
  console.log(`Status totals: ${statusTotals}.`);
  console.log(`Last-seen buckets (<30d/30-59d/>=60d/invalid): ${ageLine("open")}; ${ageLine("resolved")}; ${ageLine("ignored")}; ${ageLine("other")}.`);
  console.log(`Active reasons: ${activeReasons}.`);
  console.log(`Manual review only: ${manualReview}.`);
  console.log(`Canonical Firebase content estimate: ${(contentBytes / 1048576).toFixed(1)} MiB.`);
  const reservationPercent = estimatedBytes / STORAGE_BUDGET * 100;
  console.log(`Conservative reservation: ${(estimatedBytes / 1048576).toFixed(1)} MiB / 700 MiB (${reservationPercent.toFixed(1)}%).`);
  if (reservationPercent >= 80) console.log("Capacity warning: reservation is at or above the 80% review threshold; do not apply without operator review.");
  if (estimatedBytes > STORAGE_BUDGET) throw new Error("Estimated Firebase reservation exceeds the 700 MiB safety budget");
}

async function migrate(projectDir, database, mode, checkpointFile, now) {
  const email = process.env.FIREBASE_CLIENT_EMAIL;
  const privateKey = process.env.FIREBASE_PRIVATE_KEY;
  const rawURL = process.env.FIREBASE_DATABASE_URL;
  if (!email || !privateKey || !rawURL) throw new Error("Firebase service-account environment variables are required");
  const baseURL = databaseURL(rawURL);
  const targetHash = createHash("sha256").update(baseURL).digest("hex");
  let checkpoint = loadCheckpoint(checkpointFile, emptyCheckpoint(database, targetHash, now.toISOString()));
  const token = await accessToken(email, privateKey);
  const apply = mode === "apply";
  for (; checkpoint.pass <= MAX_PASSES; checkpoint.pass++) {
    let cursor = checkpoint.cursor;
    let passChanged = Number(checkpoint.changed ?? 0);
    while (true) {
      const page = readPage(projectDir, database, cursor);
      if (!page.groups.length) break;
      const values = buildFirebaseGroups(page.groups, page.reports, now);
      for (const row of page.groups) {
        const fingerprint = validateFingerprint(text(row.fingerprint));
        const entry = values.get(fingerprint);
        const sourceHash = contentDigest({ state: entry.state, value: entry.value });
        const previous = checkpoint.groups[fingerprint];
        if (mode === "verify" || previous?.sourceHash !== sourceHash) {
          const firebaseHash = await reconcileGroup(baseURL, token, fingerprint, entry, apply);
          if (apply) runWrangler(projectDir, database, stateSQL(fingerprint, row, entry, now.toISOString()));
          if (mode === "verify") verifyD1State(row, entry, page.states);
          checkpoint.groups[fingerprint] = { sourceHash, firebaseHash, state: entry.state };
          passChanged++;
          console.log(`${mode === "verify" ? "Verified" : "Reconciled"} ${fingerprint.slice(0, 8)} ${sourceHash.slice(0, 12)}.`);
        } else if (apply) {
          await readAndVerify(baseURL, token, fingerprint, entry.value);
        }
        cursor = fingerprint;
        checkpoint.cursor = cursor;
        checkpoint.changed = passChanged;
        saveCheckpoint(checkpointFile, checkpoint);
      }
    }
    if (mode === "verify") {
      checkpoint.cursor = "";
      checkpoint.changed = 0;
      checkpoint.completedAt = new Date().toISOString();
      saveCheckpoint(checkpointFile, checkpoint);
      console.log(`Verified ${Object.keys(checkpoint.groups).length} Firebase groups without writes.`);
      return;
    }
    if (passChanged === 0) {
      checkpoint.cursor = "";
      checkpoint.changed = 0;
      checkpoint.completedAt = new Date().toISOString();
      saveCheckpoint(checkpointFile, checkpoint);
      console.log(`Migration converged after ${checkpoint.pass} pass(es).`);
      return;
    }
    checkpoint.cursor = "";
    checkpoint.changed = 0;
    saveCheckpoint(checkpointFile, checkpoint);
  }
  throw new Error("Firebase migration did not converge after 3 reconciliation passes; keep CRASH_STORAGE_MODE=d1");
}

async function main() {
  const projectDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const database = process.env.DIAGNOSTICS_D1_DATABASE || "reasonix-crash";
  const args = parseArgs(process.argv.slice(2), projectDir);
  if (args.reset) {
    try { unlinkSync(args.checkpoint); } catch (error) { if (error?.code !== "ENOENT") throw error; }
  }
  const now = new Date();
  await dryRun(projectDir, database, now);
  if (args.mode === "dry-run") {
    console.log("Dry run only. Use --apply to migrate or --verify-only to perform readback verification.");
    return;
  }
  await migrate(projectDir, database, args.mode, args.checkpoint, now);
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : "";
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
