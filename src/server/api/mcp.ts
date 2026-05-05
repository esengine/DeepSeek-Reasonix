/** Spec mutations don't auto-reload — adding a server shifts the system prefix and zeroes the next cache hit. */

import { readConfig, writeConfig } from "../../config.js";
import {
  fetchSmitheryDetail,
  handleToFetchResult,
  loadMorePages,
  openRegistry,
  specStringFor,
} from "../../mcp/registry-fetch.js";
import type { RegistryEntry } from "../../mcp/registry-types.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface SpecBody {
  spec?: unknown;
}
interface InvokeBody {
  server?: unknown;
  tool?: unknown;
  args?: unknown;
}
interface InstallBody {
  name?: unknown;
  maxPages?: unknown;
}

function parseBody<T>(raw: string): T {
  if (!raw) return {} as T;
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? (parsed as T) : ({} as T);
  } catch {
    return {} as T;
  }
}

function clampInt(
  raw: string | null | undefined,
  min: number,
  max: number,
  fallback: number,
): number {
  if (raw == null) return fallback;
  const n = Number.parseInt(raw, 10);
  if (!Number.isFinite(n)) return fallback;
  return Math.max(min, Math.min(max, n));
}

function findRegistryEntry(entries: RegistryEntry[], name: string): RegistryEntry | null {
  const exact = entries.find((e) => e.name === name);
  if (exact) return exact;
  const lower = name.toLowerCase();
  const ci = entries.find((e) => e.name.toLowerCase() === lower);
  if (ci) return ci;
  const tail = entries.find((e) => e.name.toLowerCase().endsWith(`/${lower}`));
  if (tail) return tail;
  return null;
}

export async function handleMcp(
  method: string,
  rest: string[],
  body: string,
  ctx: DashboardContext,
  query: URLSearchParams = new URLSearchParams(),
): Promise<ApiResult> {
  // Bridged-server view (live).
  if (method === "GET" && rest.length === 0) {
    const servers = (ctx.mcpServers ?? []).map((s) => ({
      label: s.label,
      spec: s.spec,
      toolCount: s.toolCount,
      protocolVersion: s.report.protocolVersion,
      serverInfo: s.report.serverInfo,
      capabilities: s.report.capabilities,
      tools: s.report.tools.supported ? s.report.tools.items : [],
      resources: s.report.resources.supported ? s.report.resources.items : [],
      prompts: s.report.prompts.supported ? s.report.prompts.items : [],
      instructions: s.report.instructions ?? null,
    }));
    return {
      status: 200,
      body: {
        servers,
        canHotReload: Boolean(ctx.reloadMcp),
        canInvoke: Boolean(ctx.invokeMcpTool),
      },
    };
  }

  // Persisted spec list — what config.mcp[] holds. May differ from
  // bridged set (a recent edit hasn't been reloaded yet).
  if (method === "GET" && rest[0] === "specs") {
    const cfg = readConfig(ctx.configPath);
    return { status: 200, body: { specs: cfg.mcp ?? [] } };
  }

  if (method === "POST" && rest[0] === "specs") {
    const { spec } = parseBody<SpecBody>(body);
    if (typeof spec !== "string" || !spec.trim()) {
      return { status: 400, body: { error: "spec (non-empty string) required" } };
    }
    const cfg = readConfig(ctx.configPath);
    const list = cfg.mcp ?? [];
    if (list.includes(spec)) {
      return { status: 200, body: { added: false, alreadyPresent: true } };
    }
    cfg.mcp = [...list, spec.trim()];
    writeConfig(cfg, ctx.configPath);
    ctx.audit?.({ ts: Date.now(), action: "add-mcp-spec", payload: { spec } });
    let bridged = false;
    if (ctx.reloadMcp) {
      try {
        await ctx.reloadMcp();
        bridged = true;
      } catch {
        /* fall through to requiresRestart */
      }
    }
    return { status: 200, body: { added: true, requiresRestart: !bridged, bridged } };
  }

  if (method === "DELETE" && rest[0] === "specs") {
    const { spec } = parseBody<SpecBody>(body);
    if (typeof spec !== "string") {
      return { status: 400, body: { error: "spec (string) required" } };
    }
    const cfg = readConfig(ctx.configPath);
    const list = cfg.mcp ?? [];
    if (!list.includes(spec)) {
      return { status: 200, body: { removed: false } };
    }
    cfg.mcp = list.filter((s) => s !== spec);
    writeConfig(cfg, ctx.configPath);
    ctx.audit?.({ ts: Date.now(), action: "remove-mcp-spec", payload: { spec } });
    let bridged = false;
    if (ctx.reloadMcp) {
      try {
        await ctx.reloadMcp();
        bridged = true;
      } catch {
        /* fall through */
      }
    }
    return { status: 200, body: { removed: true, requiresRestart: !bridged, bridged } };
  }

  if (method === "POST" && rest[0] === "reload") {
    if (!ctx.reloadMcp) {
      return {
        status: 503,
        body: {
          error:
            "live MCP reload not wired in this session — restart `reasonix code` to apply spec edits.",
        },
      };
    }
    const count = await ctx.reloadMcp();
    return { status: 200, body: { reloaded: true, count } };
  }

  // Marketplace registry — open + lazy-paginate. Query: ?pages=N&q=&maxPages=&limit=&refresh=1
  // Caps are generous on purpose: registry walks are bounded by the upstream
  // 24h cache, and an HTTP response of ~1000 entries is still under 1 MB.
  // The dashboard's "load more" click bumps these by 50 entries / 3 pages
  // each time, so without these ceilings users would hit a frustrating wall
  // after a few clicks.
  if (method === "GET" && rest[0] === "registry" && (rest[1] === undefined || rest[1] === "list")) {
    const pagesWanted = clampInt(query.get("pages"), 1, 200, 1);
    const maxPages = clampInt(query.get("maxPages"), 1, 200, 20);
    const limit = clampInt(query.get("limit"), 1, 1000, 30);
    const refreshRaw = query.get("refresh");
    const refresh = refreshRaw === "1" || refreshRaw === "true";
    const q = (query.get("q") ?? "").trim().toLowerCase();

    try {
      const handle = await openRegistry({ noCache: refresh });
      const target = q ? maxPages : pagesWanted;
      const additional = Math.max(0, target - handle.cache.pagination.pagesLoaded);
      if (additional > 0) {
        await loadMorePages(handle, {
          pages: additional,
          matchTarget: q ? limit : undefined,
          filter: q
            ? (e) => `${e.name} ${e.title} ${e.description}`.toLowerCase().includes(q)
            : undefined,
        });
      }
      const result = handleToFetchResult(handle);
      const matched = q
        ? result.entries.filter((e) =>
            `${e.name} ${e.title} ${e.description}`.toLowerCase().includes(q),
          )
        : result.entries;
      const ranked = matched.slice().sort((a, b) => {
        const ap = a.popularity ?? -1;
        const bp = b.popularity ?? -1;
        if (ap !== bp) return bp - ap;
        return a.name.localeCompare(b.name);
      });
      return {
        status: 200,
        body: {
          source: result.source,
          fromCache: result.fromCache,
          fetchedAt: result.fetchedAt,
          loaded: result.entries.length,
          hasMore: result.hasMore,
          matched: matched.length,
          entries: ranked.slice(0, limit),
          errors: result.errors,
        },
      };
    } catch (err) {
      return { status: 500, body: { error: (err as Error).message } };
    }
  }

  if (method === "POST" && rest[0] === "registry" && rest[1] === "install") {
    const { name, maxPages } = parseBody<InstallBody>(body);
    if (typeof name !== "string" || !name.trim()) {
      return { status: 400, body: { error: "name (string) required" } };
    }
    const cap = typeof maxPages === "number" && maxPages > 0 ? maxPages : 30;
    try {
      const handle = await openRegistry({});
      const target = name.trim();
      const lower = target.toLowerCase();
      const filter = (e: RegistryEntry): boolean => {
        const n = e.name.toLowerCase();
        return n === lower || n.endsWith(`/${lower}`) || n.includes(lower);
      };
      const additional = Math.max(0, cap - handle.cache.pagination.pagesLoaded);
      if (additional > 0) {
        await loadMorePages(handle, { pages: additional, matchTarget: 1, filter });
      }
      const entry = findRegistryEntry(handle.cache.entries, target);
      if (!entry) {
        return {
          status: 404,
          body: {
            error: `no MCP server named "${target}" found in ${handle.cache.pagination.pagesLoaded} page(s)`,
          },
        };
      }
      if (!entry.install && entry.source === "smithery") {
        const fetched = await fetchSmitheryDetail(entry.name);
        if (fetched) entry.install = fetched;
      }
      if (!entry.install) {
        return {
          status: 422,
          body: {
            error: `Could not derive install metadata for ${entry.name}`,
            hint: `npx -y @smithery/cli install ${entry.name}`,
          },
        };
      }
      const spec = specStringFor(entry.name, entry.install);
      const cfg = readConfig(ctx.configPath);
      const existing = cfg.mcp ?? [];
      if (existing.includes(spec)) {
        return { status: 200, body: { added: false, alreadyPresent: true, spec, entry } };
      }
      cfg.mcp = [...existing, spec];
      writeConfig(cfg, ctx.configPath);
      ctx.audit?.({
        ts: Date.now(),
        action: "install-mcp-from-registry",
        payload: { name: entry.name, spec },
      });
      let bridged = false;
      let bridgeError: string | undefined;
      if (ctx.reloadMcp) {
        try {
          await ctx.reloadMcp();
          bridged = true;
        } catch (err) {
          bridgeError = (err as Error).message;
        }
      }
      return {
        status: 200,
        body: {
          added: true,
          requiresRestart: !ctx.reloadMcp || !!bridgeError,
          bridged,
          bridgeError,
          spec,
          entry,
        },
      };
    } catch (err) {
      return { status: 500, body: { error: (err as Error).message } };
    }
  }

  if (method === "POST" && rest[0] === "invoke") {
    if (!ctx.invokeMcpTool) {
      return {
        status: 503,
        body: { error: "MCP invocation requires an attached session." },
      };
    }
    const { server, tool, args } = parseBody<InvokeBody>(body);
    if (typeof server !== "string" || typeof tool !== "string") {
      return { status: 400, body: { error: "server + tool (strings) required" } };
    }
    try {
      const result = await ctx.invokeMcpTool(
        server,
        tool,
        typeof args === "object" && args !== null ? (args as Record<string, unknown>) : {},
      );
      return { status: 200, body: { result } };
    } catch (err) {
      return { status: 500, body: { error: (err as Error).message } };
    }
  }

  return { status: 405, body: { error: `method ${method} not supported on this path` } };
}
