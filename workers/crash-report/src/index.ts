// Ingest + dashboard for desktop crash/feedback reports and the anonymous launch
// ping. Reports are user-initiated; pings are opt-out (desktop.telemetry).
import { z } from "zod";
import type { Env } from "./env";
import { html, redirect } from "./shell";
import { renderGroup, renderStats, type Group } from "./stats";
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

const MAX_BODY_BYTES = 32 * 1024;
const SAMPLES_PER_GROUP = 5;

const Device = z
  .object({
    osVersion: z.string().max(128),
    cpu: z.string().max(128),
    cores: z.number().int().min(0).max(4096),
    ramGb: z.number().min(0).max(65536),
  })
  .partial();

const Report = z.object({
  kind: z.enum(["crash", "feedback"]),
  version: z.string().min(1).max(64),
  os: z.string().min(1).max(32),
  arch: z.string().min(1).max(32),
  message: z.string().min(1).max(16 * 1024),
  device: Device.optional(),
});

const Ping = z.object({
  installId: z.string().regex(/^[0-9a-f]{32}$/),
  version: z.string().min(1).max(64),
  os: z.string().min(1).max(32),
  arch: z.string().min(1).max(32),
  osVersion: z.string().max(128).optional(),
});

// Opt-in aggregate agent metrics: a per-launch snapshot of (signal, bucket)
// counters. No install id, no content — just enumerated signals so the worker
// table can never be polluted with arbitrary keys.
const METRIC_SIGNALS = [
  "finish_reason",
  "empty_final",
  "provider_error",
  "cache_hit",
  "tool_error",
  "compaction",
  "turns",
] as const;

const Metrics = z.object({
  version: z.string().min(1).max(64),
  os: z.string().min(1).max(32),
  counters: z
    .array(
      z.object({
        signal: z.enum(METRIC_SIGNALS),
        bucket: z
          .string()
          .min(1)
          .max(32)
          .regex(/^[a-z0-9_]+$/),
        count: z.number().int().min(1).max(1_000_000),
      }),
    )
    .min(1)
    .max(64),
});

export function normalizeForFingerprint(kind: string, message: string): string {
  const head = message.split("\n").slice(0, 12).join("\n");
  return (
    kind +
    "\n" +
    head
      .replace(/[A-Za-z]:\\[^\s)('"]+/g, "<path>")
      .replace(/(?:wails|https?|file):\/\/[^\s)('"]+/g, "<url>")
      .replace(/0x[0-9a-fA-F]+/g, "<addr>")
      .replace(/:\d+(?::\d+)?/g, ":<n>")
  );
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

  const fingerprint = await sha256Hex(normalizeForFingerprint(r.kind, r.message));
  const now = new Date().toISOString();

  await env.DB.prepare(
    `INSERT INTO groups (fingerprint, kind, count, first_seen, last_seen, last_version)
     VALUES (?1, ?2, 1, ?3, ?3, ?4)
     ON CONFLICT (fingerprint) DO UPDATE SET
       count = count + 1, last_seen = ?3, last_version = ?4`,
  )
    .bind(fingerprint, r.kind, now, r.version)
    .run();

  const group = await env.DB.prepare("SELECT count FROM groups WHERE fingerprint = ?1")
    .bind(fingerprint)
    .first<{ count: number }>();
  if ((group?.count ?? 1) <= SAMPLES_PER_GROUP) {
    await env.DB.prepare(
      `INSERT INTO reports (fingerprint, kind, version, os, arch, message, device, created_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
    )
      .bind(fingerprint, r.kind, r.version, r.os, r.arch, r.message, JSON.stringify(r.device ?? {}), now)
      .run();
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
  action: z.enum(["status", "delete", "note"]),
  status: z.enum(["open", "resolved", "ignored"]).optional(),
  note: z.string().max(500).optional(),
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

async function handleStats(env: Env, user: User): Promise<Response> {
  const [daily, versions, platforms, crashes, metrics] = await Promise.all([
    env.DB.prepare(
      "SELECT date, COUNT(*) AS users, SUM(opens) AS opens FROM pings WHERE date >= date('now', '-29 day') GROUP BY date",
    ).all<{ date: string; users: number; opens: number }>(),
    env.DB.prepare(
      "SELECT version AS label, COUNT(DISTINCT install_id) AS users FROM pings WHERE date >= date('now', '-6 day') GROUP BY label ORDER BY users DESC LIMIT 15",
    ).all<{ label: string; users: number }>(),
    env.DB.prepare(
      "SELECT os || ' ' || arch AS label, COUNT(DISTINCT install_id) AS users FROM pings WHERE date >= date('now', '-6 day') GROUP BY label ORDER BY users DESC",
    ).all<{ label: string; users: number }>(),
    env.DB.prepare(
      "SELECT fingerprint, kind, count, last_version, substr(last_seen, 1, 10) AS seen, status FROM groups ORDER BY last_seen DESC LIMIT 25",
    ).all<{ fingerprint: string; kind: string; count: number; last_version: string; seen: string; status: string }>(),
    env.DB.prepare(
      "SELECT signal, bucket, SUM(count) AS total FROM metrics WHERE date >= date('now', '-6 day') GROUP BY signal, bucket ORDER BY signal, total DESC",
    ).all<{ signal: string; bucket: string; total: number }>(),
  ]);
  return html(
    renderStats(
      {
        daily: daily.results,
        versions: versions.results,
        platforms: platforms.results,
        crashes: crashes.results,
        metrics: metrics.results,
      },
      user,
    ),
  );
}

async function handleGroup(env: Env, fingerprint: string, user: User): Promise<Response> {
  const group = await env.DB.prepare("SELECT * FROM groups WHERE fingerprint = ?1").bind(fingerprint).first<Group>();
  if (!group) return new Response("not found", { status: 404 });
  const reports = await env.DB.prepare(
    "SELECT version, os, arch, message, device, created_at FROM reports WHERE fingerprint = ?1 ORDER BY id DESC",
  )
    .bind(fingerprint)
    .all<{ version: string; os: string; arch: string; message: string; device: string; created_at: string }>();
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
    await env.DB.prepare("UPDATE groups SET status = ?1 WHERE fingerprint = ?2").bind(status, fingerprint).run();
    await logAction(env, admin, "set_status", fingerprint.slice(0, 8), status);
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
    if (path === "/stats" && method === "GET") return requireViewer(user) ?? handleStats(env, user as User);
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
