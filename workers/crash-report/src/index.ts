// Ingest + stats for desktop crash/feedback reports and the anonymous launch
// ping. Reports are user-initiated; pings are opt-out (desktop.telemetry).
import { z } from "zod";

interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

interface Env {
  DB: D1Database;
  RATE_LIMITER: RateLimiter;
  PING_LIMITER: RateLimiter;
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

function esc(s: unknown): string {
  return String(s).replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

function barTable(rows: Record<string, unknown>[], labelKey: string, valueKey: string, extraKeys: string[] = []): string {
  const max = Math.max(1, ...rows.map((r) => Number(r[valueKey]) || 0));
  const tr = rows
    .map((r) => {
      const v = Number(r[valueKey]) || 0;
      const extras = extraKeys.map((k) => `<td>${esc(r[k])}</td>`).join("");
      return `<tr><td>${esc(r[labelKey])}</td><td class="num">${v}</td>${extras}<td class="bar"><div style="width:${Math.round((v / max) * 100)}%"></div></td></tr>`;
    })
    .join("");
  return `<table><tbody>${tr}</tbody></table>`;
}

async function handleStats(env: Env): Promise<Response> {
  const [daily, versions, oses, crashes] = await Promise.all([
    env.DB.prepare(
      "SELECT date, COUNT(*) AS users, SUM(opens) AS opens FROM pings WHERE date >= date('now', '-29 day') GROUP BY date ORDER BY date DESC",
    ).all(),
    env.DB.prepare(
      "SELECT version, COUNT(DISTINCT install_id) AS users FROM pings WHERE date >= date('now', '-6 day') GROUP BY version ORDER BY users DESC LIMIT 15",
    ).all(),
    env.DB.prepare(
      "SELECT os || ' ' || arch AS platform, COUNT(DISTINCT install_id) AS users FROM pings WHERE date >= date('now', '-6 day') GROUP BY platform ORDER BY users DESC",
    ).all(),
    env.DB.prepare(
      "SELECT substr(fingerprint, 1, 8) AS fp, kind, count, last_version, substr(last_seen, 1, 10) AS seen FROM groups ORDER BY last_seen DESC LIMIT 20",
    ).all(),
  ]);

  const crashRows = (crashes.results as Record<string, unknown>[])
    .map(
      (r) =>
        `<tr><td><code>${esc(r.fp)}</code></td><td>${esc(r.kind)}</td><td class="num">${esc(r.count)}</td><td>${esc(r.last_version)}</td><td>${esc(r.seen)}</td></tr>`,
    )
    .join("");

  const html = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Reasonix stats</title><style>
body{font:14px/1.5 system-ui,sans-serif;background:#1a1a2e;color:#e6e6f0;max-width:880px;margin:24px auto;padding:0 16px}
h1{font-size:18px}h2{font-size:15px;margin-top:28px;color:#9f9fc0}
table{border-collapse:collapse;width:100%}td,th{padding:4px 10px 4px 0;text-align:left;border-bottom:1px solid #2a2a40}
td.num{text-align:right;font-variant-numeric:tabular-nums}
td.bar{width:45%}td.bar div{background:#5b8cff;height:10px;border-radius:3px;min-width:2px}
code{color:#9f9fc0}.hint{color:#8a8aa3;font-size:12px}
</style></head><body>
<h1>Reasonix desktop stats</h1>
<h2>Daily active installs (30 days) — users / opens</h2>
${barTable(daily.results as Record<string, unknown>[], "date", "users", ["opens"])}
<h2>Versions (last 7 days, distinct installs)</h2>
${barTable(versions.results as Record<string, unknown>[], "version", "users")}
<h2>Platforms (last 7 days, distinct installs)</h2>
${barTable(oses.results as Record<string, unknown>[], "platform", "users")}
<h2>Recent crash groups</h2>
<table><thead><tr><th>fingerprint</th><th>kind</th><th>count</th><th>last version</th><th>last seen</th></tr></thead>
<tbody>${crashRows}</tbody></table>
<p class="hint">Crash stacks: wrangler d1 execute reasonix-crash --remote --command "SELECT message FROM reports WHERE fingerprint LIKE '&lt;fp&gt;%'"</p>
</body></html>`;
  return new Response(html, { headers: { "content-type": "text/html; charset=utf-8" } });
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/v1/report" && request.method === "POST") return handleReport(request, env);
    if (url.pathname === "/v1/ping" && request.method === "POST") return handlePing(request, env);
    if (url.pathname === "/stats" && request.method === "GET") {
      if (!statsAuthorized(request, env)) {
        return new Response("auth required", {
          status: 401,
          headers: { "www-authenticate": 'Basic realm="reasonix-stats"' },
        });
      }
      return handleStats(env);
    }
    if (url.pathname === "/v1/report" || url.pathname === "/v1/ping" || url.pathname === "/stats") {
      return new Response("method not allowed", { status: 405 });
    }
    return new Response("not found", { status: 404 });
  },
};
