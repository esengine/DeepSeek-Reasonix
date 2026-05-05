/** Primary: registry.modelcontextprotocol.io. Fallback: registry.smithery.ai. Last resort: bundled MCP_CATALOG. */

import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { MCP_CATALOG } from "./catalog.js";
import type {
  CacheFile,
  CachePagination,
  RegistryEntry,
  RegistryInstall,
  RegistrySource,
} from "./registry-types.js";

export const OFFICIAL_REGISTRY_URL = "https://registry.modelcontextprotocol.io/v0/servers";
export const SMITHERY_REGISTRY_URL = "https://registry.smithery.ai/servers";
export const CACHE_TTL_MS = 24 * 60 * 60 * 1000;
export const FETCH_TIMEOUT_MS = 10_000;
export const CACHE_SCHEMA_VERSION = 2;

export function defaultCachePath(): string {
  return join(homedir(), ".reasonix", "mcp-registry-cache.json");
}

function readCache(path: string): CacheFile | null {
  try {
    const raw = readFileSync(path, "utf8");
    const parsed = JSON.parse(raw) as Partial<CacheFile>;
    if (
      parsed.schemaVersion !== CACHE_SCHEMA_VERSION ||
      typeof parsed.fetchedAt !== "number" ||
      !Array.isArray(parsed.entries) ||
      typeof parsed.pagination?.pagesLoaded !== "number"
    ) {
      return null;
    }
    return parsed as CacheFile;
  } catch {
    return null;
  }
}

function writeCache(path: string, file: CacheFile): void {
  try {
    const dir = dirname(path);
    if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
    writeFileSync(path, JSON.stringify(file, null, 2));
  } catch {
    /* cache failures are non-fatal */
  }
}

async function timeoutFetch(url: string, fetcher: typeof fetch): Promise<Response> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), FETCH_TIMEOUT_MS);
  try {
    return await fetcher(url, { signal: ctrl.signal });
  } finally {
    clearTimeout(timer);
  }
}

interface OfficialPackage {
  registryType?: string;
  identifier?: string;
  version?: string;
  transport?: { type?: string };
  environmentVariables?: Array<{ name?: string }>;
}

interface OfficialServerCore {
  name?: string;
  title?: string;
  description?: string;
  packages?: OfficialPackage[];
  remotes?: Array<{ type?: string; url?: string }>;
  websiteUrl?: string;
  icons?: Array<{ src?: string }>;
}

interface OfficialServerEntry {
  server?: OfficialServerCore;
}

interface OfficialResponse {
  servers?: OfficialServerEntry[];
  metadata?: { nextCursor?: string };
}

function normalizeOfficialPackage(pkg: OfficialPackage | undefined): RegistryInstall | undefined {
  if (!pkg) return undefined;
  const runtime = pkg.registryType === "npm" ? "npm" : pkg.registryType === "pypi" ? "pypi" : null;
  if (!runtime) return undefined;
  const t = pkg.transport?.type;
  const transport: RegistryInstall["transport"] =
    t === "sse" ? "sse" : t === "streamable-http" ? "streamable-http" : "stdio";
  const requiredEnv = (pkg.environmentVariables ?? [])
    .map((e) => e.name)
    .filter((n): n is string => typeof n === "string" && n.length > 0);
  const install: RegistryInstall = { runtime, transport };
  if (pkg.identifier) install.packageId = pkg.identifier;
  if (pkg.version) install.version = pkg.version;
  if (requiredEnv.length > 0) install.requiredEnv = requiredEnv;
  return install;
}

function normalizeOfficial(server: OfficialServerCore | undefined): RegistryEntry | null {
  if (!server?.name) return null;
  let install = normalizeOfficialPackage(server.packages?.[0]);
  if (!install && server.remotes?.[0]?.url) {
    const remote = server.remotes[0];
    const transport: RegistryInstall["transport"] =
      remote.type === "streamable-http" ? "streamable-http" : "sse";
    install = { runtime: "remote", transport, url: remote.url };
  }
  const entry: RegistryEntry = {
    name: server.name,
    title: server.title || server.name,
    description: server.description ?? "",
    source: "official",
  };
  if (install) entry.install = install;
  if (server.websiteUrl) entry.homepage = server.websiteUrl;
  const icon = server.icons?.find((i) => i.src)?.src;
  if (icon) entry.iconUrl = icon;
  return entry;
}

interface OfficialPageResult {
  entries: RegistryEntry[];
  nextCursor: string | null;
}

export async function fetchOfficialPage(
  cursor: string | null,
  fetcher: typeof fetch = globalThis.fetch,
): Promise<OfficialPageResult> {
  const url = cursor
    ? `${OFFICIAL_REGISTRY_URL}?cursor=${encodeURIComponent(cursor)}`
    : OFFICIAL_REGISTRY_URL;
  const resp = await timeoutFetch(url, fetcher);
  if (!resp.ok) throw new Error(`official registry HTTP ${resp.status}`);
  const json = (await resp.json()) as OfficialResponse;
  const entries: RegistryEntry[] = [];
  for (const e of json.servers ?? []) {
    const norm = normalizeOfficial(e.server);
    if (norm) entries.push(norm);
  }
  return { entries, nextCursor: json.metadata?.nextCursor ?? null };
}

interface SmitheryServer {
  qualifiedName?: string;
  displayName?: string;
  description?: string;
  useCount?: number;
  homepage?: string;
  iconUrl?: string;
}

interface SmitheryResponse {
  servers?: SmitheryServer[];
  pagination?: { totalPages?: number; pageSize?: number };
}

function normalizeSmithery(s: SmitheryServer): RegistryEntry | null {
  if (!s.qualifiedName) return null;
  const entry: RegistryEntry = {
    name: s.qualifiedName,
    title: s.displayName || s.qualifiedName,
    description: s.description ?? "",
    source: "smithery",
  };
  if (typeof s.useCount === "number") entry.popularity = s.useCount;
  if (s.homepage) entry.homepage = s.homepage;
  if (s.iconUrl) entry.iconUrl = s.iconUrl;
  return entry;
}

interface SmitheryConnection {
  type?: string;
  deploymentUrl?: string;
  bundleUrl?: string;
  runtime?: string;
}

interface SmitheryDetailResponse {
  qualifiedName?: string;
  remote?: boolean;
  deploymentUrl?: string | null;
  connections?: SmitheryConnection[];
}

/** Resolve a Smithery listing entry into a runnable install. http → streamable-http remote; stdio → spawn via @smithery/cli. */
export async function fetchSmitheryDetail(
  qualifiedName: string,
  fetcher: typeof fetch = globalThis.fetch,
): Promise<RegistryInstall | null> {
  const url = `${SMITHERY_REGISTRY_URL}/${encodeURIComponent(qualifiedName)}`;
  const resp = await timeoutFetch(url, fetcher);
  if (!resp.ok) return null;
  const json = (await resp.json()) as SmitheryDetailResponse;
  const conn = json.connections?.[0];
  if (!conn) return null;
  if (conn.type === "http" || conn.type === "ws") {
    const deploymentUrl = conn.deploymentUrl ?? json.deploymentUrl;
    if (!deploymentUrl) return null;
    return { runtime: "remote", transport: "streamable-http", url: deploymentUrl };
  }
  if (conn.type === "stdio") {
    return {
      runtime: "npm",
      packageId: "@smithery/cli",
      transport: "stdio",
      extraArgs: ["run", qualifiedName],
    };
  }
  return null;
}

export async function fetchSmitheryFirstPage(
  fetcher: typeof fetch = globalThis.fetch,
): Promise<RegistryEntry[]> {
  const resp = await timeoutFetch(SMITHERY_REGISTRY_URL, fetcher);
  if (!resp.ok) throw new Error(`smithery HTTP ${resp.status}`);
  const json = (await resp.json()) as SmitheryResponse;
  const entries = (json.servers ?? [])
    .map(normalizeSmithery)
    .filter((x): x is RegistryEntry => x !== null);
  if (entries.length === 0) throw new Error("smithery returned no entries");
  return entries;
}

export function fallbackFromCatalog(): RegistryEntry[] {
  return MCP_CATALOG.map((e) => ({
    name: e.name,
    title: e.name,
    description: e.summary,
    source: "local" as const,
    install: {
      runtime: "npm" as const,
      packageId: e.package,
      transport: "stdio" as const,
    },
  }));
}

export type FetchProgress = (info: {
  source: "official" | "smithery";
  page: number;
  entries: number;
}) => void;

export interface FetchOptions {
  /** Force a network refresh even when cache is fresh. */
  noCache?: boolean;
  /** Override fetch — primarily for tests. */
  fetcher?: typeof fetch;
  /** Override cache file path — primarily for tests. */
  cachePath?: string;
  /** Skip the fallback chain and force a specific source. */
  preferSource?: "official" | "smithery" | "local";
  /** Progress callback — once per fetched page. */
  onProgress?: FetchProgress;
}

export interface RegistryHandle {
  source: RegistrySource;
  /** Always present; mutated in place by loadMorePages. */
  cache: CacheFile;
  fromCache: boolean;
  fetchedAt: number;
  errors: string[];
  /** When source === "official", the path this handle persists to. Smithery + local are not persisted incrementally. */
  cachePath: string;
}

function newOfficialCache(initial: OfficialPageResult): CacheFile {
  const seen = new Map<string, RegistryEntry>();
  for (const e of initial.entries) if (!seen.has(e.name)) seen.set(e.name, e);
  return {
    schemaVersion: CACHE_SCHEMA_VERSION,
    fetchedAt: Date.now(),
    source: "official",
    entries: [...seen.values()],
    pagination: { pagesLoaded: 1, nextCursor: initial.nextCursor },
  };
}

function newStaticCache(source: RegistrySource, entries: RegistryEntry[]): CacheFile {
  return {
    schemaVersion: CACHE_SCHEMA_VERSION,
    fetchedAt: Date.now(),
    source,
    entries,
    pagination: { pagesLoaded: 1, nextCursor: null },
  };
}

/** Open the registry: returns a handle with at least one page loaded. Caller can advance via loadMorePages. */
export async function openRegistry(opts: FetchOptions = {}): Promise<RegistryHandle> {
  const fetcher = opts.fetcher ?? globalThis.fetch;
  const cachePath = opts.cachePath ?? defaultCachePath();
  const errors: string[] = [];

  if (!opts.noCache && !opts.preferSource) {
    const cached = readCache(cachePath);
    if (cached && Date.now() - cached.fetchedAt < CACHE_TTL_MS && cached.entries.length > 0) {
      return {
        source: cached.source,
        cache: cached,
        fromCache: true,
        fetchedAt: cached.fetchedAt,
        errors: [],
        cachePath,
      };
    }
  }

  const tryOfficial = async (): Promise<RegistryHandle> => {
    const first = await fetchOfficialPage(null, fetcher);
    const cache = newOfficialCache(first);
    opts.onProgress?.({ source: "official", page: 1, entries: cache.entries.length });
    writeCache(cachePath, cache);
    return {
      source: "official",
      cache,
      fromCache: false,
      fetchedAt: cache.fetchedAt,
      errors,
      cachePath,
    };
  };

  const trySmithery = async (): Promise<RegistryHandle> => {
    const entries = await fetchSmitheryFirstPage(fetcher);
    const cache = newStaticCache("smithery", entries);
    opts.onProgress?.({ source: "smithery", page: 1, entries: entries.length });
    writeCache(cachePath, cache);
    return {
      source: "smithery",
      cache,
      fromCache: false,
      fetchedAt: cache.fetchedAt,
      errors,
      cachePath,
    };
  };

  const tryLocal = (): RegistryHandle => {
    const cache = newStaticCache("local", fallbackFromCatalog());
    return {
      source: "local",
      cache,
      fromCache: false,
      fetchedAt: cache.fetchedAt,
      errors,
      cachePath,
    };
  };

  if (opts.preferSource === "local") return tryLocal();
  if (opts.preferSource === "smithery") {
    try {
      return await trySmithery();
    } catch (e) {
      errors.push(`smithery: ${(e as Error).message}`);
      return tryLocal();
    }
  }

  try {
    return await tryOfficial();
  } catch (e) {
    errors.push(`official: ${(e as Error).message}`);
  }
  try {
    return await trySmithery();
  } catch (e) {
    errors.push(`smithery: ${(e as Error).message}`);
  }

  const stale = readCache(cachePath);
  if (stale) {
    return {
      source: stale.source,
      cache: stale,
      fromCache: true,
      fetchedAt: stale.fetchedAt,
      errors,
      cachePath,
    };
  }
  return tryLocal();
}

export interface LoadMoreOptions {
  /** Number of additional pages to fetch (cap). Stops early when the source is exhausted. */
  pages?: number;
  /** Override fetch — primarily for tests. */
  fetcher?: typeof fetch;
  /** Stop early if filter() finds at least this many matching entries (across all loaded pages). */
  matchTarget?: number;
  /** Filter applied for matchTarget counting. */
  filter?: (e: RegistryEntry) => boolean;
  /** Progress callback. */
  onProgress?: FetchProgress;
}

export interface LoadMoreResult {
  pagesAdded: number;
  newEntries: number;
  exhausted: boolean;
}

/** Advance an official-source handle by fetching more pages on demand. Smithery / local handles are no-ops. */
export async function loadMorePages(
  handle: RegistryHandle,
  opts: LoadMoreOptions = {},
): Promise<LoadMoreResult> {
  if (handle.source !== "official") {
    return { pagesAdded: 0, newEntries: 0, exhausted: true };
  }
  const cache = handle.cache;
  if (cache.pagination.nextCursor === null) {
    return { pagesAdded: 0, newEntries: 0, exhausted: true };
  }

  const fetcher = opts.fetcher ?? globalThis.fetch;
  const limit = opts.pages ?? 1;
  const seen = new Set(cache.entries.map((e) => e.name));
  const matchCount = (): number => {
    if (!opts.filter) return 0;
    let n = 0;
    for (const e of cache.entries) if (opts.filter(e)) n++;
    return n;
  };

  let pagesAdded = 0;
  let newEntries = 0;
  for (let i = 0; i < limit; i++) {
    if (cache.pagination.nextCursor === null) break;
    if (opts.matchTarget !== undefined && matchCount() >= opts.matchTarget) break;
    const result = await fetchOfficialPage(cache.pagination.nextCursor, fetcher);
    for (const e of result.entries) {
      if (!seen.has(e.name)) {
        seen.add(e.name);
        cache.entries.push(e);
        newEntries++;
      }
    }
    cache.pagination.pagesLoaded += 1;
    cache.pagination.nextCursor = result.nextCursor;
    pagesAdded += 1;
    opts.onProgress?.({
      source: "official",
      page: cache.pagination.pagesLoaded,
      entries: cache.entries.length,
    });
  }

  if (pagesAdded > 0) writeCache(handle.cachePath, cache);
  return {
    pagesAdded,
    newEntries,
    exhausted: cache.pagination.nextCursor === null,
  };
}

/** Build a `--mcp`-format spec string from a registry install descriptor. */
export function specStringFor(name: string, install: RegistryInstall): string {
  const localName = name.split("/").pop() ?? name;
  const safe = localName.replace(/[^a-zA-Z0-9_-]/g, "-").replace(/^-+|-+$/g, "") || "mcp";
  if (install.runtime === "remote") {
    if (!install.url) throw new Error(`remote install for ${name} has no URL`);
    if (install.transport === "streamable-http") return `${safe}=streamable+${install.url}`;
    return `${safe}=${install.url}`;
  }
  const trail = install.extraArgs?.length ? ` ${install.extraArgs.join(" ")}` : "";
  if (install.runtime === "npm") {
    if (!install.packageId) throw new Error(`npm install for ${name} has no packageId`);
    const pinned = install.version ? `${install.packageId}@${install.version}` : install.packageId;
    return `${safe}=npx -y ${pinned}${trail}`;
  }
  if (install.runtime === "pypi") {
    if (!install.packageId) throw new Error(`pypi install for ${name} has no packageId`);
    return `${safe}=uvx ${install.packageId}${trail}`;
  }
  throw new Error(`unsupported install runtime: ${(install as RegistryInstall).runtime}`);
}

/** Re-exported for consumers that want a shape compatible with the old fetchRegistry result. */
export interface FetchResult {
  entries: RegistryEntry[];
  source: RegistrySource;
  fromCache: boolean;
  fetchedAt: number;
  errors: string[];
  /** Whether more pages are available beyond what's already loaded. */
  hasMore: boolean;
}

export function handleToFetchResult(handle: RegistryHandle): FetchResult {
  return {
    entries: handle.cache.entries,
    source: handle.source,
    fromCache: handle.fromCache,
    fetchedAt: handle.fetchedAt,
    errors: handle.errors,
    hasMore: handle.cache.pagination.nextCursor !== null,
  };
}
