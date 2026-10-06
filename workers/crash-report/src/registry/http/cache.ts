import type { Context, MiddlewareHandler } from "hono";
import type { z } from "zod";
import type { AppEnv } from "../env";

export const PUBLIC_CACHE_CONTROL = "public, max-age=60, stale-while-revalidate=300";
export const DETAIL_CACHE_CONTROL = "public, max-age=60, stale-while-revalidate=30";
const NO_STORE = "no-store";
const PRIVATE_NO_STORE = "private, no-store";

// A Worker's own responses never reach the edge cache by header alone; only an
// explicit caches.default entry is shared across clients.
function edgeCache(): Cache | undefined {
  return typeof caches === "undefined" ? undefined : (caches as unknown as { default?: Cache }).default;
}

async function etagOf(body: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(body));
  const hex = [...new Uint8Array(digest).slice(0, 16)].map((b) => b.toString(16).padStart(2, "0")).join("");
  return `W/"${hex}"`;
}

function matches(ifNoneMatch: string | undefined, etag: string): boolean {
  if (!ifNoneMatch) return false;
  if (ifNoneMatch.trim() === "*") return true;
  const bare = (t: string) => t.trim().replace(/^W\//, "");
  return ifNoneMatch.split(",").some((t) => bare(t) === bare(etag));
}

function respond(c: Context<AppEnv>, body: string, contentType: string, etag: string, policy: string): Response {
  const headers = { "Cache-Control": policy, ETag: etag };
  if (matches(c.req.header("if-none-match"), etag)) return new Response(null, { status: 304, headers });
  return new Response(body, { status: 200, headers: { ...headers, "Content-Type": contentType } });
}

// One entry per distinct meaning: only validated parameters, in fixed order,
// with q folded to lower case. The path comes from the matched route and its
// decoded parameters, so percent-encoded spellings share one entry. Unknown parameters and ordering never split it.
function cacheKey(c: Context<AppEnv>, schema: z.ZodTypeAny): Request | undefined {
  const parsed = schema.safeParse(Object.fromEntries(new URL(c.req.url).searchParams));
  if (!parsed.success) return undefined;
  const data = parsed.data as Record<string, unknown>;
  const query = new URLSearchParams();
  for (const name of Object.keys(data).sort()) {
    const value = data[name];
    if (value !== undefined) query.set(name, name === "q" ? String(value).toLowerCase() : String(value));
  }
  const params = c.req.param() as Record<string, string>;
  const path = c.req.routePath.replace(/:(\w+)/g, (_m, name: string) => encodeURIComponent(params[name] ?? "")).replace(/(.)\/$/, "$1");
  return new Request(`${new URL(c.req.url).origin}${path}?${query}`, { method: "GET" });
}

// Identity-free reads: one body for every caller, so it may be shared. A request
// carrying credentials skips the shared cache entirely. Non-200 is never cached.
export function publicRead(schema: z.ZodTypeAny, policy: string = PUBLIC_CACHE_CONTROL): MiddlewareHandler<AppEnv> {
  return async (c, next) => {
    if (c.req.header("authorization") || c.req.header("cookie")) {
      await next();
      c.res.headers.set("Cache-Control", PRIVATE_NO_STORE);
      return;
    }
    const cache = edgeCache();
    const key = cache ? cacheKey(c, schema) : undefined;
    const hit = cache && key ? await cache.match(key) : undefined;
    const hitTag = hit?.headers.get("etag");
    if (hit && hitTag) {
      c.res = respond(c, await hit.text(), hit.headers.get("content-type") ?? "application/json", hitTag, policy);
      return;
    }
    await next();
    if (c.res.status !== 200) {
      c.res.headers.set("Cache-Control", NO_STORE);
      return;
    }
    const body = await c.res.clone().text();
    const type = c.res.headers.get("content-type") ?? "application/json";
    const etag = await etagOf(body);
    if (cache && key) {
      const entry = new Response(body, { headers: { "Content-Type": type, ETag: etag, "Cache-Control": policy } });
      const write = cache.put(key, entry).catch(() => undefined);
      try {
        c.executionCtx.waitUntil(write);
      } catch {
        await write;
      }
    }
    c.res = respond(c, body, type, etag, policy);
  };
}

// Anything that did not opt in to sharing (writes, admin, me, health, errors)
// must not be stored by a browser or an intermediary.
export const noStoreByDefault: MiddlewareHandler<AppEnv> = async (c, next) => {
  await next();
  if (!c.res.headers.has("Cache-Control")) c.res.headers.set("Cache-Control", NO_STORE);
};
