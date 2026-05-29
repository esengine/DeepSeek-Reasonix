import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  addSkillPath,
  loadResolvedSkillPaths,
  loadSkillPaths,
  resolveSkillPath,
} from "../src/config.js";
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
        model: "deepseek-v4-pro",
        reasoningEffort: "high",
        search: false,
      }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    const cfg = readCfg(configPath);
    expect(cfg.lang).toBe("EN");
    expect(cfg.baseUrl).toBe("https://example.com");
    expect(cfg.model).toBe("deepseek-v4-pro");
    expect(cfg.reasoningEffort).toBe("high");
    expect(cfg.search).toBe(false);
  });

  it("accepts the full effort enum (low | medium | high | max)", async () => {
    for (const effort of ["low", "medium", "high", "max"] as const) {
      const res = await handleSettings(
        "POST",
        [],
        JSON.stringify({ reasoningEffort: effort }),
        makeCtx(configPath),
      );
      expect(res.status).toBe(200);
      expect(readCfg(configPath).reasoningEffort).toBe(effort);
    }
  });

  it("rejects an invalid effort value", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ reasoningEffort: "absurd" }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(400);
  });

  it("does not write to disk when no fields are provided", async () => {
    const before = readFileSync(configPath, "utf8");
    const res = await handleSettings("POST", [], JSON.stringify({}), makeCtx(configPath));
    expect(res.status).toBe(200);
    expect((res.body as { changed: string[] }).changed).toEqual([]);
    expect(readFileSync(configPath, "utf8")).toBe(before);
  });

  it("empty baseUrl clears the field on disk (issue #1409)", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ baseUrl: "" }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    const cfg = readCfg(configPath);
    expect(cfg.baseUrl).toBeUndefined();
    expect(Object.hasOwn(cfg, "baseUrl")).toBe(false);
  });

  it("whitespace-only baseUrl clears the field (issue #1409)", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ baseUrl: "   " }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    expect(readCfg(configPath).baseUrl).toBeUndefined();
  });

  it("GET surfaces null after baseUrl is cleared (issue #1409)", async () => {
    await handleSettings("POST", [], JSON.stringify({ baseUrl: "" }), makeCtx(configPath));
    const res = await handleSettings("GET", [], "", makeCtx(configPath));
    expect(res.status).toBe(200);
    expect((res.body as { baseUrl: string | null }).baseUrl).toBeNull();
  });

  it("accepts baidu as a web search engine and persists its API key", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ webSearchEngine: "baidu", baiduApiKey: "  baidu-test-key  " }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    const cfg = readCfg(configPath);
    expect(cfg.webSearchEngine).toBe("baidu");
    expect(cfg.baiduApiKey).toBe("baidu-test-key");
  });

  it("GET exposes a masked baiduApiKey prefix for dashboard settings", async () => {
    const prevBaidu = process.env.BAIDU_API_KEY;
    const prevQianfan = process.env.QIANFAN_API_KEY;
    Reflect.deleteProperty(process.env, "BAIDU_API_KEY");
    Reflect.deleteProperty(process.env, "QIANFAN_API_KEY");
    try {
      writeFileSync(
        configPath,
        JSON.stringify({ baiduApiKey: "bce-v3-config-secret-123456" }),
        "utf8",
      );
      const res = await handleSettings("GET", [], "", makeCtx(configPath));
      expect(res.status).toBe(200);
      expect((res.body as { webSearchApiKeys?: { baidu?: string } }).webSearchApiKeys?.baidu).toBe(
        "bce-v3…3456",
      );
    } finally {
      if (prevBaidu === undefined) Reflect.deleteProperty(process.env, "BAIDU_API_KEY");
      else process.env.BAIDU_API_KEY = prevBaidu;
      if (prevQianfan === undefined) Reflect.deleteProperty(process.env, "QIANFAN_API_KEY");
      else process.env.QIANFAN_API_KEY = prevQianfan;
    }
  });

  it("clears baiduApiKey when posted as null", async () => {
    writeFileSync(configPath, JSON.stringify({ baiduApiKey: "old-key" }), "utf8");
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ baiduApiKey: null }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    expect(readCfg(configPath).baiduApiKey).toBeUndefined();
  });

  it("rejects a non-string baseUrl (issue #1409)", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ baseUrl: 42 }),
      makeCtx(configPath),
    );
    expect(res.status).toBe(400);
    expect(readCfg(configPath).baseUrl).toBe("https://orig");
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

  it("GET persists search=true when the field is missing (issue #778)", async () => {
    writeFileSync(configPath, JSON.stringify({ lang: "EN" }), "utf8");
    expect(readCfg(configPath).search).toBeUndefined();
    const res = await handleSettings("GET", [], "", makeCtx(configPath));
    expect(res.status).toBe(200);
    expect((res.body as { search: boolean }).search).toBe(true);
    expect(readCfg(configPath).search).toBe(true);
  });

  it("GET does not rewrite the file when search is already explicit", async () => {
    writeFileSync(configPath, JSON.stringify({ lang: "EN", search: false }), "utf8");
    const before = readFileSync(configPath, "utf8");
    const res = await handleSettings("GET", [], "", makeCtx(configPath));
    expect(res.status).toBe(200);
    expect((res.body as { search: boolean }).search).toBe(false);
    expect(readFileSync(configPath, "utf8")).toBe(before);
  });

  it("GET/POST supports skillPaths as a comma-separated string", async () => {
    const customA = join(dir, "custom-a");
    const customB = join(dir, "custom-b");
    mkdirSync(customA, { recursive: true });
    const post = await handleSettings(
      "POST",
      [],
      JSON.stringify({ skillPaths: `${customA}, ${customB}, ${customA}` }),
      { ...makeCtx(configPath), getCurrentCwd: () => dir },
    );
    expect(post.status).toBe(200);
    expect((post.body as { changed: string[] }).changed).toContain("skillPaths");
    expect(readCfg(configPath).skills).toEqual({ paths: [customA, customB] });

    const get = await handleSettings("GET", [], "", {
      ...makeCtx(configPath),
      getCurrentCwd: () => dir,
    });
    expect((get.body as { skillPaths: string[] }).skillPaths).toEqual([customA, customB]);
  });

  it("POST keeps relative, home, and absolute skillPaths raw while exposing resolved entries", async () => {
    const absolute = "/opt/skills";
    const post = await handleSettings(
      "POST",
      [],
      JSON.stringify({
        skillPaths: "skills-local, ~/.agents/skills/find-skills, /opt/skills, skills-local",
      }),
      { ...makeCtx(configPath), getCurrentCwd: () => dir },
    );
    expect(post.status).toBe(200);
    expect(readCfg(configPath).skills).toEqual({
      paths: ["skills-local", "~/.agents/skills/find-skills", absolute],
    });

    const get = await handleSettings("GET", [], "", {
      ...makeCtx(configPath),
      getCurrentCwd: () => dir,
    });
    expect(get.status).toBe(200);
    expect((get.body as { skillPaths: string[] }).skillPaths).toEqual([
      "skills-local",
      "~/.agents/skills/find-skills",
      absolute,
    ]);
    expect(
      (get.body as { skillPathEntries: Array<{ raw: string; resolved: string }> }).skillPathEntries,
    ).toEqual([
      { raw: "skills-local", resolved: resolve(dir, "skills-local") },
      {
        raw: "~/.agents/skills/find-skills",
        resolved: join(homedir(), ".agents", "skills", "find-skills"),
      },
      { raw: absolute, resolved: resolve(absolute) },
    ]);
  });

  it("POST supports skillPaths as an array and stores relative paths raw", async () => {
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ skillPaths: ["skills-a", "", "skills-b"] }),
      { ...makeCtx(configPath), getCurrentCwd: () => dir },
    );
    expect(res.status).toBe(200);
    expect(readCfg(configPath).skills).toEqual({ paths: ["skills-a", "skills-b"] });
    expect(loadResolvedSkillPaths(dir, configPath)).toEqual([
      join(dir, "skills-a"),
      join(dir, "skills-b"),
    ]);
  });

  it("GET reads raw skillPaths written through the CLI helper", async () => {
    const result = addSkillPath("cli-skills", dir, configPath);
    expect("error" in result).toBe(false);

    const get = await handleSettings("GET", [], "", {
      ...makeCtx(configPath),
      getCurrentCwd: () => dir,
    });
    expect(get.status).toBe(200);
    expect((get.body as { skillPaths: string[] }).skillPaths).toEqual(["cli-skills"]);
  });

  it("GET returns settings POST skillPaths without expanding current user home", async () => {
    const post = await handleSettings(
      "POST",
      [],
      JSON.stringify({ skillPaths: ["~/.agents/skills/find-skills"] }),
      { ...makeCtx(configPath), getCurrentCwd: () => dir },
    );
    expect(post.status).toBe(200);
    expect(readCfg(configPath).skills).toEqual({ paths: ["~/.agents/skills/find-skills"] });

    const get = await handleSettings("GET", [], "", {
      ...makeCtx(configPath),
      getCurrentCwd: () => dir,
    });
    expect(get.status).toBe(200);
    expect((get.body as { skillPaths: string[] }).skillPaths).toEqual([
      "~/.agents/skills/find-skills",
    ]);
    expect(loadResolvedSkillPaths(dir, configPath)).toEqual([
      join(homedir(), ".agents", "skills", "find-skills"),
    ]);
  });

  it("CLI helper reads raw skillPaths written through settings POST", async () => {
    const post = await handleSettings("POST", [], JSON.stringify({ skillPaths: ["web-skills"] }), {
      ...makeCtx(configPath),
      getCurrentCwd: () => dir,
    });
    expect(post.status).toBe(200);
    expect(loadSkillPaths(dir, configPath)).toEqual(["web-skills"]);
    expect(loadResolvedSkillPaths(dir, configPath)).toEqual([join(dir, "web-skills")]);
  });

  it("CLI helper preserves raw skillPaths and exposes resolved paths", () => {
    const result = addSkillPath("skills-local", dir, configPath);
    expect("error" in result).toBe(false);
    expect(loadSkillPaths(dir, configPath)).toEqual(["skills-local"]);
    expect(loadResolvedSkillPaths(dir, configPath)).toEqual([join(dir, "skills-local")]);
    expect(resolveSkillPath("skills-local", dir)).toBe(join(dir, "skills-local"));
  });

  it("CLI helper preserves home raw skillPaths and resolves them internally", () => {
    const result = addSkillPath("~/.agents/skills/find-skills", dir, configPath);
    expect("error" in result).toBe(false);
    expect(loadSkillPaths(dir, configPath)).toEqual(["~/.agents/skills/find-skills"]);
    expect(loadResolvedSkillPaths(dir, configPath)).toEqual([
      join(homedir(), ".agents", "skills", "find-skills"),
    ]);
  });

  it("fires applyEffortLive + applyModelLive only after the disk write succeeds", async () => {
    const calls: string[] = [];
    const ctx: DashboardContext = {
      ...makeCtx(configPath),
      applyEffortLive: (e) => calls.push(`effort:${e}`),
      applyModelLive: (m) => calls.push(`model:${m}`),
    };
    const res = await handleSettings(
      "POST",
      [],
      JSON.stringify({ model: "deepseek-v4-pro", reasoningEffort: "high" }),
      ctx,
    );
    expect(res.status).toBe(200);
    expect(calls).toEqual(["effort:high", "model:deepseek-v4-pro"]);
    const cfg = readCfg(configPath);
    expect(cfg.model).toBe("deepseek-v4-pro");
    expect(cfg.reasoningEffort).toBe("high");
  });

  it("drops prototype-pollution keys from subagentModels", async () => {
    const res = await handleSettings(
      "POST",
      [],
      `{"subagentModels":{"__proto__":"flash","constructor":"pro","prototype":"flash","explore":"pro"}}`,
      makeCtx(configPath),
    );
    expect(res.status).toBe(200);
    const cfg = readCfg(configPath);
    expect(cfg.subagentModels).toEqual({ explore: "pro" });
    expect(({} as Record<string, unknown>).flash).toBeUndefined();
  });
});
