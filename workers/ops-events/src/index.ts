import type { Env } from "./env";
import { cleanTitle, type NewEvent, type Scalar } from "./events";
import { equalSecret, mapGithubEvent, verifySignature } from "./github";
export { EventHub } from "./hub";

const MAX_BODY = 256 * 1024;
const TYPE = /^[a-z0-9_.-]{1,40}$/;

const listSet = (value: string | undefined) => new Set((value ?? "").split(",").map((s) => s.trim()).filter(Boolean));
const stub = (env: Env) => env.HUB.get(env.HUB.idFromName("main"));
const deny = () => new Response(null, { status: 401 });

function bearer(request: Request): string {
  const header = request.headers.get("authorization") ?? "";
  return header.startsWith("Bearer ") ? header.slice(7) : "";
}

// The Monitor client can only pass a URL, so the listen token may be a query parameter.
function listenAllowed(request: Request, env: Env, url: URL): boolean {
  const supplied = bearer(request) || url.searchParams.get("token") || "";
  return equalSecret(supplied, env.OPS_LISTEN_TOKEN ?? "");
}

export function validateEmit(body: unknown): NewEvent | null {
  if (!body || typeof body !== "object") return null;
  const b = body as Record<string, unknown>;
  if (typeof b.t !== "string" || !TYPE.test(b.t) || typeof b.src !== "string" || !TYPE.test(b.src)) return null;
  const event: NewEvent = { t: b.t, src: b.src };
  if (b.n !== undefined) {
    if (typeof b.n !== "number" || !Number.isFinite(b.n)) return null;
    event.n = b.n;
  }
  if (b.title !== undefined) event.title = cleanTitle(b.title);
  if (b.by !== undefined) {
    if (typeof b.by !== "string" || b.by.length > 60) return null;
    event.by = b.by;
  }
  if (b.url !== undefined) {
    if (typeof b.url !== "string" || !/^https:\/\/\S{1,190}$/.test(b.url)) return null;
    event.url = b.url;
  }
  if (b.extra !== undefined) {
    if (!b.extra || typeof b.extra !== "object") return null;
    const entries = Object.entries(b.extra as Record<string, unknown>);
    if (entries.length > 10) return null;
    const extra: Record<string, Scalar> = {};
    for (const [k, v] of entries) {
      if (!/^[a-z0-9_]{1,24}$/.test(k) || !["string", "number", "boolean"].includes(typeof v)) return null;
      extra[k] = typeof v === "string" ? v.slice(0, 80) : (v as Scalar);
    }
    event.extra = extra;
  }
  return event;
}

async function push(env: Env, event: NewEvent, extra: { delivery?: string; rateKey?: string } = {}): Promise<Response> {
  return stub(env).fetch("https://hub/push", { method: "POST", body: JSON.stringify({ event, ...extra }) });
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/health") return new Response("ok");
    if (url.pathname === "/ws" && request.headers.get("upgrade")?.toLowerCase() === "websocket") {
      if (!listenAllowed(request, env, url)) return deny();
      const forward = new URL("https://hub/connect");
      const since = url.searchParams.get("since");
      if (since) forward.searchParams.set("since", since);
      return stub(env).fetch(new Request(forward, request));
    }
    if (url.pathname === "/github" && request.method === "POST") {
      const raw = await request.text();
      if (raw.length > MAX_BODY) return new Response(null, { status: 413 });
      if (!(await verifySignature(env.GITHUB_WEBHOOK_SECRET ?? "", raw, request.headers.get("x-hub-signature-256")))) return deny();
      const name = request.headers.get("x-github-event") ?? "";
      if (name === "ping") return new Response("pong");
      let payload: Record<string, unknown>;
      try {
        payload = JSON.parse(raw) as Record<string, unknown>;
      } catch {
        return new Response(null, { status: 400 });
      }
      const event = mapGithubEvent(name, payload, { ignoreLogins: listSet(env.OPS_IGNORE_LOGINS), ciWorkflows: listSet(env.OPS_CI_WORKFLOWS) });
      if (!event) return new Response(null, { status: 204 });
      return push(env, event, { delivery: request.headers.get("x-github-delivery") ?? undefined });
    }
    if (url.pathname === "/emit" && request.method === "POST") {
      if (!equalSecret(bearer(request), env.OPS_EMIT_TOKEN ?? "")) return deny();
      const text = await request.text();
      if (text.length > 4096) return new Response(null, { status: 413 });
      let body: unknown;
      try {
        body = JSON.parse(text);
      } catch {
        return new Response(null, { status: 400 });
      }
      const event = validateEmit(body);
      if (!event) return new Response(JSON.stringify({ error: "invalid_event" }), { status: 400, headers: { "content-type": "application/json" } });
      return push(env, event, { rateKey: "emit" });
    }
    return new Response(null, { status: 404 });
  },
} satisfies ExportedHandler<Env>;
