// Ingest + dashboard for desktop crash/feedback/performance reports and the
// anonymous launch ping. Reports are user-initiated; pings are opt-out
// (desktop.telemetry).
import { z } from "zod";
import type { Env } from "./env";
import { html, redirect } from "./shell";
import { renderGroup, renderStats, type Group, type StatsModule } from "./stats";
import { renderLogin, renderRegister, renderAccount } from "./auth_pages";
import { renderUsers, renderAudit, type UserRow, type AuditRow } from "./admin";
import {
  atLeast,
  createSession,
  currentUser,
  endSession,
  hashPassword,
  isAdminEmail,
  logAction,
  sameOrigin,
  sessionCookie,
  clearCookie,
  verifyPassword,
  type Role,
  type User,
} from "./auth";

const MAX_BODY_BYTES = 96 * 1024;
const LATEST_SAMPLES_PER_GROUP = 5;

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
  "compaction",
  "turns",
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
  source: string;
  label: string;
  errorType: string;
  errorMessage: string;
  topFrame: string;
};

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
  if (isOpaqueScriptErrorReport(input)) return "low";
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
      })
    : normalizeForFingerprint(r.kind, message);
  const fingerprint = await sha256Hex(fingerprintBasis);
  const now = new Date().toISOString();
  const title = crashTitle(message);
  const source = r.source ?? "legacy";
  const label = r.label ?? "";
  const errorType = r.errorType ?? "";
  const buildCommit = r.buildCommit ?? "";
  const channel = r.channel ?? "";
  const severity = severityForReport({ kind: r.kind, source, label, errorType, errorMessage, topFrame });
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

  await env.DB.prepare(
    `INSERT INTO pings (date, install_id, version, os, arch, os_version, opens)
     VALUES (date('now'), ?1, ?2, ?3, ?4, ?5, 1)
     ON CONFLICT (date, install_id) DO UPDATE SET
       opens = opens + 1, version = ?2, os_version = ?5`,
  )
    .bind(p.installId, p.version, p.os, p.arch, p.osVersion ?? "")
    .run();

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
  await env.DB.batch(m.counters.map((c) => upsert.bind(m.version, m.os, c.signal, c.bucket, c.count)));
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

const Credentials = z.object({
  email: z.string().email().max(254),
  password: z.string().min(8).max(200),
});

const PasswordChange = z.object({
  current: z.string().min(1).max(200),
  next: z.string().min(8).max(200),
});

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

async function handleRegister(request: Request, env: Env): Promise<Response> {
  if (!sameOrigin(request)) return new Response("forbidden", { status: 403 });
  const parsed = Credentials.safeParse(await formObject(request));
  if (!parsed.success)
    return html(renderRegister({ kind: "err", text: "Enter a valid email and a password of at least 8 characters." }));
  const email = parsed.data.email.toLowerCase();

  const existing = await env.DB.prepare("SELECT id FROM users WHERE email = ?1").bind(email).first();
  if (existing) return html(renderRegister({ kind: "err", text: "That email is already registered — try signing in." }));

  const role: Role = isAdminEmail(env, email) ? "admin" : "pending";
  const now = new Date().toISOString();
  const hash = await hashPassword(parsed.data.password);
  const res = await env.DB.prepare(
    "INSERT INTO users (email, password_hash, role, created_at, approved_at) VALUES (?1, ?2, ?3, ?4, ?5)",
  )
    .bind(email, hash, role, now, role === "admin" ? now : null)
    .run();

  const token = await createSession(env, res.meta.last_row_id);
  return redirect(role === "pending" ? "/account" : "/stats", sessionCookie(token));
}

async function handleLogin(request: Request, env: Env): Promise<Response> {
  if (!sameOrigin(request)) return new Response("forbidden", { status: 403 });
  const parsed = Credentials.safeParse(await formObject(request));
  if (!parsed.success) return html(renderLogin({ kind: "err", text: "Enter a valid email and password." }));
  const email = parsed.data.email.toLowerCase();

  const row = await env.DB.prepare("SELECT id, password_hash, role FROM users WHERE email = ?1")
    .bind(email)
    .first<{ id: number; password_hash: string; role: Role }>();
  const ok = row ? await verifyPassword(parsed.data.password, row.password_hash) : false;
  if (!row || !ok) return html(renderLogin({ kind: "err", text: "Wrong email or password." }));

  const token = await createSession(env, row.id);
  return redirect(atLeast(row.role, "viewer") ? "/stats" : "/account", sessionCookie(token));
}

async function handleAccountPassword(request: Request, env: Env, user: User): Promise<Response> {
  if (!sameOrigin(request)) return new Response("forbidden", { status: 403 });
  const parsed = PasswordChange.safeParse(await formObject(request));
  if (!parsed.success) return html(renderAccount(user, { kind: "err", text: "New password must be at least 8 characters." }));

  const row = await env.DB.prepare("SELECT password_hash FROM users WHERE id = ?1")
    .bind(user.id)
    .first<{ password_hash: string }>();
  if (!row || !(await verifyPassword(parsed.data.current, row.password_hash)))
    return html(renderAccount(user, { kind: "err", text: "Current password is incorrect." }));

  await env.DB.prepare("UPDATE users SET password_hash = ?1 WHERE id = ?2")
    .bind(await hashPassword(parsed.data.next), user.id)
    .run();
  return html(renderAccount(user, { kind: "ok", text: "Password updated." }));
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
      status, title, source, label, error_type, top_frame, severity, last_os, last_arch, regressed_at
    FROM groups ${where.length ? `WHERE ${where.join(" AND ")}` : ""}
    ORDER BY
      CASE WHEN status = 'open' THEN 0 ELSE 1 END,
      CASE WHEN regressed_at <> '' THEN 0 ELSE 1 END,
      ${latestOrder}
      CASE severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END,
      count DESC,
      last_seen DESC
    LIMIT 50`;
  const stmt = env.DB.prepare(sql);
  const query = binds.length ? stmt.bind(...binds) : stmt;
  return query.all<{
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
    regressed_at: string;
  }>();
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
  const diagnosticCounts = latestVersion
    ? env.DB.prepare(
        `SELECT
          SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END) AS open_reports,
          SUM(CASE WHEN first_version = ?1 THEN 1 ELSE 0 END) AS new_latest_reports,
          SUM(CASE WHEN regressed_at <> '' THEN 1 ELSE 0 END) AS regressed_reports,
          SUM(CASE WHEN status = 'open' AND severity IN ('critical', 'high') THEN 1 ELSE 0 END) AS critical_open_reports
        FROM groups`,
      )
        .bind(latestVersion)
        .first<{ open_reports: number; new_latest_reports: number; regressed_reports: number; critical_open_reports: number }>()
    : env.DB.prepare(
        `SELECT
          SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END) AS open_reports,
          0 AS new_latest_reports,
          SUM(CASE WHEN regressed_at <> '' THEN 1 ELSE 0 END) AS regressed_reports,
          SUM(CASE WHEN status = 'open' AND severity IN ('critical', 'high') THEN 1 ELSE 0 END) AS critical_open_reports
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

async function handleStats(request: Request, env: Env, user: User, activeModule: StatsModule): Promise<Response> {
  const url = new URL(request.url);
  const filters = statsFilters(url);
  const latestVersion = await latestObservedVersion(env);
  const days = filters.windowDays;
  const [daily, versions, platforms, crashes, metrics, previousMetrics, metricUsers, sources, overview] = await Promise.all([
    env.DB.prepare(
      `SELECT date, COUNT(*) AS users, SUM(opens) AS opens FROM pings WHERE date >= date('now', '${currentWindowSince(days)}') GROUP BY date`,
    ).all<{ date: string; users: number; opens: number }>(),
    env.DB.prepare(
      `SELECT version AS label, COUNT(DISTINCT install_id) AS users FROM pings WHERE date >= date('now', '${currentWindowSince(days)}') GROUP BY label ORDER BY users DESC LIMIT 15`,
    ).all<{ label: string; users: number }>(),
    env.DB.prepare(
      `SELECT os || ' ' || arch AS label, COUNT(DISTINCT install_id) AS users FROM pings WHERE date >= date('now', '${currentWindowSince(days)}') GROUP BY label ORDER BY users DESC`,
    ).all<{ label: string; users: number }>(),
    crashGroups(env, filters, latestVersion),
    metricRows(env, days),
    metricRows(env, days, true),
    metricUserRows(env, days),
    env.DB.prepare("SELECT source AS label, COUNT(*) AS users FROM groups GROUP BY source ORDER BY users DESC").all<{ label: string; users: number }>(),
    diagnosticOverview(env, latestVersion, days),
  ]);
  return html(
    renderStats(
      {
        daily: daily.results,
        versions: versions.results,
        platforms: platforms.results,
        crashes: crashes.results,
        metrics,
        previousMetrics,
        metricUsers,
        sources: sources.results,
        overview,
        latestVersion,
        filters,
      },
      user,
      activeModule,
    ),
  );
}

async function handleGroup(env: Env, fingerprint: string, user: User): Promise<Response> {
  const group = await env.DB.prepare("SELECT * FROM groups WHERE fingerprint = ?1").bind(fingerprint).first<Group>();
  if (!group) return new Response("not found", { status: 404 });
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

  const target = await env.DB.prepare("SELECT email, role FROM users WHERE id = ?1")
    .bind(a.userId)
    .first<{ email: string; role: Role }>();
  if (!target) return redirect("/admin");

  if (a.action === "delete") {
    await env.DB.batch([
      env.DB.prepare("DELETE FROM sessions WHERE user_id = ?1").bind(a.userId),
      env.DB.prepare("DELETE FROM users WHERE id = ?1").bind(a.userId),
    ]);
    await logAction(env, admin, "delete_user", target.email);
    return redirect("/admin");
  }

  const role: Role = a.role ?? "pending";
  const now = new Date().toISOString();
  await env.DB.prepare("UPDATE users SET role = ?1, approved_at = ?2, approved_by = ?3 WHERE id = ?4")
    .bind(role, role === "pending" ? null : now, admin.id, a.userId)
    .run();
  await logAction(env, admin, "set_role", target.email, `${target.role} → ${role}`);
  return redirect("/admin");
}

async function handleAdminList(env: Env, admin: User): Promise<Response> {
  const users = await env.DB.prepare(
    "SELECT id, email, role, created_at, approved_at FROM users ORDER BY (role = 'pending') DESC, created_at DESC",
  ).all<UserRow>();
  return html(renderUsers(admin, users.results));
}

async function handleAdminAudit(env: Env, admin: User): Promise<Response> {
  const rows = await env.DB.prepare(
    "SELECT at, actor_email, action, target, detail FROM audit_log ORDER BY id DESC LIMIT 200",
  ).all<AuditRow>();
  return html(renderAudit(admin, rows.results));
}

function requireViewer(user: User | null): Response | null {
  if (!user) return redirect("/login");
  if (!atLeast(user.role, "viewer")) return redirect("/account");
  return null;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const path = url.pathname;
    const method = request.method;

    if (path === "/v1/report" && method === "POST") return handleReport(request, env);
    if (path === "/v1/ping" && method === "POST") return handlePing(request, env);
    if (path === "/v1/metrics" && method === "POST") return handleMetrics(request, env);

    if (path === "/register" && method === "GET") return html(renderRegister());
    if (path === "/register" && method === "POST") return handleRegister(request, env);
    if (path === "/login" && method === "GET") return html(renderLogin());
    if (path === "/login" && method === "POST") return handleLogin(request, env);
    if (path === "/logout" && method === "POST") {
      await endSession(request, env);
      return redirect("/login", clearCookie());
    }

    const user = await currentUser(request, env);

    if (path === "/") return redirect(user ? (atLeast(user.role, "viewer") ? "/stats" : "/account") : "/login");

    if (path === "/account" && method === "GET")
      return user ? html(renderAccount(user)) : redirect("/login");
    if (path === "/account/password" && method === "POST")
      return user ? handleAccountPassword(request, env, user) : redirect("/login");

    const groupMatch = path.match(/^\/stats\/group\/([0-9a-f]{64})$/);
    const statsModuleMatch = path.match(/^\/stats\/(diagnostics|usage|preferences|health)$/);
    if ((path === "/stats" || statsModuleMatch) && method === "GET")
      return requireViewer(user) ?? handleStats(request, env, user as User, (statsModuleMatch?.[1] as StatsModule | undefined) ?? "usage");
    if (groupMatch && method === "GET") return requireViewer(user) ?? handleGroup(env, groupMatch[1], user as User);
    if (groupMatch && method === "POST") {
      if (user?.role !== "admin") return new Response("forbidden", { status: 403 });
      return handleGroupAction(request, env, user, groupMatch[1]);
    }

    if (path === "/admin" && method === "GET") {
      if (!user) return redirect("/login");
      return user.role === "admin" ? handleAdminList(env, user) : redirect("/account");
    }
    if (path === "/admin/audit" && method === "GET") {
      if (!user) return redirect("/login");
      return user.role === "admin" ? handleAdminAudit(env, user) : redirect("/account");
    }
    if (path === "/admin/users" && method === "POST") {
      if (user?.role !== "admin") return new Response("forbidden", { status: 403 });
      return handleAdminUsers(request, env, user);
    }

    if (
      path === "/v1/report" ||
      path === "/v1/ping" ||
      path === "/v1/metrics" ||
      path === "/login" ||
      path === "/register" ||
      path === "/logout" ||
      path === "/account" ||
      path === "/account/password" ||
      path.startsWith("/stats") ||
      path.startsWith("/admin")
    ) {
      return new Response("method not allowed", { status: 405 });
    }
    return new Response("not found", { status: 404 });
  },
};
