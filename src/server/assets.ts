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

/** mtime-keyed cache for text files — `npm run build` invalidates without restart. */
const textCache = new Map<string, { body: string; mtimeMs: number }>();

/** mtime-keyed cache for binary files (fonts, images). */
const binaryCache = new Map<string, { body: Buffer; mtimeMs: number }>();

function loadCachedText(path: string): string {
  const fd = openSync(path, "r");
  try {
    const stat = fstatSync(fd);
    const cached = textCache.get(path);
    if (cached && cached.mtimeMs === stat.mtimeMs) return cached.body;
    const buf = Buffer.alloc(stat.size);
    let read = 0;
    while (read < stat.size) {
      const n = readSync(fd, buf, read, stat.size - read, read);
      if (n <= 0) break;
      read += n;
    }
    const body = buf.toString("utf8", 0, read);
    textCache.set(path, { body, mtimeMs: stat.mtimeMs });
    return body;
  } finally {
    closeSync(fd);
  }
}

function loadCachedBinary(path: string): Buffer {
  const fd = openSync(path, "r");
  try {
    const stat = fstatSync(fd);
    const cached = binaryCache.get(path);
    if (cached && cached.mtimeMs === stat.mtimeMs) return cached.body;
    const buf = Buffer.alloc(stat.size);
    let read = 0;
    while (read < stat.size) {
      const n = readSync(fd, buf, read, stat.size - read, read);
      if (n <= 0) break;
      read += n;
    }
    binaryCache.set(path, { body: buf.slice(0, read), mtimeMs: stat.mtimeMs });
    return buf.slice(0, read);
  } finally {
    closeSync(fd);
  }
}

function loadIndexTemplate(): string {
  return loadCachedText(join(ASSET_DIR, "index.html"));
}

/** Append `?token=` to relative chunk imports — browsers drop the parent query on relative ESM resolution. */
function injectTokenIntoChunkImports(body: string, token: string): string {
  return body.replace(
    /(from\s*|import\s*)(["'])(\.\/[\w.-]+\.js)\2/g,
    (_, kw: string, q: string, path: string) => `${kw}${q}${path}?token=${token}${q}`,
  );
}

function loadApp(token: string): string {
  const raw = loadCachedText(join(ASSET_DIR, "dist", "app.js"));
  return injectTokenIntoChunkImports(raw, token);
}

function loadChunk(name: string, token: string): string | null {
  try {
    const raw = loadCachedText(join(ASSET_DIR, "dist", name));
    return injectTokenIntoChunkImports(raw, token);
  } catch {
    return null;
  }
}

function loadAppMap(): string | null {
  try {
    return loadCachedText(join(ASSET_DIR, "dist", "app.js.map"));
  } catch {
    return null;
  }
}

function loadCss(): string {
  // Try new React dashboard first, then fall back to old Preact
  try {
    return loadCachedText(join(ASSET_DIR, "dist", "app.css"));
  } catch {
    return loadCachedText(join(ASSET_DIR, "app.css"));
  }
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

function loadVendorCss(name: string): string | null {
  try {
    return loadCachedText(join(ASSET_DIR, "dist", name));
  } catch {
    return null;
  }
}

/** MIME types for static files we serve from dist/. */
const MIME_MAP: Record<string, string> = {
  ".js": "application/javascript; charset=utf-8",
  ".mjs": "application/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".woff2": "font/woff2",
  ".woff": "font/woff",
  ".ttf": "font/ttf",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".json": "application/json; charset=utf-8",
};

/** Binary extensions that must be served as raw buffers, not UTF-8 strings. */
const BINARY_EXTS = new Set([".woff2", ".woff", ".ttf", ".png", ".ico"]);

function mimetypeFor(name: string): string | null {
  for (const [ext, mt] of Object.entries(MIME_MAP)) {
    if (name.endsWith(ext)) return mt;
  }
  return null;
}

function isBinaryAsset(name: string): boolean {
  for (const ext of BINARY_EXTS) {
    if (name.endsWith(ext)) return true;
  }
  return false;
}

function loadDistFile(name: string): { body: string | Buffer; isBinary: boolean } | null {
  const paths = [join(ASSET_DIR, "dist", "assets", name), join(ASSET_DIR, "dist", name)];
  const binary = isBinaryAsset(name);
  for (const p of paths) {
    try {
      return {
        body: binary ? loadCachedBinary(p) : loadCachedText(p),
        isBinary: binary,
      };
    } catch {
      /* try next path */
    }
  }
  return null;
}

export function serveAsset(
  name: string,
  token = "",
): { body: string | Buffer; contentType: string } | null {
  if (name === "app.js") {
    return { body: loadApp(token), contentType: "application/javascript; charset=utf-8" };
  }
  if (name === "app.js.map") {
    const body = loadAppMap();
    return body == null ? null : { body, contentType: "application/json; charset=utf-8" };
  }
  if (name === "app.css") {
    return { body: loadCss(), contentType: "text/css; charset=utf-8" };
  }
  // Same rewrite for chunk-to-chunk imports (e.g. vendor-markdown → vendor-react).
  if (/^vendor-[\w.-]+\.js$/.test(name)) {
    const body = loadChunk(name, token);
    if (body == null) return null;
    return { body, contentType: "application/javascript; charset=utf-8" };
  }
  if (VENDOR_CSS_NAMES.has(name)) {
    const body = loadVendorCss(name);
    if (body == null) return null;
    return { body, contentType: "text/css; charset=utf-8" };
  }
  // 通用静态文件：字体、图片等
  const mt = mimetypeFor(name);
  if (mt) {
    const result = loadDistFile(name);
    if (result != null) return { body: result.body, contentType: mt };
  }
  return null;
}
