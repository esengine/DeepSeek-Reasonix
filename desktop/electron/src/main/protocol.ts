import { createReadStream, statSync } from "node:fs";
import { extname, join, resolve, sep } from "node:path";
import { Readable } from "node:stream";
import { errorText, type Logger } from "./log.js";

export const APP_SCHEME = "reasonix";
export const APP_ORIGIN = "reasonix://app";
export const APP_INDEX_URL = "reasonix://app/index.html";

// Must match desktop/workspace_media.go, desktop/theme_assets.go and
// desktop/remote_markdown_image.go; only these reach the resource origin.
export const FORWARDED_PATHS = [
  "/__reasonix_workspace_media/",
  "/__reasonix_theme_asset/",
  "/__reasonix_remote_markdown_image",
] as const;

export type AppRoute =
  | { kind: "file"; path: string; mime: string }
  | { kind: "forward"; target: string }
  | { kind: "notFound"; reason: string };

const MIME: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".map": "application/json; charset=utf-8",
  ".webmanifest": "application/manifest+json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".txt": "text/plain; charset=utf-8",
  ".wasm": "application/wasm",
  ".mp3": "audio/mpeg",
  ".ogg": "audio/ogg",
  ".wav": "audio/wav",
  ".mp4": "video/mp4",
  ".webm": "video/webm",
};

export function mimeFor(path: string): string {
  return MIME[extname(path).toLowerCase()] ?? "application/octet-stream";
}

export function isForwardedPath(pathname: string): boolean {
  return FORWARDED_PATHS.some((prefix) =>
    prefix.endsWith("/") ? pathname.startsWith(prefix) : pathname === prefix || pathname.startsWith(prefix + "/"));
}

const notFound = (reason: string): AppRoute => ({ kind: "notFound", reason });

export function routeAppRequest(rawURL: string, distRoot: string, isFile: (path: string) => boolean): AppRoute {
  let url: URL;
  try {
    url = new URL(rawURL);
  } catch {
    return notFound("malformed URL");
  }
  if (url.protocol !== `${APP_SCHEME}:`) return notFound("unsupported scheme");
  if (url.host !== "app") return notFound("unknown host");
  if (isForwardedPath(url.pathname)) return { kind: "forward", target: url.pathname + url.search };
  let decoded: string;
  try {
    decoded = decodeURIComponent(url.pathname);
  } catch {
    return notFound("malformed path");
  }
  if (decoded.includes("\0") || decoded.includes("\\")) return notFound("illegal characters");
  const pathname = decoded === "/" || decoded === "" ? "/index.html" : decoded;
  const segments = pathname.split("/").slice(1);
  if (segments.some((segment) => segment === "" || segment === "." || segment === "..")) return notFound("path traversal");
  const root = resolve(distRoot);
  const file = join(root, ...segments);
  if (!file.startsWith(root + sep)) return notFound("outside dist root");
  if (!isFile(file)) return notFound("no such file");
  return { kind: "file", path: file, mime: mimeFor(file) };
}

export function resolveDistRoot(input: { env: NodeJS.ProcessEnv; appPath: string; resourcesPath: string; packaged: boolean }): string {
  const override = (input.env.REASONIX_FRONTEND_DIST ?? "").trim();
  if (override !== "") return resolve(override);
  return input.packaged ? join(input.resourcesPath, "app") : resolve(input.appPath, "..", "frontend", "dist");
}

export interface ResourceOrigin {
  origin: string;
  token: string;
}

export interface AppProtocolDeps {
  protocol: { handle(scheme: string, handler: (request: Request) => Promise<Response> | Response): void };
  fetch(input: string, init: RequestInit & { bypassCustomProtocolHandlers?: boolean }): Promise<Response>;
  distRoot: string;
  resources(): ResourceOrigin | null;
  log: Logger;
}

const FORWARDED_REQUEST_HEADERS = ["accept", "range", "if-none-match", "if-modified-since"];

function fileExists(path: string): boolean {
  try {
    return statSync(path).isFile();
  } catch {
    return false;
  }
}

function text(status: number, body: string): Response {
  return new Response(body, { status, headers: { "content-type": "text/plain; charset=utf-8" } });
}

export function registerAppProtocol(deps: AppProtocolDeps): void {
  deps.protocol.handle(APP_SCHEME, async (request) => {
    const route = routeAppRequest(request.url, deps.distRoot, fileExists);
    if (route.kind === "notFound") {
      deps.log.warn(`404 ${request.url}: ${route.reason}`);
      return text(404, "Not found");
    }
    if (route.kind === "forward") {
      const resources = deps.resources();
      if (!resources) return text(503, "Desktop service unavailable");
      const headers = new Headers({ authorization: `Bearer ${resources.token}` });
      for (const name of FORWARDED_REQUEST_HEADERS) {
        const value = request.headers.get(name);
        if (value) headers.set(name, value);
      }
      try {
        return await deps.fetch(resources.origin + route.target, { method: request.method, headers, bypassCustomProtocolHandlers: true });
      } catch (error) {
        deps.log.warn(`resource forward failed for ${route.target}: ${errorText(error)}`);
        return text(502, "Resource origin unreachable");
      }
    }
    const body = Readable.toWeb(createReadStream(route.path)) as unknown as ReadableStream;
    return new Response(body, { status: 200, headers: { "content-type": route.mime, "cache-control": "no-cache" } });
  });
}
