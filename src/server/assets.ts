import { readFileSync } from "node:fs";
import { open } from "node:fs/promises";
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

async function loadCachedText(path: string): Promise<string> {
  const fd = await open(path, "r");
  try {
    const fileStat = await fd.stat();
    const cached = textCache.get(path);
    if (cached && cached.mtimeMs === fileStat.mtimeMs) return cached.body;
    const body = await fd.readFile("utf8");
    textCache.set(path, { body, mtimeMs: fileStat.mtimeMs });
    return body;
  } finally {
    await fd.close();
  }
}

async function loadCachedBinary(path: string): Promise<Buffer> {
  const fd = await open(path, "r");
  try {
    const fileStat = await fd.stat();
    const cached = binaryCache.get(path);
    if (cached && cached.mtimeMs === fileStat.mtimeMs) return cached.body;
    const body = Buffer.from(await fd.readFile());
    binaryCache.set(path, { body, mtimeMs: fileStat.mtimeMs });
    return body;
  } finally {
    await fd.close();
  }
}

async function loadIndexTemplate(): Promise<string> {
  return loadCachedText(join(ASSET_DIR, "index.html"));
}

/** Append `?token=` to relative chunk imports — browsers drop the parent query on relative ESM resolution. */
function injectTokenIntoChunkImports(body: string, token: string): string {
  return body.replace(
    /(from\s*|import\s*)(["'])(\.\/[\w.-]+\.js)\2/g,
    (_, kw: string, q: string, path: string) => `${kw}${q}${path}?token=${token}${q}`,
  );
}

/** Same trick for CSS `url(/assets/foo.woff)` — fonts referenced from a token-stripped stylesheet would 401 otherwise. */
function injectTokenIntoCssAssetUrls(body: string, token: string): string {
  return body.replace(
    /url\((['"]?)(\/assets\/[\w./-]+\.(?:woff2?|ttf|otf|png|svg))(?:\?[^)'"]*)?\1\)/g,
    (_, q: string, path: string) => `url(${q}${path}?token=${token}${q})`,
  );
}

async function loadApp(token: string): Promise<string> {
  const raw = await loadCachedText(join(ASSET_DIR, "dist", "app.js"));
  return injectTokenIntoChunkImports(raw, token);
}

async function loadChunk(name: string, token: string): Promise<string | null> {
  try {
    const raw = await loadCachedText(join(ASSET_DIR, "dist", name));
    return injectTokenIntoChunkImports(raw, token);
  } catch {
    return null;
  }
}

async function loadAppMap(): Promise<string | null> {
  try {
    return loadCachedText(join(ASSET_DIR, "dist", "app.js.map"));
  } catch {
    return null;
  }
}

async function loadCss(token: string): Promise<string> {
  // Try new React dashboard first, then fall back to old Preact
  let raw: string;
  try {
    raw = await loadCachedText(join(ASSET_DIR, "dist", "app.css"));
  } catch {
    raw = await loadCachedText(join(ASSET_DIR, "app.css"));
  }
  return injectTokenIntoCssAssetUrls(raw, token);
}

/** Token HTML-attribute-escaped in case a future mint produces non-hex bytes. */
export async function renderIndexHtml(
  token: string,
  mode: "standalone" | "attached",
): Promise<string> {
  const tpl = await loadIndexTemplate();
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

async function loadVendorCss(name: string, token: string): Promise<string | null> {
  try {
    return injectTokenIntoCssAssetUrls(await loadCachedText(join(ASSET_DIR, "dist", name)), token);
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

async function loadDistFile(
  name: string,
): Promise<{ body: string | Buffer; isBinary: boolean } | null> {
  const paths = [join(ASSET_DIR, "dist", "assets", name), join(ASSET_DIR, "dist", name)];
  const binary = isBinaryAsset(name);
  for (const p of paths) {
    try {
      return {
        body: binary ? await loadCachedBinary(p) : await loadCachedText(p),
        isBinary: binary,
      };
    } catch {
      /* try next path */
    }
  }
  return null;
}

export async function serveAsset(
  name: string,
  token = "",
): Promise<{ body: string | Buffer; contentType: string } | null> {
  if (name === "app.js") {
    return { body: await loadApp(token), contentType: "application/javascript; charset=utf-8" };
  }
  if (name === "app.js.map") {
    const body = await loadAppMap();
    return body == null ? null : { body, contentType: "application/json; charset=utf-8" };
  }
  if (name === "app.css") {
    return { body: await loadCss(token), contentType: "text/css; charset=utf-8" };
  }
  // Same rewrite for chunk-to-chunk imports (e.g. vendor-markdown → vendor-react).
  if (/^vendor-[\w.-]+\.js$/.test(name)) {
    const body = await loadChunk(name, token);
    if (body == null) return null;
    return { body, contentType: "application/javascript; charset=utf-8" };
  }
  if (VENDOR_CSS_NAMES.has(name)) {
    const body = await loadVendorCss(name, token);
    if (body == null) return null;
    return { body, contentType: "text/css; charset=utf-8" };
  }
  // 通用静态文件：字体、图片等
  const mt = mimetypeFor(name);
  if (mt) {
    const result = await loadDistFile(name);
    if (result != null) return { body: result.body, contentType: mt };
  }
  return null;
}
