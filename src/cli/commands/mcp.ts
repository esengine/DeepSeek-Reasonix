import { readConfig, writeConfig } from "../../config.js";
import { MCP_CATALOG, mcpCommandFor } from "../../mcp/catalog.js";
import {
  type FetchProgress,
  fetchSmitheryDetail,
  handleToFetchResult,
  loadMorePages,
  openRegistry,
  specStringFor,
} from "../../mcp/registry-fetch.js";
import type { RegistryEntry } from "../../mcp/registry-types.js";

const DEFAULT_LIST_LIMIT = 30;
/** Soft cap on how far `search` walks the registry on first run. */
const SEARCH_PAGE_CAP = 20;
/** Soft cap on how far `install` walks looking for a name. */
const INSTALL_PAGE_CAP = 30;

const progressToStderr: FetchProgress = ({ source, page, entries }) => {
  if (page === 1 || page % 5 === 0) {
    process.stderr.write(`\r▸ fetching ${source} registry · page ${page} · ${entries} entries`);
  }
};

function finishProgressLine(): void {
  if (process.stderr.isTTY) process.stderr.write("\r\x1b[K");
  else process.stderr.write("\n");
}

export interface McpListOptions {
  json?: boolean;
  /** Skip network — only show the bundled MCP_CATALOG entries. */
  local?: boolean;
  /** Bypass cache TTL. */
  refresh?: boolean;
  /** How many entries to show. Default 30. */
  limit?: number;
  /** Eagerly load this many pages before showing. Default 1. */
  pages?: number;
  /** Walk all pages of the registry (slow on first run). */
  all?: boolean;
}

export interface McpSearchOptions {
  json?: boolean;
  refresh?: boolean;
  limit?: number;
  /** Cap how many pages to walk while searching. Default 20. */
  maxPages?: number;
}

export interface McpInstallOptions {
  refresh?: boolean;
  /** Cap how many pages to walk while looking for the name. Default 30. */
  maxPages?: number;
}

function rankEntries(entries: RegistryEntry[]): RegistryEntry[] {
  return [...entries].sort((a, b) => {
    const ap = a.popularity ?? -1;
    const bp = b.popularity ?? -1;
    if (ap !== bp) return bp - ap;
    return a.name.localeCompare(b.name);
  });
}

function pad(s: string, width: number): string {
  return s.length >= width ? s : s + " ".repeat(width - s.length);
}

function fmtAge(ms: number): string {
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s ago`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`;
  return `${Math.floor(sec / 86400)}d ago`;
}

function printEntry(e: RegistryEntry, indent = "  "): void {
  const tag =
    e.source === "official" ? "[official]" : e.source === "smithery" ? "[smithery]" : "[local]";
  const pop = e.popularity !== undefined ? ` · ${e.popularity.toLocaleString()} uses` : "";
  console.log(`${indent}${pad(e.name, 36)} ${tag}${pop}`);
  if (e.description) console.log(`${indent}    ${e.description}`);
  if (e.install?.requiredEnv?.length) {
    console.log(`${indent}    needs: ${e.install.requiredEnv.join(", ")}`);
  } else if (!e.install) {
    console.log(`${indent}    (smithery listing — install detail fetched lazily on install)`);
  }
}

export async function mcpListCommand(opts: McpListOptions = {}): Promise<void> {
  if (opts.local) {
    if (opts.json) {
      console.log(JSON.stringify(MCP_CATALOG, null, 2));
      return;
    }
    console.log("Bundled MCP servers (offline catalog):");
    console.log("");
    for (const entry of MCP_CATALOG) {
      console.log(`  ${pad(entry.name, 12)} ${entry.summary}`);
      console.log(`               ${mcpCommandFor(entry)}`);
      if (entry.note) console.log(`               · ${entry.note}`);
      console.log("");
    }
    return;
  }

  const handle = await openRegistry({ noCache: opts.refresh, onProgress: progressToStderr });
  const wantedPages = opts.all ? Number.POSITIVE_INFINITY : (opts.pages ?? 1);
  const additional = Math.max(0, wantedPages - handle.cache.pagination.pagesLoaded);
  if (additional > 0) {
    await loadMorePages(handle, {
      pages: additional,
      onProgress: progressToStderr,
    });
  }
  finishProgressLine();

  const result = handleToFetchResult(handle);
  const ranked = rankEntries(result.entries);
  const limit = opts.limit ?? DEFAULT_LIST_LIMIT;
  const shown = ranked.slice(0, limit);

  if (opts.json) {
    console.log(
      JSON.stringify(
        {
          source: result.source,
          fromCache: result.fromCache,
          fetchedAt: result.fetchedAt,
          loaded: result.entries.length,
          hasMore: result.hasMore,
          entries: shown,
        },
        null,
        2,
      ),
    );
    return;
  }

  const ageStr = result.fromCache
    ? `cached, ${fmtAge(Date.now() - result.fetchedAt)}`
    : "just fetched";
  const moreStr = result.hasMore ? "more available" : "all loaded";
  console.log(
    `MCP servers from ${result.source} registry (${result.entries.length} loaded, ${moreStr}, ${ageStr}):`,
  );
  if (result.errors.length > 0) {
    for (const e of result.errors) console.error(`  warn: ${e}`);
  }
  console.log("");
  for (const e of shown) printEntry(e);
  if (ranked.length > limit) {
    console.log(
      `  … ${ranked.length - limit} more loaded — use \`reasonix mcp search <query>\` to filter`,
    );
  }
  if (result.hasMore) {
    console.log("  ▸ more pages available — `reasonix mcp list --pages <n>` or --all");
  }
  console.log("");
  console.log("Install:  reasonix mcp install <name>");
}

function matchFilter(query: string): (e: RegistryEntry) => boolean {
  const q = query.toLowerCase();
  return (e) => `${e.name} ${e.title} ${e.description}`.toLowerCase().includes(q);
}

export async function mcpSearchCommand(query: string, opts: McpSearchOptions = {}): Promise<void> {
  const q = query.trim();
  if (!q) {
    console.error("usage: reasonix mcp search <query>");
    process.exit(1);
  }
  const handle = await openRegistry({ noCache: opts.refresh, onProgress: progressToStderr });
  const filter = matchFilter(q);
  const limit = opts.limit ?? DEFAULT_LIST_LIMIT;
  const cap = opts.maxPages ?? SEARCH_PAGE_CAP;

  await loadMorePages(handle, {
    pages: Math.max(0, cap - handle.cache.pagination.pagesLoaded),
    matchTarget: limit,
    filter,
    onProgress: progressToStderr,
  });
  finishProgressLine();

  const result = handleToFetchResult(handle);
  const matches = rankEntries(result.entries.filter(filter));
  const shown = matches.slice(0, limit);

  if (opts.json) {
    console.log(
      JSON.stringify(
        {
          query: q,
          source: result.source,
          loaded: result.entries.length,
          hasMore: result.hasMore,
          matches: matches.length,
          entries: shown,
        },
        null,
        2,
      ),
    );
    return;
  }

  if (shown.length === 0) {
    console.log(
      `No matches for "${q}" across ${result.entries.length} loaded entries (${result.source}${
        result.hasMore ? ", more pages exist — try --refresh or `mcp list --all`" : ""
      }).`,
    );
    return;
  }
  console.log(
    `${matches.length} match(es) for "${q}" in ${result.source} registry (${result.entries.length} entries scanned):`,
  );
  console.log("");
  for (const e of shown) printEntry(e);
  if (matches.length > limit) console.log(`  … ${matches.length - limit} more matches`);
}

function findEntry(entries: RegistryEntry[], name: string): RegistryEntry | null {
  const exact = entries.find((e) => e.name === name);
  if (exact) return exact;
  const lower = name.toLowerCase();
  const ci = entries.find((e) => e.name.toLowerCase() === lower);
  if (ci) return ci;
  const tail = entries.find((e) => e.name.toLowerCase().endsWith(`/${lower}`));
  if (tail) return tail;
  return null;
}

export async function mcpInstallCommand(name: string, opts: McpInstallOptions = {}): Promise<void> {
  const target = name.trim();
  if (!target) {
    console.error("usage: reasonix mcp install <name>");
    process.exit(1);
  }

  const handle = await openRegistry({ noCache: opts.refresh, onProgress: progressToStderr });
  const lower = target.toLowerCase();
  const filter = (e: RegistryEntry): boolean => {
    const n = e.name.toLowerCase();
    return n === lower || n.endsWith(`/${lower}`) || n.includes(lower);
  };
  const cap = opts.maxPages ?? INSTALL_PAGE_CAP;

  await loadMorePages(handle, {
    pages: Math.max(0, cap - handle.cache.pagination.pagesLoaded),
    matchTarget: 1,
    filter,
    onProgress: progressToStderr,
  });
  finishProgressLine();

  const entry = findEntry(handle.cache.entries, target);
  if (!entry) {
    console.error(
      `No MCP server named "${target}" found after walking ${handle.cache.pagination.pagesLoaded} page(s) of the ${handle.source} registry.`,
    );
    if (handle.cache.pagination.nextCursor !== null) {
      console.error(`Try: reasonix mcp install ${target} --max-pages 100`);
    }
    process.exit(1);
  }

  if (!entry.install && entry.source === "smithery") {
    process.stderr.write(`▸ fetching smithery install detail for ${entry.name}…\n`);
    const fetched = await fetchSmitheryDetail(entry.name);
    if (fetched) entry.install = fetched;
  }

  if (!entry.install) {
    console.error(
      `Could not derive install metadata for "${entry.name}" — try \`npx -y @smithery/cli install ${entry.name}\` directly.`,
    );
    process.exit(1);
  }

  let spec: string;
  try {
    spec = specStringFor(entry.name, entry.install);
  } catch (err) {
    console.error(`Cannot build install spec for ${entry.name}: ${(err as Error).message}`);
    process.exit(1);
  }

  const cfg = readConfig();
  const existing = cfg.mcp ?? [];
  if (existing.includes(spec)) {
    console.log(`Already installed: ${spec}`);
    return;
  }
  const next = { ...cfg, mcp: [...existing, spec] };
  writeConfig(next);

  console.log(`Installed: ${entry.name}`);
  console.log(`  spec:    ${spec}`);
  if (entry.install.requiredEnv?.length) {
    console.log(
      `  needs:   ${entry.install.requiredEnv.join(", ")}  (set these in your env before next chat)`,
    );
  }
  console.log("");
  console.log(
    "Use it:  reasonix chat   (or `reasonix code`) — the server will be bridged automatically.",
  );
}
