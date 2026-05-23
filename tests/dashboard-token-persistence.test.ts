import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  clearDashboardToken,
  ensureDashboardToken,
  readConfig,
  saveDashboardPort,
} from "../src/config.js";

describe("dashboard token + port persistence", () => {
  let dir: string;
  let cfgPath: string;
  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-dash-persist-"));
    cfgPath = join(dir, "config.json");
  });
  afterEach(() => {
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
  });

  it("mints a 64-char hex token on first call and writes it to config", () => {
    const tok = ensureDashboardToken(cfgPath);
    expect(tok).toMatch(/^[a-f0-9]{64}$/);
    expect(readConfig(cfgPath).dashboard?.token).toBe(tok);
  });

  it("returns the same token on subsequent calls (URL survives restart)", () => {
    const a = ensureDashboardToken(cfgPath);
    const b = ensureDashboardToken(cfgPath);
    expect(b).toBe(a);
  });

  it("remints when persisted token is shorter than 16 chars", () => {
    writeFileSync(cfgPath, JSON.stringify({ dashboard: { token: "abc" } }));
    const tok = ensureDashboardToken(cfgPath);
    expect(tok).not.toBe("abc");
    expect(tok.length).toBeGreaterThanOrEqual(64);
  });

  it("preserves other config fields when minting", () => {
    writeFileSync(cfgPath, JSON.stringify({ apiKey: "sk-keep-me", lang: "en" }));
    ensureDashboardToken(cfgPath);
    const cfg = readConfig(cfgPath);
    expect(cfg.apiKey).toBe("sk-keep-me");
    expect(cfg.lang).toBe("en");
    expect(cfg.dashboard?.token).toMatch(/^[a-f0-9]{64}$/);
  });

  it("saveDashboardPort persists the actual bound port", () => {
    saveDashboardPort(54123, cfgPath);
    expect(readConfig(cfgPath).dashboard?.port).toBe(54123);
  });

  it("saveDashboardPort is a no-op when the value matches what's already on disk", () => {
    saveDashboardPort(54123, cfgPath);
    const before = readFileSync(cfgPath, "utf8");
    saveDashboardPort(54123, cfgPath);
    const after = readFileSync(cfgPath, "utf8");
    expect(after).toBe(before);
  });

  it("saveDashboardPort rejects out-of-range / non-integer inputs", () => {
    saveDashboardPort(0, cfgPath);
    saveDashboardPort(70000, cfgPath);
    saveDashboardPort(Number.NaN, cfgPath);
    expect(existsSync(cfgPath)).toBe(false);
  });

  it("clearDashboardToken wipes only the token, leaving other dashboard fields intact", () => {
    ensureDashboardToken(cfgPath);
    saveDashboardPort(54123, cfgPath);
    clearDashboardToken(cfgPath);
    const cfg = readConfig(cfgPath);
    expect(cfg.dashboard?.token).toBeUndefined();
    expect(cfg.dashboard?.port).toBe(54123);
  });

  it("clearDashboardToken is a no-op when no token is stored", () => {
    writeFileSync(cfgPath, JSON.stringify({ apiKey: "sk-x" }));
    const before = readFileSync(cfgPath, "utf8");
    clearDashboardToken(cfgPath);
    const after = readFileSync(cfgPath, "utf8");
    expect(after).toBe(before);
  });
});
