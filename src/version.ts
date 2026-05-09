/** VERSION sourced from package.json so it never drifts from npm; latest-check returns null on any failure. */

import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** npm registry endpoint for the `latest` dist-tag of this package. */
const REGISTRY_URL = "https://registry.npmjs.org/reasonix/latest";

/** TTL for the on-disk cache entry. 24h keeps noise low; users who
 * want a fresh check can run `reasonix update` which passes
 * `force: true`. */
export const LATEST_CACHE_TTL_MS = 24 * 60 * 60 * 1000;

/** Network timeout. Short — we never block the UI waiting on this. */
export const LATEST_FETCH_TIMEOUT_MS = 2_000;

/** `name === "reasonix"` guard avoids picking up an outer package.json when loaded as a dep. */
function readPackageVersion(): string {
  try {
    let dir = dirname(fileURLToPath(import.meta.url));
    for (let i = 0; i < 6; i++) {
      const p = join(dir, "package.json");
      if (existsSync(p)) {
        const pkg = JSON.parse(readFileSync(p, "utf8"));
        if (pkg?.name === "reasonix" && typeof pkg.version === "string") {
          return pkg.version;
        }
      }
      const parent = dirname(dir);
      if (parent === dir) break;
      dir = parent;
    }
  } catch {
    /* fall through to fallback */
  }
  return "0.0.0-dev";
}

export const VERSION: string = readPackageVersion();

interface VersionCacheEntry {
  version: string;
  /** Epoch millis the entry was written. Drives TTL comparisons. */
  checkedAt: number;
}

function cachePath(homeDirOverride?: string): string {
  return join(homeDirOverride ?? homedir(), ".reasonix", "version-cache.json");
}

function readCache(homeDirOverride?: string): VersionCacheEntry | null {
  try {
    const raw = readFileSync(cachePath(homeDirOverride), "utf8");
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed.version === "string" && typeof parsed.checkedAt === "number") {
      return parsed;
    }
  } catch {
    /* missing or malformed → no cached entry */
  }
  return null;
}

function writeCache(entry: VersionCacheEntry, homeDirOverride?: string): void {
  try {
    const p = cachePath(homeDirOverride);
    mkdirSync(dirname(p), { recursive: true });
    writeFileSync(p, JSON.stringify(entry), "utf8");
  } catch {
    /* cache is best-effort — a failed write just means we'll re-fetch
     * next launch. No reason to surface this to the user. */
  }
}

export interface GetLatestVersionOptions {
  /** Ignore the cached entry and always fetch fresh. Used by `reasonix update`. */
  force?: boolean;
  /** Registry URL override (tests). */
  registryUrl?: string;
  /** Home-directory override (tests). */
  homeDir?: string;
  /** Fetch implementation override (tests). Defaults to `globalThis.fetch`. */
  fetchImpl?: typeof fetch;
  /** TTL override (tests). */
  ttlMs?: number;
  /** Network timeout override (tests). */
  timeoutMs?: number;
}

/** Returns null on failure; cache only writes on success so bad responses can't poison it. */
export async function getLatestVersion(opts: GetLatestVersionOptions = {}): Promise<string | null> {
  const ttl = opts.ttlMs ?? LATEST_CACHE_TTL_MS;
  if (!opts.force) {
    const cached = readCache(opts.homeDir);
    if (cached && Date.now() - cached.checkedAt < ttl) return cached.version;
  }

  const fetchImpl = opts.fetchImpl ?? globalThis.fetch;
  if (!fetchImpl) return null;
  const url = opts.registryUrl ?? REGISTRY_URL;
  const timeout = opts.timeoutMs ?? LATEST_FETCH_TIMEOUT_MS;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);
  try {
    const res = await fetchImpl(url, {
      signal: controller.signal,
      headers: { accept: "application/json" },
    });
    if (!res.ok) return null;
    const body = (await res.json()) as { version?: unknown };
    if (typeof body.version !== "string") return null;
    writeCache({ version: body.version, checkedAt: Date.now() }, opts.homeDir);
    return body.version;
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

/** Pre-release with same core sorts BELOW the bare version — matches npm `latest` dist-tag semantics. */
export function compareVersions(a: string, b: string): number {
  const [aCore = "0", aPre = ""] = a.split("-", 2);
  const [bCore = "0", bPre = ""] = b.split("-", 2);
  const aParts = aCore.split(".").map((p) => Number.parseInt(p, 10) || 0);
  const bParts = bCore.split(".").map((p) => Number.parseInt(p, 10) || 0);
  for (let i = 0; i < 3; i++) {
    const diff = (aParts[i] ?? 0) - (bParts[i] ?? 0);
    if (diff !== 0) return diff;
  }
  if (!aPre && !bPre) return 0;
  if (!aPre) return 1;
  if (!bPre) return -1;
  return aPre < bPre ? -1 : aPre > bPre ? 1 : 0;
}

export type InstallSource = "npm" | "bun" | "pnpm" | "yarn" | "npx" | "unknown";

/** Each manager owns a unique global path segment, so argv[1] tells us who installed us. */
export function detectInstallSource(bin?: string): InstallSource {
  const raw = bin ?? process.argv[1] ?? "";
  if (!raw) return "unknown";
  const norm = raw.replace(/\\/g, "/").toLowerCase();
  if (/\/_npx\//.test(norm)) return "npx";
  if (/\/\.pnpm\//.test(norm) && /dlx/i.test(norm)) return "npx";
  const ua = (process.env.npm_config_user_agent ?? "").toLowerCase();
  if (ua.includes("npx/")) return "npx";
  if (/\/\.bun\//.test(norm) || /\/bun\/install\//.test(norm)) return "bun";
  if (/\/pnpm\/global\//.test(norm) || /\/pnpm\/[^/]+\/node_modules\//.test(norm)) return "pnpm";
  if (/\/yarn\/global\//.test(norm) || /\/\.yarn\/global\//.test(norm)) return "yarn";
  if (/\/node_modules\/reasonix(\b|\/)/.test(norm)) return "npm";
  return "unknown";
}

/** Returns null when no path is given. Callers must check installSource first. */
export function isNpxInstall(): boolean {
  return detectInstallSource() === "npx";
}

/** Pin npm to the install location via --prefix so `nvm use` doesn't redirect the install elsewhere. */
export function detectNpmInstallPrefix(bin?: string): string | null {
  const raw = bin ?? process.argv[1] ?? "";
  if (!raw) return null;
  const norm = raw.replace(/\\/g, "/");
  const posix = norm.match(/^(.+?)\/lib\/node_modules\/reasonix(?:\/|$)/i);
  if (posix) return posix[1] ?? null;
  const win = norm.match(/^(.+?)\/node_modules\/reasonix(?:\/|$)/i);
  if (win) return win[1] ?? null;
  return null;
}
