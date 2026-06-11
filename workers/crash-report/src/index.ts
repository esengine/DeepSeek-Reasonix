// Ingest endpoint for desktop crash/feedback reports — user-initiated only (the
// client sends nothing without an explicit click on the crash overlay).
import { z } from "zod";

interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

interface Env {
  DB: D1Database;
  RATE_LIMITER: RateLimiter;
}

const MAX_BODY_BYTES = 32 * 1024;
const SAMPLES_PER_GROUP = 5;

const Report = z.object({
  kind: z.enum(["crash", "feedback"]),
  version: z.string().min(1).max(64),
  os: z.string().min(1).max(32),
  arch: z.string().min(1).max(32),
  message: z.string().min(1).max(16 * 1024),
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

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname !== "/v1/report") return new Response("not found", { status: 404 });
    if (request.method !== "POST") return new Response("method not allowed", { status: 405 });

    const length = Number(request.headers.get("content-length") ?? "0");
    if (!length || length > MAX_BODY_BYTES) return new Response("payload too large", { status: 413 });

    const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
    const { success } = await env.RATE_LIMITER.limit({ key: ip });
    if (!success) return new Response("rate limited", { status: 429 });

    let parsed: z.infer<typeof Report>;
    try {
      parsed = Report.parse(JSON.parse(await request.text()));
    } catch {
      return new Response("bad request", { status: 400 });
    }

    const fingerprint = await sha256Hex(normalizeForFingerprint(parsed.kind, parsed.message));
    const now = new Date().toISOString();

    await env.DB.prepare(
      `INSERT INTO groups (fingerprint, kind, count, first_seen, last_seen, last_version)
       VALUES (?1, ?2, 1, ?3, ?3, ?4)
       ON CONFLICT (fingerprint) DO UPDATE SET
         count = count + 1, last_seen = ?3, last_version = ?4`,
    )
      .bind(fingerprint, parsed.kind, now, parsed.version)
      .run();

    const group = await env.DB.prepare("SELECT count FROM groups WHERE fingerprint = ?1")
      .bind(fingerprint)
      .first<{ count: number }>();
    if ((group?.count ?? 1) <= SAMPLES_PER_GROUP) {
      await env.DB.prepare(
        `INSERT INTO reports (fingerprint, kind, version, os, arch, message, created_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)`,
      )
        .bind(fingerprint, parsed.kind, parsed.version, parsed.os, parsed.arch, parsed.message, now)
        .run();
    }

    return new Response("ok", { status: 202 });
  },
};
