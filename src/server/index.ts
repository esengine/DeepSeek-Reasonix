/** Dashboard HTTP server — pinned to 127.0.0.1, ephemeral per-boot token; mutations require the token in the header (CSRF). */

import { randomBytes } from "node:crypto";
import { type IncomingMessage, type ServerResponse, createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { handleEvents } from "./api/events.js";
import { renderIndexHtml, serveAsset } from "./assets.js";
import type { DashboardContext } from "./context.js";
import { handleApi } from "./router.js";

export interface StartDashboardOptions {
  /** Force a specific port. 0 = ephemeral. Default: 0. */
  port?: number;
  /** Host to bind. Argument exists for tests; production must keep 127.0.0.1 (no remote auth). */
  host?: string;
  token?: string;
}

export interface DashboardServerHandle {
  url: string;
  token: string;
  port: number;
  /** Stop accepting new connections, drain, close. Idempotent. */
  close: () => Promise<void>;
}

function mintToken(): string {
  return randomBytes(32).toString("hex");
}

/** `===` short-circuits on first mismatch — leaks position via timing even on localhost. */
export function constantTimeEquals(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let mismatch = 0;
  for (let i = 0; i < a.length; i++) {
    mismatch |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }
  return mismatch === 0;
}

/** Mutations require header (CSRF); reads accept header or query. Returns null on success. */
export function checkAuth(
  req: IncomingMessage,
  expectedToken: string,
  isMutation: boolean,
): { status: number; body: string } | null {
  const url = new URL(req.url ?? "/", "http://localhost");
  const queryToken = url.searchParams.get("token") ?? "";
  const headerToken =
    typeof req.headers["x-reasonix-token"] === "string"
      ? (req.headers["x-reasonix-token"] as string)
      : "";

  if (isMutation) {
    // Header-only for mutations. Query-only requests would still
    // reject here even if the token matched.
    if (!headerToken || !constantTimeEquals(headerToken, expectedToken)) {
      return {
        status: 403,
        body: JSON.stringify({
          error:
            "mutation requires X-Reasonix-Token header (CSRF defence — query token alone is rejected for POST/DELETE).",
        }),
      };
    }
    return null;
  }

  // Reads accept either form. We compare both candidates against the
  // expected token in constant time and treat the OR as "any match
  // lets through."
  if (
    (queryToken && constantTimeEquals(queryToken, expectedToken)) ||
    (headerToken && constantTimeEquals(headerToken, expectedToken))
  ) {
    return null;
  }
  return {
    status: 401,
    body: JSON.stringify({ error: "missing or invalid token" }),
  };
}

const MAX_BODY_BYTES = 256 * 1024;

export async function readBody(req: IncomingMessage): Promise<string> {
  let total = 0;
  const chunks: Buffer[] = [];
  return new Promise((resolve, reject) => {
    req.on("data", (chunk: Buffer) => {
      total += chunk.length;
      if (total > MAX_BODY_BYTES) {
        reject(new Error(`body exceeds ${MAX_BODY_BYTES} bytes`));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

export async function dispatch(
  req: IncomingMessage,
  res: ServerResponse,
  ctx: DashboardContext,
  expectedToken: string,
): Promise<void> {
  const url = new URL(req.url ?? "/", "http://localhost");
  const path = url.pathname;
  const method = (req.method ?? "GET").toUpperCase();
  const isMutation = method === "POST" || method === "DELETE" || method === "PUT";

  // SPA routes — token-gate the HTML so a stranger can't even see the
  // shell without the token. This also means the user MUST come in
  // through the token-bearing URL we print to the TUI.
  if (path === "/" || path === "/index.html") {
    const fail = checkAuth(req, expectedToken, false);
    if (fail) {
      res.writeHead(fail.status, { "content-type": "text/plain" });
      res.end("unauthorized — open the URL printed by /dashboard, including ?token=…");
      return;
    }
    const html = renderIndexHtml(expectedToken, ctx.mode);
    res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    res.end(html);
    return;
  }

  if (path.startsWith("/assets/")) {
    const fail = checkAuth(req, expectedToken, false);
    if (fail) {
      res.writeHead(fail.status);
      res.end();
      return;
    }
    const asset = serveAsset(path.slice("/assets/".length));
    if (!asset) {
      res.writeHead(404);
      res.end("not found");
      return;
    }
    res.writeHead(200, { "content-type": asset.contentType });
    res.end(asset.body);
    return;
  }

  // SSE event stream — special-cased BEFORE the normal `/api/*` branch
  // because it keeps the response open and writes its own frames; the
  // normal path would try to JSON-encode and end the response.
  if (path === "/api/events") {
    const fail = checkAuth(req, expectedToken, false);
    if (fail) {
      res.writeHead(fail.status, { "content-type": "application/json" });
      res.end(fail.body);
      return;
    }
    handleEvents(req, res, ctx);
    return;
  }

  if (path.startsWith("/api/")) {
    const fail = checkAuth(req, expectedToken, isMutation);
    if (fail) {
      res.writeHead(fail.status, { "content-type": "application/json" });
      res.end(fail.body);
      return;
    }
    let body = "";
    if (isMutation) {
      try {
        body = await readBody(req);
      } catch (err) {
        res.writeHead(413, { "content-type": "application/json" });
        res.end(JSON.stringify({ error: (err as Error).message }));
        return;
      }
    }
    const result = await handleApi(path.slice("/api/".length), method, body, ctx, url.searchParams);
    res.writeHead(result.status, { "content-type": "application/json" });
    res.end(JSON.stringify(result.body));
    return;
  }

  res.writeHead(404, { "content-type": "text/plain" });
  res.end("not found");
}

/**
 * Boot a server bound to 127.0.0.1, return an awaitable handle.
 */
export function startDashboardServer(
  ctx: DashboardContext,
  opts: StartDashboardOptions = {},
): Promise<DashboardServerHandle> {
  const token = opts.token ?? mintToken();
  const host = opts.host ?? "127.0.0.1";
  const port = opts.port ?? 0;

  return new Promise((resolve, reject) => {
    const server = createServer((req, res) => {
      dispatch(req, res, ctx, token).catch((err) => {
        if (!res.headersSent) {
          res.writeHead(500, { "content-type": "application/json" });
        }
        res.end(JSON.stringify({ error: (err as Error).message }));
      });
    });
    server.on("error", reject);
    server.listen(port, host, () => {
      const addr = server.address() as AddressInfo;
      const finalPort = addr.port;
      const url = `http://${host}:${finalPort}/?token=${token}`;

      let closed = false;
      const close = (): Promise<void> =>
        new Promise<void>((doneResolve) => {
          if (closed) return doneResolve();
          closed = true;
          server.close(() => doneResolve());
          // Force any keep-alive sockets to drop after a short grace.
          setTimeout(() => server.closeAllConnections?.(), 1000).unref();
        });

      resolve({ url, token, port: finalPort, close });
    });
  });
}
