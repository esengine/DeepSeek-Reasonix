// Regression test for issue #2020 — desktop CSP must allow DeepSeek API and updater endpoints.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

function loadCsp(): string {
  const confPath = resolve(__dirname, "../desktop/src-tauri/tauri.conf.json");
  const raw = readFileSync(confPath, "utf8");
  const conf = JSON.parse(raw) as { app?: { security?: { csp?: string } } };
  return conf.app?.security?.csp ?? "";
}

describe("desktop CSP (tauri.conf.json)", () => {
  const csp = loadCsp();

  it("allows DeepSeek API in connect-src (#2020)", () => {
    expect(csp).toContain("https://api.deepseek.com");
  });

  it("allows updater R2 endpoint in connect-src", () => {
    expect(csp).toContain("https://pub-147fb53b9c1e4bbf891a257968619ea7.r2.dev");
  });

  it("allows GitHub releases in connect-src (updater fallback)", () => {
    expect(csp).toContain("https://github.com");
  });

  it("retains Tauri IPC in connect-src", () => {
    expect(csp).toContain("ipc:");
    expect(csp).toContain("http://ipc.localhost");
  });

  it("retains self in connect-src", () => {
    const match = csp.match(/connect-src\s+([^;]+)/);
    expect(match).toBeTruthy();
    expect(match![1]).toContain("'self'");
  });
});
