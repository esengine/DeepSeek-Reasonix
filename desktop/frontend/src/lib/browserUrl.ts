const LAST_URL_KEY = "reasonix.browser.lastUrl";
const SESSION_URLS_KEY = "reasonix.browser.sessionUrls.v1";
/** Fresh panel starts with an empty address — no default localhost. */
export const DEFAULT_BROWSER_URL = "";

/** Normalize a user-entered address into an absolute http(s) URL. */
export function normalizeBrowserUrl(input: string): string | null {
  const raw = input.trim().replace(/\s+/g, "");
  if (!raw) return null;

  let candidate = raw;
  // Require "scheme://" so host:port values like localhost:3000 are not treated as schemes.
  if (!/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(candidate)) {
    if (candidate.startsWith("//")) {
      candidate = `http:${candidate}`;
    } else {
      candidate = `http://${candidate}`;
    }
  }

  try {
    const url = new URL(candidate);
    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
    return url.toString();
  } catch {
    return null;
  }
}

export function isLocalBrowserHost(url: string): boolean {
  if (!url.trim()) return false;
  try {
    const host = new URL(url).hostname.toLowerCase();
    return host === "localhost" || host === "127.0.0.1" || host === "0.0.0.0" || host === "[::1]" || host === "::1";
  } catch {
    return false;
  }
}

type SessionUrlMap = Record<string, string>;

function readSessionUrlMap(): SessionUrlMap {
  if (typeof window === "undefined") return {};
  try {
    const raw = window.localStorage.getItem(SESSION_URLS_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object") return {};
    const out: SessionUrlMap = {};
    for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
      if (typeof value !== "string") continue;
      const normalized = normalizeBrowserUrl(value);
      if (normalized) out[key] = normalized;
    }
    return out;
  } catch {
    return {};
  }
}

function writeSessionUrlMap(map: SessionUrlMap): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(SESSION_URLS_KEY, JSON.stringify(map));
  } catch {
    /* ignore */
  }
}

/** Load the browser URL bound to a chat/session tab. New sessions start empty. */
export function loadBrowserUrlForSession(sessionKey: string): string {
  const key = sessionKey.trim();
  if (!key) return DEFAULT_BROWSER_URL;
  const fromSession = readSessionUrlMap()[key];
  if (fromSession) return fromSession;
  return DEFAULT_BROWSER_URL;
}

/** Persist the browser URL for a chat/session tab. */
export function saveBrowserUrlForSession(sessionKey: string, url: string): void {
  const key = sessionKey.trim();
  if (!key || typeof window === "undefined") return;
  const map = readSessionUrlMap();
  const normalized = normalizeBrowserUrl(url);
  if (!normalized) {
    delete map[key];
  } else {
    map[key] = normalized;
  }
  writeSessionUrlMap(map);
}

/** Drop a closed session's browser URL. */
export function clearBrowserUrlForSession(sessionKey: string): void {
  const key = sessionKey.trim();
  if (!key || typeof window === "undefined") return;
  const map = readSessionUrlMap();
  if (!(key in map)) return;
  delete map[key];
  writeSessionUrlMap(map);
}

/** @deprecated Prefer session-scoped loadBrowserUrlForSession. */
export function loadLastBrowserUrl(): string {
  if (typeof window === "undefined") return DEFAULT_BROWSER_URL;
  try {
    const saved = window.localStorage.getItem(LAST_URL_KEY)?.trim();
    if (!saved) return DEFAULT_BROWSER_URL;
    const normalized = normalizeBrowserUrl(saved);
    // Drop the previous hard-coded default so panels open empty again.
    // TODO(browser): remove this migration cleanup after v1.x — it strips a
    // default localhost URL that was hard-coded in an earlier iteration.
    if (normalized === "http://localhost:3000/" || normalized === "http://localhost:3000") {
      window.localStorage.removeItem(LAST_URL_KEY);
      return DEFAULT_BROWSER_URL;
    }
    return normalized ?? DEFAULT_BROWSER_URL;
  } catch {
    return DEFAULT_BROWSER_URL;
  }
}

/** @deprecated Prefer session-scoped saveBrowserUrlForSession. */
export function saveLastBrowserUrl(url: string): void {
  if (typeof window === "undefined") return;
  const normalized = normalizeBrowserUrl(url);
  try {
    if (!normalized) {
      window.localStorage.removeItem(LAST_URL_KEY);
      return;
    }
    window.localStorage.setItem(LAST_URL_KEY, normalized);
  } catch {
    /* ignore */
  }
}

/** In-process cache of the URL currently loaded in the native WebView. */
let nativeBrowserUrl = "";

/** Remember the URL the native engine last reported (survives panel remount). */
export function rememberNativeBrowserUrl(url: string): void {
  const normalized = normalizeBrowserUrl(url);
  nativeBrowserUrl = normalized ?? "";
}

export function getNativeBrowserUrl(): string {
  return nativeBrowserUrl;
}

/** True when remount can Show the existing native page without Navigate. */
export function sameNativeBrowserUrl(url: string): boolean {
  const normalized = normalizeBrowserUrl(url);
  if (!normalized || !nativeBrowserUrl) return false;
  return normalized === nativeBrowserUrl;
}

export function browserReferencePath(pageUrl: string, selector?: string): string {
  const base = `browser://${pageUrl}`;
  if (!selector) return base;
  return `${base}#sel=${encodeURIComponent(selector)}`;
}

export function isBrowserReferencePath(path: string | undefined): boolean {
  return Boolean(path?.startsWith("browser://"));
}

export function browserPageUrlFromPath(path: string): string {
  if (!path.startsWith("browser://")) return path;
  const rest = path.slice("browser://".length);
  const hash = rest.indexOf("#");
  return hash >= 0 ? rest.slice(0, hash) : rest;
}
