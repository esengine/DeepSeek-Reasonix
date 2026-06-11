// Ingest + stats for desktop crash/feedback reports and the anonymous launch
// ping. Reports are user-initiated; pings are opt-out (desktop.telemetry).
import { z } from "zod";
import { renderGroup, renderStats } from "./stats";

interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

interface Env {
  DB: D1Database;
  RATE_LIMITER: RateLimiter;
  PING_LIMITER: RateLimiter;
  METRICS_LIMITER: RateLimiter;
  STATS_PASSWORD?: string;
}

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

function statsAuthorized(request: Request, env: Env): boolean {
  // trim: secrets piped in via PowerShell arrive with a trailing newline.
  const want = (env.STATS_PASSWORD ?? "").trim();
  if (!want) return false;
  const header = request.headers.get("authorization") ?? "";
  if (!header.startsWith("Basic ")) return false;
  let pass: string;
  try {
    pass = atob(header.slice(6)).split(":").slice(1).join(":");
  } catch {
    return false;
  }
  const enc = new TextEncoder();
  const a = enc.encode(pass);
  const b = enc.encode(want);
  return a.byteLength === b.byteLength && crypto.subtle.timingSafeEqual(a, b);
}

function html(body: string): Response {
  return new Response(body, { headers: { "content-type": "text/html; charset=utf-8" } });
}

async function handleStats(env: Env): Promise<Response> {
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
      "SELECT fingerprint, kind, count, last_version, substr(last_seen, 1, 10) AS seen FROM groups ORDER BY last_seen DESC LIMIT 25",
    ).all<{ fingerprint: string; kind: string; count: number; last_version: string; seen: string }>(),
    env.DB.prepare(
      "SELECT signal, bucket, SUM(count) AS total FROM metrics WHERE date >= date('now', '-6 day') GROUP BY signal, bucket ORDER BY signal, total DESC",
    ).all<{ signal: string; bucket: string; total: number }>(),
  ]);
  return html(
    renderStats({
      daily: daily.results,
      versions: versions.results,
      platforms: platforms.results,
      crashes: crashes.results,
      metrics: metrics.results,
    }),
  );
}

async function handleGroup(env: Env, fingerprint: string): Promise<Response> {
  const group = await env.DB.prepare("SELECT * FROM groups WHERE fingerprint = ?1")
    .bind(fingerprint)
    .first<{ fingerprint: string; kind: string; count: number; first_seen: string; last_seen: string; last_version: string }>();
  if (!group) return new Response("not found", { status: 404 });
  const reports = await env.DB.prepare(
    "SELECT version, os, arch, message, device, created_at FROM reports WHERE fingerprint = ?1 ORDER BY id DESC",
  )
    .bind(fingerprint)
    .all<{ version: string; os: string; arch: string; message: string; device: string; created_at: string }>();
  return html(renderGroup(group, reports.results));
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/v1/report" && request.method === "POST") return handleReport(request, env);
    if (url.pathname === "/v1/ping" && request.method === "POST") return handlePing(request, env);
    if (url.pathname === "/v1/metrics" && request.method === "POST") return handleMetrics(request, env);

    const group = url.pathname.match(/^\/stats\/group\/([0-9a-f]{64})$/);
    if ((url.pathname === "/stats" || group) && request.method === "GET") {
      if (!statsAuthorized(request, env)) {
        return new Response("auth required", {
          status: 401,
          headers: { "www-authenticate": 'Basic realm="reasonix-stats"' },
        });
      }
      return group ? handleGroup(env, group[1]) : handleStats(env);
    }
    if (
      url.pathname === "/v1/report" ||
      url.pathname === "/v1/ping" ||
      url.pathname === "/v1/metrics" ||
      url.pathname.startsWith("/stats")
    ) {
      return new Response("method not allowed", { status: 405 });
    }
    return new Response("not found", { status: 404 });
  },
};
