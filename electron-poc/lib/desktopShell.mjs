/**
 * Local shell: serve the Wails desktop React dist on loopback and reverse-proxy
 * reasonix serve API routes onto the same origin (avoids CORS).
 */
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".webp": "image/webp",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".map": "application/json",
  ".ico": "image/x-icon",
};

/** Paths handled by reasonix serve (not static assets). */
const API_PREFIXES = [
  "/events",
  "/history",
  "/context",
  "/status",
  "/sessions",
  "/skills",
  "/todos",
  "/checkpoints",
  "/branches",
  "/models",
  "/provider-setup",
  "/submit",
  "/cancel",
  "/approve",
  "/answer",
  "/plan",
  "/compact",
  "/new",
  "/rewind",
  "/fork",
  "/summarize",
  "/tool-approval-mode",
  "/auto-approve-tools",
  "/bypass",
  "/goal",
  "/resume",
  "/forget",
  "/delete-session",
  "/extensions/",
  "/login",
  "/tabs",
  "/desktop/",
];

function isApiPath(pathname) {
  return API_PREFIXES.some((p) => pathname === p || pathname.startsWith(p));
}

function safeJoin(root, reqPath) {
  const decoded = decodeURIComponent(reqPath.split("?")[0]);
  const rel = decoded === "/" ? "/index.html" : decoded;
  const full = path.normalize(path.join(root, rel));
  if (!full.startsWith(path.normalize(root + path.sep)) && full !== path.normalize(root)) {
    return null;
  }
  return full;
}

/**
 * @param {{
 *   staticDir: string,
 *   serveBaseUrl: string,
 *   token: string,
 *   workspace?: string,
 * }} opts
 */
export function startDesktopShell(opts) {
  const staticDir = path.resolve(opts.staticDir);
  if (!fs.existsSync(path.join(staticDir, "index.html"))) {
    throw new Error(
      `desktop UI not built: missing ${path.join(staticDir, "index.html")}. Run: npm run build:ui`,
    );
  }

  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url || "/", "http://127.0.0.1");
      if (isApiPath(url.pathname)) {
        await proxyToServe(req, res, url, opts.serveBaseUrl, opts.token);
        return;
      }
      await serveStatic(req, res, url, staticDir, opts);
    } catch (err) {
      res.writeHead(500, { "Content-Type": "text/plain" });
      res.end(String(err?.message || err));
    }
  });

  return new Promise((resolve, reject) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      const baseUrl = `http://127.0.0.1:${port}`;
      resolve({
        server,
        port,
        baseUrl,
        uiUrl: `${baseUrl}/`,
        close: () =>
          new Promise((r) => {
            server.close(() => r(undefined));
          }),
      });
    });
    server.on("error", reject);
  });
}

async function serveStatic(req, res, url, staticDir, opts) {
  let filePath = safeJoin(staticDir, url.pathname);
  if (!filePath) {
    res.writeHead(403);
    res.end("forbidden");
    return;
  }
  if (fs.existsSync(filePath) && fs.statSync(filePath).isDirectory()) {
    filePath = path.join(filePath, "index.html");
  }
  if (!fs.existsSync(filePath)) {
    // SPA fallback
    filePath = path.join(staticDir, "index.html");
  }
  if (!fs.existsSync(filePath)) {
    res.writeHead(404);
    res.end("not found");
    return;
  }

  const ext = path.extname(filePath).toLowerCase();
  let body = fs.readFileSync(filePath);
  if (ext === ".html") {
    const shellOrigin = `http://127.0.0.1`; // filled below via Host header
    const host = req.headers.host || "127.0.0.1";
    const inject = `<script>window.__REASONIX_SERVE__=${JSON.stringify({
      baseUrl: `http://${host}`,
      token: opts.token,
      workspace: opts.workspace || "",
    })};</script>`;
    let html = body.toString("utf8");
    if (html.includes("</head>")) {
      html = html.replace("</head>", `${inject}</head>`);
    } else {
      html = inject + html;
    }
    body = Buffer.from(html, "utf8");
  }
  res.writeHead(200, {
    "Content-Type": MIME[ext] || "application/octet-stream",
    "Cache-Control": ext === ".html" ? "no-cache" : "public, max-age=3600",
  });
  res.end(body);
}

async function proxyToServe(req, res, url, serveBaseUrl, token) {
  // Drop client query token noise; authenticate upstream with the live cookie only.
  // serve token-mode: valid cookie → 200; ?token= alone → 302 strip (which we never follow).
  const target = new URL(url.pathname, serveBaseUrl.replace(/\/$/, "") + "/");
  // Preserve non-token query params (if any future API needs them).
  for (const [k, v] of url.searchParams.entries()) {
    if (k === "token") continue;
    target.searchParams.append(k, v);
  }

  const headers = { ...req.headers, host: target.host };
  // Always overwrite reasonix_token — browser may still send a stale shell cookie
  // after serve restart (old mergeCookie kept it → 401 Unauthorized toast).
  headers.cookie = withReasonixTokenCookie(headers.cookie, token);
  delete headers["accept-encoding"];
  delete headers["connection"];
  delete headers["keep-alive"];
  delete headers["transfer-encoding"];
  delete headers["content-length"];

  const init = {
    method: req.method,
    headers,
    // Never follow auth redirects; cookie on the first hop must succeed.
    redirect: "manual",
    duplex: "half",
  };
  if (req.method !== "GET" && req.method !== "HEAD") {
    const chunks = [];
    for await (const c of req) chunks.push(c);
    init.body = Buffer.concat(chunks);
    if (!headers["content-type"] && req.method === "POST") {
      headers["content-type"] = "application/json";
    }
  }

  const upstream = await fetch(target.toString(), init);
  const outHeaders = {};
  upstream.headers.forEach((v, k) => {
    const key = k.toLowerCase();
    // Do not leak serve Set-Cookie onto the shell origin (stale token trap).
    if (key === "transfer-encoding" || key === "set-cookie" || key === "content-encoding") return;
    outHeaders[k] = v;
  });
  res.writeHead(upstream.status, outHeaders);
  if (!upstream.body) {
    res.end();
    return;
  }
  const reader = upstream.body.getReader();
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      res.write(Buffer.from(value));
    }
  } catch {
    /* client aborted */
  }
  res.end();
}

/**
 * Force `reasonix_token=<token>` into the Cookie header, replacing any prior value.
 * @param {string | string[] | undefined} existing
 * @param {string} token
 */
export function withReasonixTokenCookie(existing, token) {
  const raw = Array.isArray(existing) ? existing.join("; ") : String(existing || "");
  const parts = raw
    .split(";")
    .map((p) => p.trim())
    .filter((p) => p && !/^reasonix_token=/i.test(p));
  parts.push(`reasonix_token=${token}`);
  return parts.join("; ");
}

export function defaultDesktopUiDir() {
  // Prefer electron-poc/desktop-ui (built copy), then desktop/frontend/dist
  const here = path.dirname(fileURLToPath(import.meta.url));
  const candidates = [
    path.resolve(here, "../desktop-ui"),
    path.resolve(here, "../../desktop/frontend/dist"),
  ];
  for (const c of candidates) {
    if (fs.existsSync(path.join(c, "index.html"))) return c;
  }
  return candidates[0];
}
