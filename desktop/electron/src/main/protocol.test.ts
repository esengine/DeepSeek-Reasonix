import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { isForwardedPath, resolveDistRoot, routeAppRequest } from "./protocol.js";

let dist = "";
const isFile = (path: string) => {
  try {
    return statSync(path).isFile();
  } catch {
    return false;
  }
};
const route = (url: string) => routeAppRequest(url, dist, isFile);

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

test("the dist root honours the override, then packaging layout", () => {
  assert.equal(resolveDistRoot({ env: { REASONIX_FRONTEND_DIST: "/tmp/dist" }, appPath: "/app/electron", resourcesPath: "/res", packaged: true }), "/tmp/dist");
  assert.equal(resolveDistRoot({ env: {}, appPath: "/app/electron", resourcesPath: "/res", packaged: false }), "/app/frontend/dist");
  assert.equal(resolveDistRoot({ env: {}, appPath: "/app/electron", resourcesPath: "/res", packaged: true }), "/res/app");
});
