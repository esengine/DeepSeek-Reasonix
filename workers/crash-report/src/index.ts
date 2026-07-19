// Ingest + dashboard for desktop crash/feedback/performance reports and the
// anonymous launch ping. Reports are user-initiated; pings are opt-out
// (desktop.telemetry).
import { z } from "zod";
import type { Env } from "./env";
import { html, redirect } from "./shell";
import { renderGroup, renderStats, type Group, type StatsModule } from "./stats";
import { renderAccount } from "./auth_pages";
import { renderUsers, renderAudit, type UserRow, type AuditRow } from "./admin";
import {
  atLeast,
  currentUser,
  loginUrl,
  logAction,
  sameOrigin,
  sharedLogout,
  type Role,
  type User,
} from "./auth";
import registryApp from "./registry/app";
import type { Bindings as RegistryBindings } from "./registry/env";
import { PackageRepo } from "./registry/db/packages";
import { EventRepo } from "./registry/db/events";
import { renderCommunity } from "./community";
import { desktopReleaseChannel, handleDesktopReleaseManifest } from "./desktop_release";

const MAX_BODY_BYTES = 96 * 1024;
const LATEST_SAMPLES_PER_GROUP = 5;
const DEVELOPMENT_FINGERPRINT_PREFIX = "dev:";
const GROUP_PATH_RE = /^\/stats\/group\/((?:dev:)?[0-9a-f]{64})$/;

const Device = z
  .object({
    osVersion: z.string().max(128),
    cpu: z.string().max(128),
    cores: z.number().int().min(0).max(4096),
    ramGb: z.number().min(0).max(65536),
  })
  .partial();

const Report = z.object({
  kind: z.enum(["crash", "exception", "feedback", "performance", "bot"]),
  version: z.string().min(1).max(64),
  os: z.string().min(1).max(32),
  arch: z.string().min(1).max(32),
  message: z.string().min(1).max(16 * 1024),
  device: Device.optional(),
  schemaVersion: z.number().int().min(1).max(10).optional(),
  source: z.string().trim().min(1).max(32).regex(/^[a-z0-9_.-]+$/).optional(),
  label: z.string().max(64).optional(),
  errorType: z.string().max(128).optional(),
  errorMessage: z.string().max(4 * 1024).optional(),
  stack: z.string().max(16 * 1024).optional(),
  componentStack: z.string().max(16 * 1024).optional(),
  topFrame: z.string().max(300).optional(),
  fingerprintHint: z.string().max(300).optional(),
  buildCommit: z.string().max(64).optional(),
  channel: z.string().max(32).optional(),
  language: z.string().max(64).optional(),
  view: z.string().max(200).optional(),
  breadcrumbs: z
    .array(
      z.object({
        t: z.number().int().optional(),
        cat: z.string().max(64).optional(),
        msg: z.string().max(240).optional(),
      }),
    )
    .max(30)
    .optional(),
  occurredAt: z.string().max(64).optional(),
});
type ReportPayload = z.infer<typeof Report>;

const Ping = z.object({
  installId: z.string().regex(/^[0-9a-f]{32}$/),
  version: z.string().min(1).max(64),
  os: z.string().min(1).max(32),
  arch: z.string().min(1).max(32),
  osVersion: z.string().max(128).optional(),
});

// Opt-in aggregate desktop metrics: a per-launch snapshot of (signal, bucket)
// counters. No install id, no content — just enumerated signals and bounded
// buckets so the worker table can never be polluted with arbitrary keys.
const METRIC_SIGNALS = [
  "finish_reason",
  "empty_final",
  "provider_error",
  "cache_hit",
  "tool_error",
  "updater_error",
  "compaction",
  "turns",
  "desktop_hang",
  "desktop_hang_age",
  "client_surface",
  "client_version",
  "settings_language",
  "settings_desktop_layout",
  "settings_theme",
  "settings_theme_style",
  "settings_close_behavior",
  "settings_display_mode",
  "settings_auto_plan",
  "settings_status_bar_style",
  "settings_status_bar_items_count",
  "settings_check_updates",
  "settings_default_model",
  "settings_planner_model",
  "settings_subagent_model",
  "settings_subagent_effort",
  "settings_reasoning_language",
  "settings_provider_count",
  "settings_provider_access_count",
  "settings_provider_access",
  "settings_bot_enabled",
  "settings_bot_model",
  "settings_bot_tool_approval",
  "settings_bot_allowlist",
  "settings_bot_allow_all",
  "settings_bot_qq_enabled",
  "settings_bot_feishu_enabled",
  "settings_bot_weixin_enabled",
  "settings_bot_connection_count",
  "settings_bot_connection_provider",
  "settings_bot_connection_enabled",
  "settings_bot_connection_status",
  "settings_bot_connection_model",
  "settings_bot_connection_approval",
] as const;

const Metrics = z.object({
  installId: z
    .string()
    .regex(/^[0-9a-f]{32}$/)
    .optional(),
  version: z.string().min(1).max(64),
  os: z.string().min(1).max(32),
  counters: z
    .array(
      z.object({
        signal: z.enum(METRIC_SIGNALS),
        bucket: z
          .string()
          .min(1)
          .max(96)
          .regex(/^[a-z0-9_]+$/),
        count: z.number().int().min(1).max(1_000_000),
      }),
    )
    .min(1)
    .max(128),
});

type FingerprintInput = {
  kind: string;
  message: string;
  source?: string;
  label?: string;
  errorType?: string;
  errorMessage?: string;
  topFrame?: string;
  fingerprintHint?: string;
};

export function scrubSensitiveText(input: string): string {
  return input
    .replace(/([A-Z]:\\Users\\)[^/\\:\s"']+/gi, "$1_")
    .replace(/(\/(?:home|Users)\/)[^/\\:\s"']+/g, "$1_")
    .replace(/\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b/g, "[redacted-email]")
    .replace(/\bBearer\s+[A-Za-z0-9._~+/=-]{16,}/gi, "Bearer [redacted]")
    .replace(
      /\b(api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|authorization|secret|password|passwd|pwd|token)\b\s*[:=]\s*(?:Bearer\s+)?['"]?[^'"\s,;]+['"]?/gi,
      "$1=[redacted]",
    )
    .replace(/\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b/g, "[redacted-jwt]")
    .replace(/\b(?:sk|rk)-(?:proj-)?[A-Za-z0-9_-]{16,}\b/g, "[redacted-key]")
    .replace(/\b[0-9a-fA-F]{32,}\b/g, "[redacted-hex]")
    .replace(/[A-Za-z0-9+/]{40,}={0,2}/g, "[redacted-token]")
    .replace(/\b[A-Za-z0-9_-]{48,}\b/g, "[redacted-token]");
}

function normalizeStackFrame(frame: string): string {
  return frame
    .replace(/[A-Za-z]:\\[^\s)('"]+/g, "<path>")
    .replace(/\/(?:home|Users)\/[^\s)('"]+/g, "/<home>")
    .replace(/(?:wails|https?|file):\/\/[^\s)('"]+/g, "<url>")
    .replace(/0x[0-9a-fA-F]+/g, "<addr>")
    .replace(/:\d+(?::\d+)?/g, ":<n>");
}

function normalizeFingerprintText(text: string): string {
  return text
    .replace(/[A-Za-z]:\\[^\s)('"]+/g, "<path>")
    .replace(/(?:wails|https?|file):\/\/[^\s)('"]+/g, "<url>")
    .replace(/0x[0-9a-fA-F]+/g, "<addr>")
    .replace(/^build [0-9a-f]+$/gm, "build <commit>")
    .replace(/:\d+(?::\d+)?/g, ":<n>");
}

export function normalizeForFingerprint(inputOrKind: FingerprintInput | string, legacyMessage = ""): string {
  if (typeof inputOrKind === "string") {
    const head = legacyMessage.split("\n").slice(0, 12).join("\n");
    return inputOrKind + "\n" + normalizeFingerprintText(head);
  }
  const input = inputOrKind;
  const messageBasis = input.errorMessage || input.message;
  const head = messageBasis.split("\n").slice(0, 6).join("\n");
  return (
    input.kind +
    "\n" +
    (input.source || "legacy") +
    "\n" +
    (input.label || "") +
    "\n" +
    (input.errorType || "") +
    "\n" +
    normalizeStackFrame(input.topFrame || "") +
    "\n" +
    (input.fingerprintHint ? `${input.fingerprintHint}\n` : "") +
    normalizeFingerprintText(head)
  );
}

function hasStructuredCrashFields(r: ReportPayload): boolean {
  return Boolean(
    r.schemaVersion ||
      r.source ||
      r.label ||
      r.errorType ||
      r.errorMessage ||
      r.stack ||
      r.componentStack ||
      r.topFrame ||
      r.fingerprintHint ||
      r.buildCommit ||
      r.channel ||
      r.language ||
      r.view ||
      r.breadcrumbs?.length ||
      r.occurredAt,
  );
}

// One-line human summary for the dashboard list. Frontend reports are formatted
// "[label]\n\n<detail>", so a bare label alone is folded together with its detail.
export function crashTitle(message: string): string {
  const lines = message
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);
  let head = lines[0] ?? "";
  if (/^\[[^\]]+\]$/.test(head) && lines[1]) head = `${head} ${lines[1]}`;
  return head.slice(0, 200);
}

type SeverityInput = {
  kind: string;
  version?: string;
  source: string;
  label: string;
  errorType: string;
  errorMessage: string;
  topFrame: string;
  channel?: string;
};

const RESIZE_OBSERVER_NOTICE_RE = /^ResizeObserver loop (?:limit exceeded|completed with undelivered notifications\.?)$/;

export function isDevelopmentReport(input: SeverityInput): boolean {
  return input.channel?.trim().toLowerCase() === "dev" || input.version?.trim().toLowerCase().startsWith("dev") === true;
}

export function namespaceReportFingerprint(hash: string, development: boolean): string {
  return development ? `${DEVELOPMENT_FINGERPRINT_PREFIX}${hash}` : hash;
}

export function groupFingerprintFromPath(path: string): string | null {
  return path.match(GROUP_PATH_RE)?.[1] ?? null;
}

export function isKnownNonCrashDiagnostic(input: SeverityInput): boolean {
  const message = input.errorMessage.trim();
  return (
    RESIZE_OBSERVER_NOTICE_RE.test(message) ||
    /Minified React error #520\b/.test(message) ||
    message.includes("additional File object is not a file on the disk")
  );
}

export function isOpaqueScriptErrorReport(input: SeverityInput): boolean {
  return (
    input.kind === "crash" &&
    input.source === "frontend.global" &&
    input.label === "window.error" &&
    input.errorType === "string" &&
    input.errorMessage.trim() === "Script error." &&
    input.topFrame.trim() === ""
  );
}

function severityForKind(kind: string): string {
  if (kind === "crash") return "high";
  if (kind === "performance") return "medium";
  if (kind === "bot") return "medium";
  if (kind === "exception") return "medium";
  return "low";
}

export function severityForReport(input: SeverityInput): string {
  if (isDevelopmentReport(input) || isOpaqueScriptErrorReport(input) || isKnownNonCrashDiagnostic(input)) return "low";
  return severityForKind(input.kind);
}

async function sha256Hex(s: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function readJSON(request: Request): Promise<unknown | Response> {
  const length = Number(request.headers.get("content-length") ?? "0");
  if (!length || length > MAX_BODY_BYTES) return new Response("payload too large", { status: 413 });
  try {
    return JSON.parse(await request.text());
  } catch {
    return new Response("bad request", { status: 400 });
  }
}

// Ingest writes fail together when the database is unhealthy (e.g. the D1
// size cap: every INSERT throws while reads stay fine). Surface that as a
// deliberate 503 with a loud log instead of an opaque worker exception, so
// `wrangler tail` / observability show the root cause and clients see a
// retryable status.
function storageUnavailable(op: string, err: unknown): Response {
  console.error(`${op}: D1 write failed`, err);
  return new Response("storage unavailable", { status: 503 });
}

async function handleReport(request: Request, env: Env): Promise<Response> {
  const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
  const { success } = await env.RATE_LIMITER.limit({ key: ip });
  if (!success) return new Response("rate limited", { status: 429 });

  const raw = await readJSON(request);
  if (raw instanceof Response) return raw;
  const parsed = Report.safeParse(raw);
  if (!parsed.success) return new Response("bad request", { status: 400 });
  const r = parsed.data;
  const message = scrubSensitiveText(r.message);
  const errorMessage = scrubSensitiveText(r.errorMessage ?? "");
  const stack = scrubSensitiveText(r.stack ?? "");
  const componentStack = scrubSensitiveText(r.componentStack ?? "");
  const topFrame = scrubSensitiveText(r.topFrame ?? "");
  const fingerprintHint = scrubSensitiveText(r.fingerprintHint ?? "");
  const view = scrubSensitiveText(r.view ?? "");
  const breadcrumbs = (r.breadcrumbs ?? []).map((b) => ({
    ...b,
    msg: b.msg ? scrubSensitiveText(b.msg) : b.msg,
  }));

  const fingerprintBasis = hasStructuredCrashFields(r)
    ? normalizeForFingerprint({
        kind: r.kind,
        message,
        source: r.source,
        label: r.label,
        errorType: r.errorType,
        errorMessage,
        topFrame,
        fingerprintHint,
      })
    : normalizeForFingerprint(r.kind, message);
  const now = new Date().toISOString();
  const title = crashTitle(message);
  const source = r.source ?? "legacy";
  const label = r.label ?? "";
  const errorType = r.errorType ?? "";
  const buildCommit = r.buildCommit ?? "";
  const channel = r.channel ?? "";
  const severityInput = {
    kind: r.kind,
    version: r.version,
    source,
    label,
    errorType,
    errorMessage,
    topFrame,
    channel,
  };
  const development = isDevelopmentReport(severityInput);
  const fingerprint = namespaceReportFingerprint(await sha256Hex(fingerprintBasis), development);
  const severity = severityForReport(severityInput);
  try {
    const prior = await env.DB.prepare("SELECT status FROM groups WHERE fingerprint = ?1")
      .bind(fingerprint)
      .first<{ status: string }>();
    const regressedAt = prior?.status === "resolved" ? now : "";

    await env.DB.prepare(
      `INSERT INTO groups (
         fingerprint, kind, count, first_seen, last_seen, first_version, last_version,
         status, title, source, label, error_type, top_frame, severity,
         last_os, last_arch, last_build_commit, last_channel, last_sample_at, regressed_at
       )
       VALUES (?1, ?2, 1, ?3, ?3, ?4, ?4, 'open', ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?3, ?15)
       ON CONFLICT (fingerprint) DO UPDATE SET
         count = count + 1,
         last_seen = ?3,
         last_version = ?4,
         title = ?5,
         source = ?6,
         label = ?7,
         error_type = ?8,
         top_frame = ?9,
         severity = CASE WHEN severity = 'critical' THEN severity WHEN ?10 = 'low' THEN 'low' ELSE severity END,
         last_os = ?11,
         last_arch = ?12,
         last_build_commit = ?13,
         last_channel = ?14,
         last_sample_at = ?3,
         status = CASE WHEN status = 'resolved' THEN 'open' ELSE status END,
         regressed_at = CASE WHEN status = 'resolved' THEN ?3 ELSE regressed_at END`,
    )
      .bind(fingerprint, r.kind, now, r.version, title, source, label, errorType, topFrame, severity, r.os, r.arch, buildCommit, channel, regressedAt)
      .run();

    await env.DB.prepare(
      `INSERT INTO reports (
         fingerprint, kind, version, os, arch, message, device, created_at,
         source, label, error_type, error_message, top_frame, build_commit, channel,
         language, view, breadcrumbs, component_stack, stack, occurred_at
       )
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18, ?19, ?20, ?21)`,
    )
      .bind(
        fingerprint,
        r.kind,
        r.version,
        r.os,
        r.arch,
        message,
        JSON.stringify(r.device ?? {}),
        now,
        source,
        label,
        errorType,
        errorMessage,
        topFrame,
        buildCommit,
        channel,
        r.language ?? "",
        view,
        JSON.stringify(breadcrumbs),
        componentStack,
        stack,
        r.occurredAt ?? "",
      )
      .run();

    await env.DB.prepare(
      `DELETE FROM reports
       WHERE fingerprint = ?1
         AND id NOT IN (
           SELECT id FROM (SELECT id FROM reports WHERE fingerprint = ?1 ORDER BY id ASC LIMIT 1)
           UNION
           SELECT id FROM (SELECT id FROM reports WHERE fingerprint = ?1 ORDER BY id DESC LIMIT ?2)
         )`,
    )
      .bind(fingerprint, LATEST_SAMPLES_PER_GROUP)
      .run();
  } catch (err) {
    return storageUnavailable("report", err);
  }

  return new Response("ok", { status: 202 });
}

async function handlePing(request: Request, env: Env): Promise<Response> {
  const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
  const { success } = await env.PING_LIMITER.limit({ key: ip });
  if (!success) return new Response("rate limited", { status: 429 });

  const raw = await readJSON(request);
  if (raw instanceof Response) return raw;
  const parsed = Ping.safeParse(raw);
  if (!parsed.success) return new Response("bad request", { status: 400 });
  const p = parsed.data;

  try {
    await env.DB.prepare(
      `INSERT INTO pings (date, install_id, version, os, arch, os_version, opens)
       VALUES (date('now'), ?1, ?2, ?3, ?4, ?5, 1)
       ON CONFLICT (date, install_id) DO UPDATE SET
         opens = opens + 1, version = ?2, os_version = ?5`,
    )
      .bind(p.installId, p.version, p.os, p.arch, p.osVersion ?? "")
      .run();
  } catch (err) {
    return storageUnavailable("ping", err);
  }

  return new Response("ok", { status: 202 });
}

async function handleMetrics(request: Request, env: Env): Promise<Response> {
  const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
  const { success } = await env.METRICS_LIMITER.limit({ key: ip });
  if (!success) return new Response("rate limited", { status: 429 });

  const raw = await readJSON(request);
  if (raw instanceof Response) return raw;
  const parsed = Metrics.safeParse(raw);
  if (!parsed.success) return new Response("bad request", { status: 400 });
  const m = parsed.data;

  const upsert = env.DB.prepare(
    `INSERT INTO metrics (date, version, os, signal, bucket, count)
     VALUES (date('now'), ?1, ?2, ?3, ?4, ?5)
     ON CONFLICT (date, version, os, signal, bucket) DO UPDATE SET
       count = count + ?5`,
  );
  try {
    await env.DB.batch(m.counters.map((c) => upsert.bind(m.version, m.os, c.signal, c.bucket, c.count)));
  } catch (err) {
    return storageUnavailable("metrics", err);
  }
  if (m.installId) {
    const userUpsert = env.DB.prepare(
      `INSERT INTO metric_users (date, version, os, signal, bucket, install_id)
       VALUES (date('now'), ?1, ?2, ?3, ?4, ?5)
       ON CONFLICT (date, signal, bucket, install_id) DO UPDATE SET
         version = ?1, os = ?2`,
    );
    try {
      await env.DB.batch(m.counters.map((c) => userUpsert.bind(m.version, m.os, c.signal, c.bucket, m.installId)));
    } catch (err) {
      console.warn("metric_users write failed", err);
    }
  }

  return new Response("ok", { status: 202 });
}

const UserAction = z.object({
  action: z.enum(["role", "delete"]),
  userId: z.coerce.number().int().positive(),
  role: z.enum(["pending", "viewer", "admin"]).optional(),
});

const GroupAction = z.object({
  action: z.enum(["status", "delete", "note", "resolution", "severity"]),
  status: z.enum(["open", "resolved", "ignored"]).optional(),
  note: z.string().max(500).optional(),
  resolvedIn: z.string().max(64).optional(),
  severity: z.enum(["low", "medium", "high", "critical"]).optional(),
});

async function formObject(request: Request): Promise<Record<string, string>> {
  const form = await request.formData();
  const out: Record<string, string> = {};
  for (const [k, v] of form) out[k] = typeof v === "string" ? v : "";
  return out;
}

type StatsFilters = {
  status: string;
  source: string;
  version: string;
  os: string;
  platform: string;
  newLatest: boolean;
  regressed: boolean;
  windowDays: 7 | 30;
  preferenceMode: "users" | "opens";
};

function statsFilters(url: URL): StatsFilters {
  const status = url.searchParams.get("status") ?? "";
  const windowParam = url.searchParams.get("window") ?? "";
  return {
    status: ["open", "resolved", "ignored"].includes(status) ? status : "",
    source: (url.searchParams.get("source") ?? "").slice(0, 32),
    version: (url.searchParams.get("version") ?? "").slice(0, 64),
    os: (url.searchParams.get("os") ?? "").slice(0, 32),
    platform: (url.searchParams.get("platform") ?? "").slice(0, 80),
    newLatest: url.searchParams.get("new") === "latest",
    regressed: url.searchParams.get("regressed") === "1",
    windowDays: windowParam === "7d" ? 7 : 30,
    preferenceMode: url.searchParams.get("prefs") === "opens" ? "opens" : "users",
  };
}

async function crashGroups(env: Env, filters: StatsFilters, latestVersion: string) {
  const where: string[] = [];
  const binds: unknown[] = [];
  const add = (sql: string, value?: unknown) => {
    where.push(sql.replace("?", `?${binds.length + 1}`));
    if (value !== undefined) binds.push(value);
  };
  if (filters.status) add("status = ?", filters.status);
  if (filters.source) add("source = ?", filters.source);
  if (filters.version) add("last_version = ?", filters.version);
  if (filters.os) add("last_os = ?", filters.os);
  if (filters.platform) add("last_os || ' ' || last_arch = ?", filters.platform);
  if (filters.newLatest && latestVersion) add("first_version = ?", latestVersion);
  if (filters.regressed) where.push("regressed_at <> ''");
  let latestOrder = "";
  if (latestVersion) {
    latestOrder = `CASE WHEN first_version = ?${binds.length + 1} THEN 0 ELSE 1 END,`;
    binds.push(latestVersion);
  }
  const sql = `SELECT fingerprint, kind, count, first_version, last_version, substr(last_seen, 1, 10) AS seen,
      status, title, source, label, error_type, top_frame, severity, last_os, last_arch, last_channel, regressed_at
    FROM groups ${where.length ? `WHERE ${where.join(" AND ")}` : ""}
    ORDER BY
      CASE WHEN status = 'open' THEN 0 ELSE 1 END,
      CASE
        WHEN severity = 'critical' THEN 0
        WHEN ${developmentGroupSQL}
          OR title = '[window.error] Script error.'
          OR title LIKE '%ResizeObserver loop %'
          OR title LIKE '%Minified React error #520%'
          OR title LIKE '%additional File object is not a file on the disk%'
          THEN 3
        WHEN severity = 'high' THEN 1
        WHEN severity = 'medium' THEN 2
        ELSE 3
      END,
      ${latestOrder}
      CASE WHEN regressed_at <> '' THEN 0 ELSE 1 END,
      count DESC,
      last_seen DESC
    LIMIT 50`;
  const stmt = env.DB.prepare(sql);
  const query = binds.length ? stmt.bind(...binds) : stmt;
  const result = await query.all<{
    fingerprint: string;
    kind: string;
    count: number;
    first_version: string;
    last_version: string;
    seen: string;
    status: string;
    title: string;
    source: string;
    label: string;
    error_type: string;
    top_frame: string;
    severity: string;
    last_os: string;
    last_arch: string;
    last_channel: string;
    regressed_at: string;
  }>();
  result.results = result.results
    .map((row) => ({ ...row, severity: effectiveGroupSeverity(row), development: isDevelopmentGroup(row) }))
    .sort((a, b) => compareDiagnosticPriority(a, b, latestVersion));
  return result;
}

type GroupPriorityRow = {
  fingerprint: string;
  status: string;
  severity: string;
  regressed_at: string;
  first_version: string;
  count: number;
  seen: string;
  title: string;
  last_version: string;
  last_channel: string;
};

const developmentGroupSQL = `fingerprint LIKE 'dev:%'`;

export function isDevelopmentGroup(row: Pick<GroupPriorityRow, "fingerprint">): boolean {
  // Legacy hashes can contain release observations between retained development
  // samples, so only the explicit namespace proves that a group is dev-only.
  return row.fingerprint.startsWith(DEVELOPMENT_FINGERPRINT_PREFIX);
}

export function effectiveGroupSeverity(
  row: Pick<GroupPriorityRow, "fingerprint" | "severity" | "title">,
): string {
  if (row.severity === "critical") return row.severity;
  if (isDevelopmentGroup(row)) return "low";
  if (
    row.title === "[window.error] Script error." ||
    row.title.includes("ResizeObserver loop ") ||
    row.title.includes("Minified React error #520") ||
    row.title.includes("additional File object is not a file on the disk")
  ) {
    return "low";
  }
  return row.severity;
}

function compareDiagnosticPriority(a: GroupPriorityRow, b: GroupPriorityRow, latestVersion: string): number {
  const statusRank = (value: string) => (value === "open" ? 0 : 1);
  const severityRank = (value: string) => ({ critical: 0, high: 1, medium: 2, low: 3 })[value] ?? 4;
  return (
    statusRank(a.status) - statusRank(b.status) ||
    severityRank(a.severity) - severityRank(b.severity) ||
    Number(b.first_version === latestVersion) - Number(a.first_version === latestVersion) ||
    Number(Boolean(b.regressed_at)) - Number(Boolean(a.regressed_at)) ||
    b.count - a.count ||
    b.seen.localeCompare(a.seen)
  );
}

type ParsedVersion = {
  version: string;
  major: number;
  minor: number;
  patch: number;
};

function parseReleaseVersion(version: string): ParsedVersion | null {
  const m = version.trim().match(/^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$/);
  if (!m) return null;
  return {
    version,
    major: Number(m[1]),
    minor: Number(m[2]),
    patch: Number(m[3]),
  };
}

function newestReleaseVersion(versions: string[]): string {
  const parsed = versions
    .filter((v) => v && v.toLowerCase() !== "dev")
    .map(parseReleaseVersion)
    .filter((v): v is ParsedVersion => v !== null);
  parsed.sort(
    (a, b) =>
      b.major - a.major ||
      b.minor - a.minor ||
      b.patch - a.patch ||
      b.version.localeCompare(a.version),
  );
  return parsed[0]?.version ?? "";
}

async function latestObservedVersion(env: Env): Promise<string> {
  const rows = await env.DB.prepare(
    `SELECT version FROM (
       SELECT version FROM pings WHERE date >= date('now', '-29 day')
       UNION
       SELECT last_version AS version FROM groups
     ) AS versions WHERE version <> ''`,
  ).all<{ version: string }>();
  return newestReleaseVersion(rows.results.map((r) => r.version));
}

type OverviewCounts = {
  latestAdoptionPct: number | null;
  openReports: number;
  newLatestReports: number;
  regressedReports: number;
  criticalOpenReports: number;
};

async function latestAdoptionPct(env: Env, latestVersion: string, days: 7 | 30): Promise<number | null> {
  if (!latestVersion) return null;
  const row = await env.DB.prepare(
    `SELECT
      COUNT(DISTINCT install_id) AS total_installs,
      COUNT(DISTINCT CASE WHEN version = ?1 THEN install_id END) AS latest_installs
    FROM pings WHERE date >= date('now', '${currentWindowSince(days)}')`,
  )
    .bind(latestVersion)
    .first<{ total_installs: number; latest_installs: number }>();
  const total = Number(row?.total_installs ?? 0);
  if (!total) return null;
  return (Number(row?.latest_installs ?? 0) / total) * 100;
}

async function diagnosticOverview(env: Env, latestVersion: string, days: 7 | 30): Promise<OverviewCounts> {
  // Keep the overview's red state aligned with the effective severity used by
  // the diagnostics list. Historical rows retain their stored severity, so
  // known browser notices and development builds must be discounted here too.
  const criticalActionable = `(severity = 'critical' OR (
    severity = 'high'
    AND kind <> 'performance'
    AND NOT ${developmentGroupSQL}
    AND title <> '[window.error] Script error.'
    AND title NOT LIKE '%ResizeObserver loop %'
    AND title NOT LIKE '%Minified React error #520%'
    AND title NOT LIKE '%additional File object is not a file on the disk%'
  ))`;
  const diagnosticCounts = latestVersion
    ? env.DB.prepare(
        `SELECT
          SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END) AS open_reports,
          SUM(CASE WHEN first_version = ?1 THEN 1 ELSE 0 END) AS new_latest_reports,
          SUM(CASE WHEN regressed_at <> '' THEN 1 ELSE 0 END) AS regressed_reports,
          SUM(CASE WHEN status = 'open' AND ${criticalActionable} THEN 1 ELSE 0 END) AS critical_open_reports
        FROM groups`,
      )
        .bind(latestVersion)
        .first<{ open_reports: number; new_latest_reports: number; regressed_reports: number; critical_open_reports: number }>()
    : env.DB.prepare(
        `SELECT
          SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END) AS open_reports,
          0 AS new_latest_reports,
          SUM(CASE WHEN regressed_at <> '' THEN 1 ELSE 0 END) AS regressed_reports,
          SUM(CASE WHEN status = 'open' AND ${criticalActionable} THEN 1 ELSE 0 END) AS critical_open_reports
        FROM groups`,
      ).first<{ open_reports: number; new_latest_reports: number; regressed_reports: number; critical_open_reports: number }>();
  const [row, adoptionPct] = await Promise.all([
    diagnosticCounts,
    latestAdoptionPct(env, latestVersion, days),
  ]);
  return {
    latestAdoptionPct: adoptionPct,
    openReports: Number(row?.open_reports ?? 0),
    newLatestReports: Number(row?.new_latest_reports ?? 0),
    regressedReports: Number(row?.regressed_reports ?? 0),
    criticalOpenReports: Number(row?.critical_open_reports ?? 0),
  };
}

function currentWindowSince(days: 7 | 30): string {
  return `-${days - 1} day`;
}

function previousWindowSince(days: 7 | 30): string {
  return `-${days * 2 - 1} day`;
}

function previousWindowUntil(days: 7 | 30): string {
  return currentWindowSince(days);
}

async function metricRows(env: Env, days: 7 | 30, previous = false): Promise<{ signal: string; bucket: string; total: number }[]> {
  const where = previous
    ? `date >= date('now', '${previousWindowSince(days)}') AND date < date('now', '${previousWindowUntil(days)}')`
    : `date >= date('now', '${currentWindowSince(days)}')`;
  const rows = await env.DB.prepare(
    `SELECT signal, bucket, SUM(count) AS total FROM metrics WHERE ${where} GROUP BY signal, bucket ORDER BY signal, total DESC`,
  ).all<{ signal: string; bucket: string; total: number }>();
  return rows.results;
}

async function metricUserRows(env: Env, days: 7 | 30): Promise<{ signal: string; bucket: string; total: number }[]> {
  try {
    const rows = await env.DB.prepare(
      `SELECT signal, bucket, COUNT(DISTINCT install_id) AS total FROM metric_users WHERE date >= date('now', '${currentWindowSince(days)}') GROUP BY signal, bucket ORDER BY signal, total DESC`,
    ).all<{ signal: string; bucket: string; total: number }>();
    return rows.results;
  } catch (err) {
    console.warn("metric_users query failed", err);
    return [];
  }
}

type Bar = { label: string; users: number };
type MetricTotals = { signal: string; bucket: string; total: number }[];

// Each stats module renders only its own section, so a page load should query
// only what that section shows — the 30-day COUNT(DISTINCT) over metric_users,
// the heaviest query, is read solely by the preferences module.
async function handleStats(request: Request, env: Env, user: User, activeModule: StatsModule): Promise<Response> {
  const url = new URL(request.url);
  const filters = statsFilters(url);
  const days = filters.windowDays;
  const since = currentWindowSince(days);
  const bars = (sql: string) => env.DB.prepare(sql).all<Bar>().then((r) => r.results);
  const pingVersions = () =>
    bars(`SELECT version AS label, COUNT(DISTINCT install_id) AS users FROM pings WHERE date >= date('now', '${since}') GROUP BY label ORDER BY users DESC LIMIT 15`);
  const pingPlatforms = () =>
    bars(`SELECT os || ' ' || arch AS label, COUNT(DISTINCT install_id) AS users FROM pings WHERE date >= date('now', '${since}') GROUP BY label ORDER BY users DESC`);

  let daily: { date: string; users: number; opens: number }[] = [];
  let versions: Bar[] = [];
  let platforms: Bar[] = [];
  let crashes: Awaited<ReturnType<typeof crashGroups>>["results"] = [];
  let metrics: MetricTotals = [];
  let previousMetrics: MetricTotals = [];
  let metricUsers: MetricTotals = [];
  let sources: Bar[] = [];
  let overview: OverviewCounts = {
    latestAdoptionPct: null,
    openReports: 0,
    newLatestReports: 0,
    regressedReports: 0,
    criticalOpenReports: 0,
  };
  let latestVersion = "";

  if (activeModule === "usage") {
    latestVersion = await latestObservedVersion(env);
    const [dailyR, versionsR, platformsR, metricsR, overviewR] = await Promise.all([
      env.DB.prepare(
        `SELECT date, COUNT(*) AS users, SUM(opens) AS opens FROM pings WHERE date >= date('now', '${since}') GROUP BY date`,
      ).all<{ date: string; users: number; opens: number }>(),
      pingVersions(),
      pingPlatforms(),
      metricRows(env, days),
      diagnosticOverview(env, latestVersion, days),
    ]);
    daily = dailyR.results;
    versions = versionsR;
    platforms = platformsR;
    metrics = metricsR;
    overview = overviewR;
  } else if (activeModule === "diagnostics") {
    latestVersion = await latestObservedVersion(env);
    const [crashesR, sourcesR, versionsR, platformsR] = await Promise.all([
      crashGroups(env, filters, latestVersion),
      bars("SELECT source AS label, COUNT(*) AS users FROM groups GROUP BY source ORDER BY users DESC"),
      pingVersions(),
      pingPlatforms(),
    ]);
    crashes = crashesR.results;
    sources = sourcesR;
    versions = versionsR;
    platforms = platformsR;
  } else if (activeModule === "preferences") {
    [metrics, metricUsers] = await Promise.all([metricRows(env, days), metricUserRows(env, days)]);
  } else {
    [metrics, previousMetrics] = await Promise.all([metricRows(env, days), metricRows(env, days, true)]);
  }

  return html(
    renderStats(
      { daily, versions, platforms, crashes, metrics, previousMetrics, metricUsers, sources, overview, latestVersion, filters },
      user,
      activeModule,
    ),
  );
}

async function handleGroup(env: Env, fingerprint: string, user: User): Promise<Response> {
  const group = await env.DB.prepare("SELECT * FROM groups WHERE fingerprint = ?1").bind(fingerprint).first<Group>();
  if (!group) return new Response("not found", { status: 404 });
  group.severity = effectiveGroupSeverity(group);
  const reports = await env.DB.prepare(
    `SELECT version, os, arch, message, device, created_at, source, label, error_type, error_message,
      top_frame, build_commit, channel, language, view, breadcrumbs, component_stack, stack, occurred_at
     FROM reports WHERE fingerprint = ?1 ORDER BY id DESC`,
  )
    .bind(fingerprint)
    .all<{
      version: string;
      os: string;
      arch: string;
      message: string;
      device: string;
      created_at: string;
      source: string;
      label: string;
      error_type: string;
      error_message: string;
      top_frame: string;
      build_commit: string;
      channel: string;
      language: string;
      view: string;
      breadcrumbs: string;
      component_stack: string;
      stack: string;
      occurred_at: string;
    }>();
  return html(renderGroup(group, reports.results, user));
}

async function handleGroupAction(request: Request, env: Env, admin: User, fingerprint: string): Promise<Response> {
  if (!sameOrigin(request)) return new Response("forbidden", { status: 403 });
  const parsed = GroupAction.safeParse(await formObject(request));
  if (!parsed.success) return redirect(`/stats/group/${fingerprint}`);
  const a = parsed.data;

  if (a.action === "delete") {
    await env.DB.batch([
      env.DB.prepare("DELETE FROM reports WHERE fingerprint = ?1").bind(fingerprint),
      env.DB.prepare("DELETE FROM groups WHERE fingerprint = ?1").bind(fingerprint),
    ]);
    await logAction(env, admin, "delete_group", fingerprint.slice(0, 8));
    return redirect("/stats");
  }
  if (a.action === "status") {
    const status = a.status ?? "open";
    await env.DB.prepare(
      "UPDATE groups SET status = ?1, resolved_at = CASE WHEN ?1 = 'resolved' THEN ?3 ELSE resolved_at END WHERE fingerprint = ?2",
    )
      .bind(status, fingerprint, new Date().toISOString())
      .run();
    await logAction(env, admin, "set_status", fingerprint.slice(0, 8), status);
    return redirect(`/stats/group/${fingerprint}`);
  }
  if (a.action === "resolution") {
    await env.DB.prepare("UPDATE groups SET resolved_in = ?1 WHERE fingerprint = ?2")
      .bind(a.resolvedIn ?? "", fingerprint)
      .run();
    await logAction(env, admin, "set_resolved_in", fingerprint.slice(0, 8), a.resolvedIn ?? "");
    return redirect(`/stats/group/${fingerprint}`);
  }
  if (a.action === "severity") {
    await env.DB.prepare("UPDATE groups SET severity = ?1 WHERE fingerprint = ?2")
      .bind(a.severity ?? "medium", fingerprint)
      .run();
    await logAction(env, admin, "set_severity", fingerprint.slice(0, 8), a.severity ?? "medium");
    return redirect(`/stats/group/${fingerprint}`);
  }
  await env.DB.prepare("UPDATE groups SET note = ?1 WHERE fingerprint = ?2").bind(a.note ?? "", fingerprint).run();
  await logAction(env, admin, "set_note", fingerprint.slice(0, 8));
  return redirect(`/stats/group/${fingerprint}`);
}

async function handleAdminUsers(request: Request, env: Env, admin: User): Promise<Response> {
  if (!sameOrigin(request)) return new Response("forbidden", { status: 403 });
  const parsed = UserAction.safeParse(await formObject(request));
  if (!parsed.success) return redirect("/admin");
  const a = parsed.data;
  if (a.userId === admin.id) return redirect("/admin");

  const target = await env.DB.prepare("SELECT email, role FROM access WHERE id = ?1")
    .bind(a.userId)
    .first<{ email: string; role: Role }>();
  if (!target) return redirect("/admin");

  if (a.action === "delete") {
    await env.DB.prepare("DELETE FROM access WHERE id = ?1").bind(a.userId).run();
    await logAction(env, admin, "delete_user", target.email);
    return redirect("/admin");
  }

  const role: Role = a.role ?? "pending";
  const now = new Date().toISOString();
  await env.DB.prepare("UPDATE access SET role = ?1, approved_at = ?2, approved_by = ?3 WHERE id = ?4")
    .bind(role, role === "pending" ? null : now, admin.email, a.userId)
    .run();
  await logAction(env, admin, "set_role", target.email, `${target.role} → ${role}`);
  return redirect("/admin");
}

async function handleAdminList(env: Env, admin: User): Promise<Response> {
  const users = await env.DB.prepare(
    "SELECT id, email, role, created_at, approved_at FROM access ORDER BY (role = 'pending') DESC, created_at DESC",
  ).all<UserRow>();
  return html(renderUsers(admin, users.results));
}

async function handleAdminAudit(env: Env, admin: User): Promise<Response> {
  const rows = await env.DB.prepare(
    "SELECT at, actor_email, action, target, detail FROM audit_log ORDER BY id DESC LIMIT 200",
  ).all<AuditRow>();
  return html(renderAudit(admin, rows.results));
}

function requireViewer(user: User | null, login: string): Response | null {
  if (!user) return redirect(login);
  if (!atLeast(user.role, "viewer")) return redirect("/account");
  return null;
}

// The folded registry API runs against its own database and resolves identity
// itself; hand it the second binding plus the account/site origins it expects.
function registryBindings(env: Env): RegistryBindings {
  return {
    DB: env.REGISTRY_DB,
    WRITE_LIMITER: env.WRITE_LIMITER,
    ACCOUNTS_ORIGIN: env.ID_ORIGIN ?? "https://id.reasonix.io",
    APP_ORIGIN: env.APP_ORIGIN ?? "https://reasonix.io",
    ALLOWED_ORIGINS: env.ALLOWED_ORIGINS ?? "https://reasonix.io,https://www.reasonix.io",
  };
}

function communityStatus(url: URL): string {
  const s = url.searchParams.get("status") ?? "pending";
  return ["pending", "active", "hidden", "rejected"].includes(s) ? s : "pending";
}

async function handleCommunityList(env: Env, admin: User, status: string): Promise<Response> {
  const rows = await new PackageRepo(env.REGISTRY_DB).listByStatus(status, 200);
  return html(renderCommunity(admin, rows, status));
}

async function handleCommunityAction(
  request: Request,
  env: Env,
  admin: User,
  handle: string,
  name: string,
  action: string,
): Promise<Response> {
  if (!sameOrigin(request)) return new Response("forbidden", { status: 403 });
  const form = await formObject(request);
  const backStatus = ["pending", "active", "hidden", "rejected"].includes(form.status) ? form.status : "pending";
  const back = redirect(`/community?status=${backStatus}`);
  const slug = `${handle}/${name}`;
  const repo = new PackageRepo(env.REGISTRY_DB);
  const now = new Date().toISOString();

  if (action === "verify" || action === "unverify") {
    await repo.setVerified(slug, action === "verify", now);
    await logAction(env, admin, `pkg_${action}`, slug);
    return back;
  }
  if (action === "approve") {
    const before = await repo.bySlug(slug);
    const row = await repo.setStatus(slug, "active", now);
    // Emit the publish event on first approval so the feed only announces
    // packages that actually went public.
    if (row && before && before.status !== "active") {
      await new EventRepo(env.REGISTRY_DB).log({
        type: "publish",
        packageId: row.id,
        actorHandle: row.scope_handle,
        summary: `published ${row.slug}@${row.latest_version}`,
        now,
      });
    }
    await logAction(env, admin, "pkg_approve", slug);
    return back;
  }
  await repo.setStatus(slug, action === "reject" ? "rejected" : "hidden", now);
  await logAction(env, admin, `pkg_${action}`, slug);
  return back;
}

// Time-series retention, run by the daily cron trigger. Every dashboard query
// against the per-install tables reads at most the current window (-29 day),
// while the aggregate `metrics` table also serves the 30d view's
// previous-window delta (back to -59 day), so it keeps a doubled horizon.
// `reports`/`groups` are excluded on purpose: they are the triage queue and
// the regression baseline, are not date-partitioned, and their growth is
// already bounded by per-group sampling. Without this purge the database
// grows until D1's size cap, at which point every ingest write starts
// throwing (all of /v1/ping, /v1/metrics and /v1/report 500 while reads keep
// working — exactly the 2026-07-03 stats blackout).
const RETENTION = [
  { table: "pings", keepDays: 30 },
  { table: "metrics", keepDays: 60 },
  { table: "metric_users", keepDays: 30 },
] as const;
// Deletes run in rowid chunks so a run never holds one giant transaction.
// Steady state is one expired day per table; the chunk cap is a backstop that
// still drains ~2M rows per table per run after an ingest outage or backlog.
const RETENTION_CHUNK_ROWS = 10_000;
const RETENTION_MAX_CHUNKS = 200;

// Must match the sentinel entry in wrangler.toml [triggers] exactly — the
// scheduled handler dispatches on controller.cron; every other trigger
// (the retention cron, manual runs) falls through to the purge.
const SENTINEL_CRON = "17 1,7,13,19 * * *";

// Ingest sentinel. The 2026-07-03 blackout went unnoticed for ten days because
// clients swallow ping failures by design and nothing watched the write path.
// Four times a day (hours chosen so the UTC day always has >1h of traffic;
// ~14k DAU means a healthy hour is never empty) this probes the two failure
// shapes independently:
//   1. canary write into `pings` (immediately deleted) — catches writes
//      throwing, e.g. the D1 size cap, regardless of traffic;
//   2. today's real ping and open totals compared with the previous run —
//      catches ingest dying upstream of the worker (edge blocking, client
//      regression) even after the UTC day already has traffic.
// Alerts go to the optional ALERT_WEBHOOK secret; without it they still land
// in the worker logs. While broken this fires at most 4 alerts/day.
const CANARY_INSTALL_ID = "ffffffffffffffffffffffffffffffff";

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

async function sendAlert(env: Env, text: string): Promise<void> {
  if (!env.ALERT_WEBHOOK) return;
  try {
    const webhook = new URL(env.ALERT_WEBHOOK);
    const feishu = webhook.hostname === "open.feishu.cn" || webhook.hostname === "open.larksuite.com";
    const body = feishu ? { msg_type: "text", content: { text } } : { text };
    const res = await fetch(webhook.toString(), {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) console.error(`alert webhook responded ${res.status}`);
  } catch (err) {
    console.error("alert webhook unreachable", err);
  }
}

async function runIngestSentinel(env: Env): Promise<void> {
  const problems: string[] = [];
  try {
    await env.DB.prepare(
      `INSERT INTO pings (date, install_id, version, os, arch, opens)
       VALUES (date('now'), ?1, 'canary', 'canary', 'canary', 0)
       ON CONFLICT (date, install_id) DO NOTHING`,
    )
      .bind(CANARY_INSTALL_ID)
      .run();
    // Also removes any leftover canary from a run that died mid-way.
    await env.DB.prepare("DELETE FROM pings WHERE install_id = ?1").bind(CANARY_INSTALL_ID).run();
  } catch (err) {
    problems.push(`canary write failed: ${errText(err)}`);
  }
  try {
    // Auto-create the one-row checkpoint so existing databases do not need a
    // manual migration before this worker version is deployed.
    await env.DB.prepare(
      `CREATE TABLE IF NOT EXISTS ingest_sentinel_state (
         id INTEGER PRIMARY KEY CHECK (id = 1),
         day TEXT NOT NULL,
         ping_count INTEGER NOT NULL,
         open_count INTEGER NOT NULL,
         checked_at TEXT NOT NULL
       )`,
    ).run();
    const row = await env.DB.prepare(
      `SELECT date('now') AS day,
              COUNT(*) AS ping_count,
              COALESCE(SUM(opens), 0) AS open_count
       FROM pings
       WHERE date = date('now') AND install_id <> ?1`,
    )
      .bind(CANARY_INSTALL_ID)
      .first<{ day: string; ping_count: number; open_count: number }>();
    const day = row?.day ?? "";
    const pingCount = Number(row?.ping_count ?? 0);
    const openCount = Number(row?.open_count ?? 0);
    const previous = await env.DB.prepare(
      "SELECT day, ping_count, open_count, checked_at FROM ingest_sentinel_state WHERE id = 1",
    ).first<{ day: string; ping_count: number; open_count: number; checked_at: string }>();
    if (!pingCount) {
      problems.push("no launch pings recorded today (UTC)");
    } else if (
      previous?.day === day &&
      pingCount <= Number(previous.ping_count) &&
      openCount <= Number(previous.open_count)
    ) {
      problems.push(
        `launch ping totals unchanged since ${previous.checked_at} UTC (${pingCount} install rows, ${openCount} opens)`,
      );
    }
    await env.DB.prepare(
      `INSERT INTO ingest_sentinel_state (id, day, ping_count, open_count, checked_at)
       VALUES (1, ?1, ?2, ?3, datetime('now'))
       ON CONFLICT (id) DO UPDATE SET
         day = ?1, ping_count = ?2, open_count = ?3, checked_at = datetime('now')`,
    )
      .bind(day, pingCount, openCount)
      .run();
  } catch (err) {
    problems.push(`ping progress check failed: ${errText(err)}`);
  }
  if (!problems.length) return;
  const message = `crash.reasonix.io ingest sentinel: ${problems.join("; ")} — https://crash.reasonix.io/stats`;
  console.error(message);
  await sendAlert(env, message);
}

async function purgeExpiredStatsRows(env: Env): Promise<void> {
  for (const { table, keepDays } of RETENTION) {
    // Keep exactly the newest `keepDays` dates: today plus keepDays-1 back,
    // matching the `date >= date('now', '-{keepDays-1} day')` reads.
    const cutoff = `-${keepDays - 1} day`;
    let purged = 0;
    try {
      for (let i = 0; i < RETENTION_MAX_CHUNKS; i++) {
        const res = await env.DB.prepare(
          `DELETE FROM ${table} WHERE rowid IN (
             SELECT rowid FROM ${table} WHERE date < date('now', ?1) LIMIT ${RETENTION_CHUNK_ROWS}
           )`,
        )
          .bind(cutoff)
          .run();
        const changes = res.meta.changes ?? 0;
        purged += changes;
        if (changes < RETENTION_CHUNK_ROWS) break;
      }
      console.log(`retention: purged ${purged} rows from ${table} (keep ${keepDays}d)`);
    } catch (err) {
      // One broken table must not stop the others; the cron retries tomorrow.
      console.error(`retention: purge failed for ${table} after ${purged} rows`, err);
    }
  }
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const path = url.pathname;
    const method = request.method;

    const desktopRelease = desktopReleaseChannel(path);
    if (desktopRelease && method === "GET") return handleDesktopReleaseManifest(desktopRelease);

    if (path === "/v1/report" && method === "POST") return handleReport(request, env);
    if (path === "/v1/ping" && method === "POST") return handlePing(request, env);
    if (path === "/v1/metrics" && method === "POST") return handleMetrics(request, env);

    // Skill/MCP registry API — the folded Hono app handles its own auth, CORS
    // and rate limiting against the registry database (public reads + publish,
    // plus the JSON /v1/admin the site's moderation panel calls).
    if (path.startsWith("/v1/packages") || path === "/v1/activity" || path.startsWith("/v1/admin")) {
      return registryApp.fetch(request, registryBindings(env));
    }

    const login = loginUrl(env, request);

    // Authentication moved to id.reasonix.io; these paths just bounce there.
    if ((path === "/login" || path === "/register") && method === "GET") return redirect(login);
    if (path === "/logout" && method === "POST") return redirect(login, await sharedLogout(request, env));

    const user = await currentUser(request, env);

    if (path === "/") return redirect(user ? (atLeast(user.role, "viewer") ? "/stats" : "/account") : login);

    if (path === "/account" && method === "GET") return user ? html(renderAccount(user)) : redirect(login);

    const groupFingerprint = groupFingerprintFromPath(path);
    const statsModuleMatch = path.match(/^\/stats\/(diagnostics|usage|preferences|health)$/);
    if ((path === "/stats" || statsModuleMatch) && method === "GET")
      return requireViewer(user, login) ?? handleStats(request, env, user as User, (statsModuleMatch?.[1] as StatsModule | undefined) ?? "usage");
    if (groupFingerprint && method === "GET") return requireViewer(user, login) ?? handleGroup(env, groupFingerprint, user as User);
    if (groupFingerprint && method === "POST") {
      if (user?.role !== "admin") return new Response("forbidden", { status: 403 });
      return handleGroupAction(request, env, user, groupFingerprint);
    }

    if (path === "/admin" && method === "GET") {
      if (!user) return redirect(login);
      return user.role === "admin" ? handleAdminList(env, user) : redirect("/account");
    }
    if (path === "/admin/audit" && method === "GET") {
      if (!user) return redirect(login);
      return user.role === "admin" ? handleAdminAudit(env, user) : redirect("/account");
    }
    if (path === "/admin/users" && method === "POST") {
      if (user?.role !== "admin") return new Response("forbidden", { status: 403 });
      return handleAdminUsers(request, env, user);
    }

    if (path === "/community" && method === "GET") {
      if (!user) return redirect(login);
      return user.role === "admin" ? handleCommunityList(env, user, communityStatus(url)) : redirect("/account");
    }
    const pkgActionMatch = path.match(/^\/community\/([^/]+)\/([^/]+)\/(approve|reject|hide|verify|unverify)$/);
    if (pkgActionMatch && method === "POST") {
      if (user?.role !== "admin") return new Response("forbidden", { status: 403 });
      return handleCommunityAction(request, env, user, pkgActionMatch[1], pkgActionMatch[2], pkgActionMatch[3]);
    }

    if (
      path === "/v1/report" ||
      path === "/v1/ping" ||
      path === "/v1/metrics" ||
      desktopReleaseChannel(path) ||
      path === "/login" ||
      path === "/register" ||
      path === "/logout" ||
      path === "/account" ||
      path.startsWith("/stats") ||
      path.startsWith("/admin") ||
      path.startsWith("/community")
    ) {
      return new Response("method not allowed", { status: 405 });
    }
    return new Response("not found", { status: 404 });
  },

  async scheduled(controller: ScheduledController, env: Env, ctx: ExecutionContext): Promise<void> {
    if (controller.cron === SENTINEL_CRON) {
      ctx.waitUntil(runIngestSentinel(env));
      return;
    }
    ctx.waitUntil(purgeExpiredStatsRows(env));
  },
};
