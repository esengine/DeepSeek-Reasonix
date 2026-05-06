import { closeSync, fstatSync, openSync, readFileSync, readSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** Resolve dashboard/ across tsx-dev and tsup-bundled layouts. */
function resolveAssetDir(): string {
  const here = dirname(fileURLToPath(import.meta.url));
  // Try a few candidates; the first existing one wins.
  // - src/server/   → ../../dashboard
  // - dist/         → ./dashboard      (post-bundle, dashboard/ flat at dist root)
  // - dist/cli/     → ../dashboard
  const candidates = [
    join(here, "..", "..", "dashboard"),
    join(here, "..", "dashboard"),
    join(here, "dashboard"),
  ];
  for (const c of candidates) {
    try {
      readFileSync(join(c, "index.html"), "utf8");
      return c;
    } catch {
      /* try next */
    }
  }
  // Fall through to the most-likely-correct dev path; the read on first
  // request will throw with a useful path in the error message.
  return candidates[0]!;
}

const ASSET_DIR = resolveAssetDir();

/** mtime-keyed cache — `npm run build` invalidates without restart. */
const fileCache = new Map<string, { body: string; mtimeMs: number }>();

function loadCachedFile(path: string): string {
  // Open once and reuse the fd so the mtime check and the read bind to
  // the same inode — closes the stat→read TOCTOU race.
  const fd = openSync(path, "r");
  try {
    const stat = fstatSync(fd);
    const cached = fileCache.get(path);
    if (cached && cached.mtimeMs === stat.mtimeMs) return cached.body;
    const buf = Buffer.alloc(stat.size);
    let read = 0;
    while (read < stat.size) {
      const n = readSync(fd, buf, read, stat.size - read, read);
      if (n <= 0) break;
      read += n;
    }
    const body = buf.toString("utf8", 0, read);
    fileCache.set(path, { body, mtimeMs: stat.mtimeMs });
    return body;
  } finally {
    closeSync(fd);
  }
}

function loadIndexTemplate(): string {
  return loadCachedFile(join(ASSET_DIR, "index.html"));
}

function loadApp(): string {
  return loadCachedFile(join(ASSET_DIR, "dist", "app.js"));
}

function loadAppMap(): string | null {
  try {
    return loadCachedFile(join(ASSET_DIR, "dist", "app.js.map"));
  } catch {
    return null;
  }
}

function loadCss(): string {
  return loadCachedFile(join(ASSET_DIR, "app.css"));
}

/** Token HTML-attribute-escaped in case a future mint produces non-hex bytes. */
export function renderIndexHtml(token: string, mode: "standalone" | "attached"): string {
  const tpl = loadIndexTemplate();
  const safeToken = token.replace(/[^a-zA-Z0-9]/g, "");
  // String.replace(string, replacement) only swaps the FIRST match. The
  // template has __REASONIX_TOKEN__ in three places (meta + css href +
  // script src) — without `replaceAll` only the meta tag gets the real
  // token, the asset URLs keep the placeholder and the browser hits a
  // 401 on every asset fetch. Same trap for __REASONIX_MODE__ if it
  // ever appears more than once.
  return tpl.replaceAll("__REASONIX_TOKEN__", safeToken).replaceAll("__REASONIX_MODE__", mode);
}

/** Vendor CSS the bundle pulls from npm and the build script copies into `dashboard/dist/`. */
const VENDOR_CSS_NAMES = new Set(["vendor-hljs.css", "vendor-uplot.css"]);

function loadVendorCss(name: string): string {
  return loadCachedFile(join(ASSET_DIR, "dist", name));
}

export function serveAsset(name: string): { body: string; contentType: string } | null {
  if (name === "app.js") {
    return { body: loadApp(), contentType: "application/javascript; charset=utf-8" };
  }
  if (name === "app.js.map") {
    const body = loadAppMap();
    return body == null ? null : { body, contentType: "application/json; charset=utf-8" };
  }
  if (name === "app.css") {
    return { body: loadCss(), contentType: "text/css; charset=utf-8" };
  }
  if (VENDOR_CSS_NAMES.has(name)) {
    return { body: loadVendorCss(name), contentType: "text/css; charset=utf-8" };
  }
  return null;
}
