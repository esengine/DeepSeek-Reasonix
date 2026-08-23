import type { SearchSource } from "./searchSources";
import { isSafeHttpUrl } from "./searchSources";

export type SearchSourceView = {
  title: string;
  href: string;
  hostname: string;
  displayUrl: string;
  canonicalUrl: string;
};

export type SearchSourcePresentation = {
  visible: SearchSourceView[];
  hiddenCount: number;
};

const REDIRECT_HOST = /^(?:page\.sm\.cn|(?:www\.)?(?:google|bing)\.com)$/;
const REDIRECT_PATH = /^\/(?:url\/?|ck\/a)$/;
const REDIRECT_PARAMS = ["url", "u", "q", "target", "dest", "destination", "redirect", "redirect_url"] as const;
const TRACKING_PARAMS = new Set(["fbclid", "gclid", "dclid", "gbraid", "wbraid", "msclkid", "mc_cid", "mc_eid", "igshid"]);

function isRedirectWrapper(parsed: URL): boolean {
  const host = parsed.hostname.toLowerCase();
  return host === "page.sm.cn" || (REDIRECT_HOST.test(host) && REDIRECT_PATH.test(parsed.pathname.toLowerCase()));
}

function unwrapRedirectUrl(parsed: URL): URL | null {
  if (!isRedirectWrapper(parsed)) return parsed;
  for (const key of REDIRECT_PARAMS) {
    const candidate = parsed.searchParams.get(key);
    if (!candidate || !isSafeHttpUrl(candidate)) continue;
    try { return new URL(candidate); } catch { /* keep looking */ }
  }
  return null;
}

function canonicalizeUrl(raw: string): URL | null {
  try {
    const parsed = new URL(raw);
    if (!isSafeHttpUrl(raw)) return null;
    const unwrapped = unwrapRedirectUrl(parsed);
    if (!unwrapped) return null;
    for (const key of [...unwrapped.searchParams.keys()]) {
      if (key.toLowerCase().startsWith("utm_") || TRACKING_PARAMS.has(key.toLowerCase())) unwrapped.searchParams.delete(key);
    }
    unwrapped.hash = "";
    return unwrapped;
  } catch { return null; }
}

function shortDisplayUrl(parsed: URL): string {
  const host = parsed.hostname.replace(/^www\./i, "");
  const path = `${parsed.pathname}${parsed.search}`;
  const display = `${host}${path === "/" ? "" : path}`;
  return display.length <= 92 ? display : `${display.slice(0, 91)}…`;
}

/** Build a safe, compact display projection without changing replay data. */
export function normalizeSearchSources(sources: SearchSource[] | undefined): SearchSourcePresentation {
  const visible: SearchSourceView[] = [];
  const seen = new Set<string>();
  let hiddenCount = 0;
  for (const source of sources ?? []) {
    const title = (source.title ?? "").trim();
    const parsed = canonicalizeUrl((source.url ?? "").trim());
    if (!title || !parsed) { hiddenCount += 1; continue; }
    const canonicalUrl = parsed.toString();
    if (seen.has(canonicalUrl)) { hiddenCount += 1; continue; }
    seen.add(canonicalUrl);
    visible.push({ title, href: canonicalUrl, hostname: parsed.hostname.replace(/^www\./i, ""), displayUrl: shortDisplayUrl(parsed), canonicalUrl });
  }
  return { visible, hiddenCount };
}
