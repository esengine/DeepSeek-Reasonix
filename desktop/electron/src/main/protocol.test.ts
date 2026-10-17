import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { isForwardedPath, registerAppProtocol, resolveDistRoot, routeAppRequest } from "./protocol.js";

let dist = "";
const isFile = (path: string) => {
  try {
    return statSync(path).isFile();
  } catch {
    return false;
  }
};
const route = (url: string) => routeAppRequest(url, dist, isFile);

test("normal app documents do not pre-enable a profiling engine", async () => {
  let handler!: (request: Request) => Promise<Response> | Response;
  registerAppProtocol({
    protocol: { handle: (_scheme, callback) => { handler = callback; } },
    fetch: async () => new Response("forwarded"), distRoot: dist, resources: () => null,
    log: { info() {}, warn() {}, error() {} },
  });
  const response = await handler(new Request("reasonix://app/"));
  assert.equal(response.headers.get("Document-Policy"), null);
  await response.text();
});

before(() => {
  dist = mkdtempSync(join(tmpdir(), "reasonix-dist-"));
  mkdirSync(join(dist, "assets"), { recursive: true });
  writeFileSync(join(dist, "index.html"), "<!doctype html>");
  writeFileSync(join(dist, "assets", "app.js"), "export {};");
  writeFileSync(join(dist, "assets", "styles.css"), "body{}");
  writeFileSync(join(dist, "assets", "font.woff2"), "");
});

after(() => rmSync(dist, { recursive: true, force: true }));

test("the root and existing files under dist are served with their MIME types", () => {
  assert.deepEqual(route("reasonix://app/"), { kind: "file", path: join(dist, "index.html"), mime: "text/html; charset=utf-8" });
  assert.deepEqual(route("reasonix://app"), { kind: "file", path: join(dist, "index.html"), mime: "text/html; charset=utf-8" });
  assert.deepEqual(route("reasonix://app/index.html?boot=1"), { kind: "file", path: join(dist, "index.html"), mime: "text/html; charset=utf-8" });
  assert.deepEqual(route("reasonix://app/assets/app.js"), { kind: "file", path: join(dist, "assets", "app.js"), mime: "text/javascript; charset=utf-8" });
  assert.equal(route("reasonix://app/assets/styles.css").kind, "file");
  assert.equal((route("reasonix://app/assets/font.woff2") as { mime: string }).mime, "font/woff2");
});

test("index.html is not a fallback for other paths", () => {
  assert.equal(route("reasonix://app/settings").kind, "notFound");
  assert.equal(route("reasonix://app/assets/").kind, "notFound");
  assert.equal(route("reasonix://app/assets/missing.js").kind, "notFound");
});

test("traversal, absolute escapes and odd hosts are rejected", () => {
  // The WHATWG parser collapses literal and %2e-encoded dot segments before
  // routing, exactly as Chromium does for a standard scheme, so these can only
  // land inside dist; the encoded-slash and backslash forms must still fail.
  assert.equal(route("reasonix://app/../index.html").kind, "file");
  assert.equal(route("reasonix://app/%2e%2e/index.html").kind, "file");
  assert.equal(route("reasonix://app/assets/../../etc/passwd").kind, "notFound");
  assert.equal(route("reasonix://app/assets/%2e%2e/%2e%2e/etc/passwd").kind, "notFound");
  for (const url of [
    "reasonix://app//etc/passwd",
    "reasonix://app/assets/..%2Fapp.js",
    "reasonix://app/assets%5Capp.js",
    "reasonix://app/%00",
    "reasonix://evil/index.html",
    "http://app/index.html",
    "not a url",
  ]) {
    assert.equal(route(url).kind, "notFound", url);
  }
});

test("only the resource prefixes forward to the loopback origin, query included", () => {
  assert.deepEqual(route("reasonix://app/__reasonix_workspace_media/tok/a.png"), { kind: "forward", target: "/__reasonix_workspace_media/tok/a.png" });
  assert.deepEqual(route("reasonix://app/__reasonix_theme_asset/x.jpg?v=2"), { kind: "forward", target: "/__reasonix_theme_asset/x.jpg?v=2" });
  assert.deepEqual(route("reasonix://app/__reasonix_remote_markdown_image?url=https%3A%2F%2Fx"), {
    kind: "forward",
    target: "/__reasonix_remote_markdown_image?url=https%3A%2F%2Fx",
  });
  assert.equal(isForwardedPath("/__reasonix_remote_markdown_imagex"), false);
  assert.equal(isForwardedPath("/__reasonix_workspace_media"), false);
  assert.equal(route("reasonix://app/__shell/quit").kind, "notFound");
});

test("attachment uploads forward only the exact upload route, preserving the filename and target", () => {
  const query = "?target=chat%2Fsession&name=large%20app.apk";
  assert.deepEqual(route(`reasonix://app/__reasonix_attachment_upload${query}`), {
    kind: "forward",
    target: `/__reasonix_attachment_upload${query}`,
  });
  for (const path of ["/__reasonix_attachment_upload/", "/__reasonix_attachment_upload/extra", "/__reasonix_attachment_uploadx"]) {
    assert.equal(isForwardedPath(path), false, path);
    assert.equal(route(`reasonix://app${path}`).kind, "notFound", path);
  }
});

test("upload forwarding delivers chunks before the source finishes and preserves cancellation", async () => {
  let handler!: (request: Request) => Promise<Response> | Response;
  let source!: ReadableStreamDefaultController<Uint8Array>;
  const chunks = [new Uint8Array([0x50, 0x4b, 0x03, 0x04]), new Uint8Array([0, 255, 128, 1])];
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      source = controller;
      controller.enqueue(chunks[0]);
    },
  });
  const controller = new AbortController();
  const query = "?target=chat%2Fsession&name=large%20app.apk";
  const request = new Request(`reasonix://app/__reasonix_attachment_upload${query}`, {
    method: "POST",
    headers: {
      "content-type": "application/octet-stream",
      authorization: "Bearer renderer-controlled",
      cookie: "private=value",
      origin: "https://untrusted.example",
      "x-untrusted": "not-forwarded",
    },
    body: stream,
    signal: controller.signal,
    duplex: "half",
  } as RequestInit);
  // Reading through any whole-body convenience method would recreate the
  // large-file allocation that this transport is intended to avoid.
  request.arrayBuffer = async () => { throw new Error("upload was buffered with arrayBuffer()"); };
  request.text = async () => { throw new Error("upload was buffered with text()"); };
  request.blob = async () => { throw new Error("upload was buffered with blob()"); };
  let forwarded = false;
  registerAppProtocol({
    protocol: { handle: (_scheme, callback) => { handler = callback; } },
    fetch: async (input, init) => {
      forwarded = true;
      assert.equal(input, `http://127.0.0.1:12345/__reasonix_attachment_upload${query}`);
      assert.equal(init.method, "POST");
      assert.equal(init.body, request.body);
      assert.equal((init as RequestInit & { duplex?: string }).duplex, "half");
      assert.equal(init.signal, request.signal);
      assert.equal(init.bypassCustomProtocolHandlers, true);
      const headers = new Headers(init.headers);
      assert.equal(headers.get("authorization"), "Bearer server-secret");
      assert.equal(headers.get("content-type"), "application/octet-stream");
      for (const name of ["cookie", "origin", "x-untrusted"]) assert.equal(headers.get(name), null, name);

      const reader = (init.body as ReadableStream<Uint8Array>).getReader();
      assert.deepEqual(await reader.read(), { done: false, value: chunks[0] });
      // The final chunk does not exist until the origin has received the first.
      // Aggregating the body before forwarding cannot complete this exchange.
      source.enqueue(chunks[1]);
      source.close();
      assert.deepEqual(await reader.read(), { done: false, value: chunks[1] });
      assert.equal((await reader.read()).done, true);
      controller.abort();
      assert.equal(init.signal?.aborted, true);
      return new Response('{"name":"large app.apk"}', { status: 201, headers: { "content-type": "application/json" } });
    },
    distRoot: dist,
    resources: () => ({ origin: "http://127.0.0.1:12345", token: "server-secret" }),
    log: { info() {}, warn() {}, error() {} },
  });
  const response = await handler(request);
  assert.equal(forwarded, true, "the upload must reach the resource origin");
  assert.equal(response.status, 201);
  assert.deepEqual(await response.json(), { name: "large app.apk" });
});

test("upload routes reject methods other than POST and unsupported body formats", async () => {
  let handler!: (request: Request) => Promise<Response> | Response;
  let forwarded = 0;
  registerAppProtocol({
    protocol: { handle: (_scheme, callback) => { handler = callback; } },
    fetch: async () => { forwarded++; return new Response("unexpected forward"); },
    distRoot: dist,
    resources: () => ({ origin: "http://127.0.0.1:12345", token: "server-secret" }),
    log: { info() {}, warn() {}, error() {} },
  });
  for (const method of ["GET", "HEAD", "PUT", "PATCH", "DELETE"]) {
    const response = await handler(new Request("reasonix://app/__reasonix_attachment_upload?name=large.apk", { method }));
    assert.equal(response.status, 405, method);
  }
  for (const contentType of ["application/json", "multipart/form-data; boundary=test", "text/plain"]) {
    const response = await handler(new Request("reasonix://app/__reasonix_attachment_upload?name=large.apk", {
      method: "POST", headers: { "content-type": contentType }, body: "not raw bytes",
    }));
    assert.equal(response.status, 415, contentType);
  }
  assert.equal(forwarded, 0);
});

test("asset routes cannot be used to forward writes or request bodies", async () => {
  let handler!: (request: Request) => Promise<Response> | Response;
  let forwarded = 0;
  registerAppProtocol({
    protocol: { handle: (_scheme, callback) => { handler = callback; } },
    fetch: async () => { forwarded++; return new Response("unexpected forward"); },
    distRoot: dist,
    resources: () => ({ origin: "http://127.0.0.1:12345", token: "server-secret" }),
    log: { info() {}, warn() {}, error() {} },
  });
  for (const path of ["/__reasonix_workspace_media/token/a.png", "/__reasonix_theme_asset/a.png", "/__reasonix_remote_markdown_image?url=https%3A%2F%2Fexample.com%2Fa.png", "/assets/app.js"]) {
    for (const method of ["POST", "PUT", "PATCH", "DELETE"]) {
      const response = await handler(new Request(`reasonix://app${path}`, { method, body: "must not reach the origin" }));
      assert.equal(response.status, 405, `${method} ${path}`);
    }
  }
  assert.equal(forwarded, 0);
});

test("the dist root honours the override, then packaging layout", () => {
  assert.equal(resolveDistRoot({ env: { REASONIX_FRONTEND_DIST: "/tmp/dist" }, appPath: "/app/electron", resourcesPath: "/res", packaged: true }), "/tmp/dist");
  assert.equal(resolveDistRoot({ env: {}, appPath: "/app/electron", resourcesPath: "/res", packaged: false }), "/app/frontend/dist");
  assert.equal(resolveDistRoot({ env: {}, appPath: "/app/electron", resourcesPath: "/res", packaged: true }), "/res/app");
});
