/** Dashboard smoke tests — verify build artifacts + CLI token URL flow. */

import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const DASHBOARD_ROOT = join(process.cwd(), "dashboard");
const DASHBOARD_DIST = join(DASHBOARD_ROOT, "dist");

describe("dashboard build artifacts", () => {
  it("index.html exists", () => {
    expect(existsSync(join(DASHBOARD_ROOT, "index.html"))).toBe(true);
  });

  it("app.js exists in dist", () => {
    expect(existsSync(join(DASHBOARD_DIST, "app.js"))).toBe(true);
  });

  it("app.css exists in dist", () => {
    expect(existsSync(join(DASHBOARD_DIST, "app.css"))).toBe(true);
  });

  it("index.html contains placeholder tokens", () => {
    const html = readFileSync(join(DASHBOARD_ROOT, "index.html"), "utf8");
    expect(html).toContain("__REASONIX_TOKEN__");
    expect(html).toContain("__REASONIX_MODE__");
  });

  it("app.js references tauri-bridge", () => {
    const js = readFileSync(join(DASHBOARD_DIST, "app.js"), "utf8");
    expect(js).toContain("tauri-bridge");
  });

  it("app.js is non-empty", () => {
    const js = readFileSync(join(DASHBOARD_DIST, "app.js"), "utf8");
    expect(js.length).toBeGreaterThan(10_000);
  });

  it("app.css is non-empty", () => {
    const css = readFileSync(join(DASHBOARD_DIST, "app.css"), "utf8");
    expect(css.length).toBeGreaterThan(1_000);
  });
});

describe("dashboard server integration", () => {
  it("assets.ts resolveAssetDir finds dashboard", async () => {
    const { serveAsset } = await import("../src/server/assets.js");
    const appJs = serveAsset("app.js");
    expect(appJs).not.toBeNull();
    expect(appJs?.contentType).toMatch(/javascript/);
  });

  it("renderIndexHtml replaces all token placeholders", async () => {
    const { renderIndexHtml } = await import("../src/server/assets.js");
    // Token is sanitized to alphanumeric only
    const html = renderIndexHtml("test-token-123", "standalone");
    expect(html).not.toContain("__REASONIX_TOKEN__");
    expect(html).not.toContain("__REASONIX_MODE__");
    expect(html).toContain("testtoken123"); // sanitized
    expect(html).toContain("standalone");
  });

  it("vendor CSS files are served when present", async () => {
    const { serveAsset } = await import("../src/server/assets.js");
    const hljs = serveAsset("vendor-hljs.css");
    const uplot = serveAsset("vendor-uplot.css");
    // These may be null if copy-dashboard-vendor-css.mjs hasn't run
    if (hljs) {
      expect(hljs.contentType).toMatch(/css/);
    }
    if (uplot) {
      expect(uplot.contentType).toMatch(/css/);
    }
  });
});
