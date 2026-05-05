import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { handleSettings } from "../src/server/api/settings.js";
import type { DashboardContext } from "../src/server/context.js";

function makeCtx(configPath: string): DashboardContext {
  return {
    configPath,
    usageLogPath: join(configPath, "..", "usage.json"),
    mode: "standalone",
  };
}

function readCfg(path: string): Record<string, unknown> {
  return JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
}

describe("settings API — combined POST persistence (#274)", () => {
  let dir: string;
  let configPath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-settings-"));
    configPath = join(dir, "config.json");
    writeFileSync(configPath, JSON.stringify({ lang: "ZH", baseUrl: "https://orig" }), "utf8");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("preserves lang when posted alongside baseUrl", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ lang: "EN", baseUrl: "https://example.com" }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    const cfg = readCfg(configPath);
    expect(cfg.lang).toBe("EN");
    expect(cfg.baseUrl).toBe("https://example.com");
  });

  it("preserves all fields in a multi-field POST", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({
        lang: "EN",
        baseUrl: "https://example.com",
        preset: "pro",
        reasoningEffort: "high",
        search: false,
      }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    const cfg = readCfg(configPath);
    expect(cfg.lang).toBe("EN");
    expect(cfg.baseUrl).toBe("https://example.com");
    expect(cfg.preset).toBe("pro");
    expect(cfg.reasoningEffort).toBe("high");
    expect(cfg.search).toBe(false);
  });

  it("does not write to disk when no fields are provided", async () => {
    const before = readFileSync(configPath, "utf8");
    const res = await handleSettings("POST", [], JSON.stringify({}), makeCtx(configPath));
    expect(res.status).toBe(200);
    expect((res.body as { changed: string[] }).changed).toEqual([]);
    expect(readFileSync(configPath, "utf8")).toBe(before);
  });

  it("rejects an invalid lang without writing other fields", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ lang: "XX", baseUrl: "https://changed" }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(400);
    const cfg = readCfg(configPath);
    expect(cfg.lang).toBe("ZH");
    expect(cfg.baseUrl).toBe("https://orig");
  });

  it("persists apiKey alongside other fields without losing them", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ apiKey: "sk-1234567890abcdef", lang: "EN" }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    const cfg = readCfg(configPath);
    expect(cfg.apiKey).toBe("sk-1234567890abcdef");
    expect(cfg.lang).toBe("EN");
  });

  it("fires applyPresetLive only after the disk write succeeds", async () => {
    const calls: string[] = [];
    const ctx: DashboardContext = {
      ...makeCtx(configPath),
      applyPresetLive: (n) => calls.push(`preset:${n}`),
      applyEffortLive: (e) => calls.push(`effort:${e}`),
    };
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ preset: "flash", reasoningEffort: "high" }),
      ctx,
    );
    expect(res.status).toBe(200);
    expect(calls).toEqual(["preset:flash", "effort:high"]);
    const cfg = readCfg(configPath);
    expect(cfg.preset).toBe("flash");
    expect(cfg.reasoningEffort).toBe("high");
  });
});
