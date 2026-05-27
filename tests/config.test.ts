import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  type DesktopOpenTab,
  addProjectPathAllowed,
  addProjectShellAllowed,
  clearProjectPathAllowed,
  clearProjectShellAllowed,
  editModeHintShown,
  isPlausibleKey,
  loadApiKey,
  loadBaseUrl,
  loadBraveApiKey,
  loadDesktopOpenTabs,
  loadEditMode,
  loadEndpoint,
  loadEngineeringLifecycleMode,
  loadFilesystemOutlineThresholdBytes,
  loadIndexConfig,
  loadIndexUserConfig,
  loadModel,
  loadMouseWheelRows,
  loadPricingOverride,
  loadProjectPathAllowed,
  loadProjectShellAllowed,
  loadProxyConfig,
  loadRateLimit,
  loadReasoningEffort,
  loadSemanticEmbeddingUserConfig,
  loadSubagentModels,
  loadTheme,
  loadToolRateLimit,
  markEditModeHintShown,
  readConfig,
  redactKey,
  redactSemanticEmbeddingConfig,
  removeProjectPathAllowed,
  removeProjectShellAllowed,
  resolveSemanticEmbeddingConfig,
  resolveThemePreference,
  saveApiKey,
  saveBaseUrl,
  saveDesktopOpenTabs,
  saveEditMode,
  saveIndexConfig,
  saveReasoningEffort,
  saveSemanticEmbeddingConfig,
  saveSubagentModels,
  saveTheme,
  searchEnabled,
  webSearchEngine,
  writeConfig,
} from "../src/config.js";

describe("config", () => {
  let dir: string;
  let path: string;
  const originalEnv = process.env.DEEPSEEK_API_KEY;
  const originalSearch = process.env.REASONIX_SEARCH;
  const originalBaseUrl = process.env.DEEPSEEK_BASE_URL;
  const originalApiBaseUrl = process.env.DEEPSEEK_API_BASE_URL;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-test-"));
    path = join(dir, "config.json");
    // biome-ignore lint/performance/noDelete: the string "undefined" leaks into process.env otherwise
    delete process.env.DEEPSEEK_API_KEY;
    // biome-ignore lint/performance/noDelete: same reason
    delete process.env.REASONIX_SEARCH;
    // biome-ignore lint/performance/noDelete: same reason
    delete process.env.DEEPSEEK_BASE_URL;
    // biome-ignore lint/performance/noDelete: same reason
    delete process.env.DEEPSEEK_API_BASE_URL;
  });

  afterEach(() => {
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
    if (originalEnv === undefined) {
      // biome-ignore lint/performance/noDelete: same reason as beforeEach
      delete process.env.DEEPSEEK_API_KEY;
    } else {
      process.env.DEEPSEEK_API_KEY = originalEnv;
    }
    if (originalSearch === undefined) {
      // biome-ignore lint/performance/noDelete: same reason
      delete process.env.REASONIX_SEARCH;
    } else {
      process.env.REASONIX_SEARCH = originalSearch;
    }
    if (originalBaseUrl === undefined) {
      // biome-ignore lint/performance/noDelete: same reason
      delete process.env.DEEPSEEK_BASE_URL;
    } else {
      process.env.DEEPSEEK_BASE_URL = originalBaseUrl;
    }
    if (originalApiBaseUrl === undefined) {
      // biome-ignore lint/performance/noDelete: same reason
      delete process.env.DEEPSEEK_API_BASE_URL;
    } else {
      process.env.DEEPSEEK_API_BASE_URL = originalApiBaseUrl;
    }
  });

  it("readConfig returns {} when file is missing", () => {
    expect(readConfig(path)).toEqual({});
  });

  it("writeConfig + readConfig round-trip", () => {
    writeConfig({ apiKey: "sk-test123abcdefghijkl" }, path);
    expect(readConfig(path).apiKey).toBe("sk-test123abcdefghijkl");
  });

  it("writeConfig leaves no `.tmp` sibling behind on success", () => {
    writeConfig({ apiKey: "sk-test123abcdefghijkl", reasoningEffort: "high" }, path);
    const tmp = `${path}.${process.pid}.tmp`;
    expect(existsSync(tmp)).toBe(false);
    expect(existsSync(path)).toBe(true);
  });

  it("saveApiKey trims whitespace", () => {
    saveApiKey("  sk-test123abcdefghijkl  ", path);
    expect(readConfig(path).apiKey).toBe("sk-test123abcdefghijkl");
  });

  it("loadApiKey prefers env var over config file", () => {
    saveApiKey("sk-fromfile1234567890ab", path);
    process.env.DEEPSEEK_API_KEY = "sk-fromenv1234567890abcd";
    expect(loadApiKey(path)).toBe("sk-fromenv1234567890abcd");
  });

  it("loadApiKey falls back to config file when env unset", () => {
    saveApiKey("sk-fromfile1234567890ab", path);
    expect(loadApiKey(path)).toBe("sk-fromfile1234567890ab");
  });

  it("saveApiKey overrides a stale env var so an explicit UI save takes effect immediately", () => {
    // Repro: user has DEEPSEEK_API_KEY=<old> in User-level env / .env / shell rc.
    // Without the env update inside saveApiKey, loadEndpoint's fallback branch
    // keeps returning the stale env, so the desktop UI save looks like a no-op.
    process.env.DEEPSEEK_API_KEY = "sk-staleenv00000000000000";
    saveApiKey("sk-freshfromui00000000000", path);
    expect(loadApiKey(path)).toBe("sk-freshfromui00000000000");
    expect(process.env.DEEPSEEK_API_KEY).toBe("sk-freshfromui00000000000");
  });

  it("loadApiKey returns undefined when nothing set", () => {
    expect(loadApiKey(path)).toBeUndefined();
  });

  it("isPlausibleKey accepts DeepSeek-shaped keys", () => {
    expect(isPlausibleKey("sk-1234567890abcdef")).toBe(true);
    expect(isPlausibleKey("sk-abcDEF_123-456789012")).toBe(true);
  });

  it("isPlausibleKey accepts non-sk tokens for self-hosted endpoints (issue #502)", () => {
    expect(isPlausibleKey("token-1234567890abcdef")).toBe(true);
    expect(isPlausibleKey("c8f5a3e2d1b9876543210fedcba98765")).toBe(true);
    expect(isPlausibleKey("Bearer_self_hosted_token_value_123")).toBe(true);
  });

  it("isPlausibleKey rejects empty / too-short / whitespace inputs", () => {
    expect(isPlausibleKey("")).toBe(false);
    expect(isPlausibleKey("hello")).toBe(false);
    expect(isPlausibleKey("sk-short")).toBe(false);
    expect(isPlausibleKey("has whitespace in the middle")).toBe(false);
  });

  it("loadBaseUrl prefers env var over config", () => {
    saveBaseUrl("https://from-config.example.com", path);
    process.env.DEEPSEEK_BASE_URL = "https://from-env.example.com";
    try {
      expect(loadBaseUrl(path)).toBe("https://from-env.example.com");
    } finally {
      // biome-ignore lint/performance/noDelete: restore exact env state
      delete process.env.DEEPSEEK_BASE_URL;
    }
  });

  it("loadBaseUrl falls back to config when env unset", () => {
    saveBaseUrl("https://self-hosted.example.com", path);
    expect(loadBaseUrl(path)).toBe("https://self-hosted.example.com");
  });

  it("loadBaseUrl accepts DEEPSEEK_API_BASE_URL as an alias (#1876)", () => {
    process.env.DEEPSEEK_API_BASE_URL = "https://nginx-proxy.internal/v1";
    expect(loadBaseUrl(path)).toBe("https://nginx-proxy.internal/v1");
  });

  it("loadBaseUrl: DEEPSEEK_BASE_URL wins over the alias when both are set", () => {
    process.env.DEEPSEEK_BASE_URL = "https://canonical.example.com";
    process.env.DEEPSEEK_API_BASE_URL = "https://alias.example.com";
    expect(loadBaseUrl(path)).toBe("https://canonical.example.com");
  });

  it("loadBaseUrl returns undefined when nothing set", () => {
    expect(loadBaseUrl(path)).toBeUndefined();
  });

  it("saveBaseUrl with empty string clears the field", () => {
    saveBaseUrl("https://self-hosted.example.com", path);
    saveBaseUrl("", path);
    expect(loadBaseUrl(path)).toBeUndefined();
  });

  it("loadEndpoint: config tuple wins when config sets baseUrl (#1631)", () => {
    // Bug scenario: user has a global env DEEPSEEK_API_KEY for the default
    // endpoint, then edits config to use a custom proxy with its own apiKey.
    // Per-field env-first would pair the stale env key with the custom URL →
    // auth fails. Tuple semantics keep them paired by source.
    process.env.DEEPSEEK_API_KEY = "sk-stale-from-shell-rc-abc";
    saveBaseUrl("https://new-api.example.com/v1", path);
    saveApiKey("sk-new-api-token-xyz1234", path);
    const ep = loadEndpoint(path);
    expect(ep.baseUrl).toBe("https://new-api.example.com/v1");
    expect(ep.apiKey).toBe("sk-new-api-token-xyz1234");
  });

  it("loadModel falls back to default when persisted id is unsupported on the official endpoint", () => {
    // Regression: v3-era `deepseek-chat`/`deepseek-reasoner` lingering in
    // config — or any other unsupported id — would be sent verbatim and
    // make the first chat request 400 with "supported API model names are
    // deepseek-v4-pro or deepseek-v4-flash, but you passed …".
    writeConfig({ model: "deepseek-chat" }, path);
    expect(loadModel(path)).toBe("deepseek-v4-flash");
    writeConfig({ model: "deepseek-made-up" }, path);
    expect(loadModel(path)).toBe("deepseek-v4-flash");
  });

  it("loadModel passes through any persisted id when a custom baseUrl is set", () => {
    writeConfig({ model: "my-self-hosted-7b", baseUrl: "https://self.example.com" }, path);
    expect(loadModel(path)).toBe("my-self-hosted-7b");
  });

  it("loadModel keeps a supported v4 id on the official endpoint", () => {
    writeConfig({ model: "deepseek-v4-pro" }, path);
    expect(loadModel(path)).toBe("deepseek-v4-pro");
  });

  it("loadEndpoint: env tuple wins when env sets baseUrl", () => {
    process.env.DEEPSEEK_BASE_URL = "https://env-proxy.example.com";
    process.env.DEEPSEEK_API_KEY = "sk-env-tuple-token-abc";
    // Write config directly — saveApiKey would mutate env as part of the desktop-UI
    // contract; here we want to test loadEndpoint's read precedence in isolation.
    writeConfig(
      { baseUrl: "https://config-only.example.com", apiKey: "sk-config-token-xyz1234" },
      path,
    );
    try {
      const ep = loadEndpoint(path);
      expect(ep.baseUrl).toBe("https://env-proxy.example.com");
      expect(ep.apiKey).toBe("sk-env-tuple-token-abc");
    } finally {
      // biome-ignore lint/performance/noDelete: restore exact env state
      delete process.env.DEEPSEEK_BASE_URL;
    }
  });

  it("loadEndpoint: default endpoint pairs env apiKey > config apiKey", () => {
    // Neither source sets baseUrl → default endpoint. Standard 12-factor
    // env > config for the apiKey on the read path.
    process.env.DEEPSEEK_API_KEY = "sk-env-default-token-abc";
    writeConfig({ apiKey: "sk-config-token-xyz1234" }, path);
    const ep = loadEndpoint(path);
    expect(ep.baseUrl).toBeUndefined();
    expect(ep.apiKey).toBe("sk-env-default-token-abc");
  });

  it("loadEndpoint: config baseUrl with no config apiKey returns undefined apiKey", () => {
    // Surfaces a clean "no key" error rather than silently using the stale
    // env key with the wrong endpoint.
    process.env.DEEPSEEK_API_KEY = "sk-stale-from-shell-rc-abc";
    saveBaseUrl("https://new-api.example.com/v1", path);
    const ep = loadEndpoint(path);
    expect(ep.baseUrl).toBe("https://new-api.example.com/v1");
    expect(ep.apiKey).toBeUndefined();
  });

  it("loads pricingOverride with valid non-negative fields", () => {
    writeConfig(
      {
        pricingOverride: {
          "third-party-model": { inputCacheHit: 0, inputCacheMiss: 1.5, output: 3 },
          invalid: { inputCacheHit: -1, inputCacheMiss: "bad" as unknown as number },
        },
      },
      path,
    );
    expect(loadPricingOverride(path)).toEqual({
      "third-party-model": { inputCacheHit: 0, inputCacheMiss: 1.5, output: 3 },
    });
  });

  it("loads positive integer rateLimit rpm only", () => {
    writeConfig({ rateLimit: { rpm: 30 } }, path);
    expect(loadRateLimit(path)).toEqual({ rpm: 30 });
    writeConfig({ rateLimit: { rpm: 0 } }, path);
    expect(loadRateLimit(path)).toBeUndefined();
    writeConfig({ rateLimit: { rpm: 1.5 } }, path);
    expect(loadRateLimit(path)).toBeUndefined();
  });

  it("loads proxy.disabled + proxy.noProxy[] when present, drops blank entries", () => {
    writeConfig(
      {
        proxy: {
          disabled: true,
          noProxy: ["internal.corp.example", "", "  ", ".workspace.lan"],
        },
      },
      path,
    );
    expect(loadProxyConfig(path)).toEqual({
      disabled: true,
      noProxy: ["internal.corp.example", ".workspace.lan"],
    });

    writeConfig({}, path);
    expect(loadProxyConfig(path)).toEqual({});
  });

  it("loads proxy.url and trims it; ignores blank values (#1868)", () => {
    writeConfig({ proxy: { url: "  http://127.0.0.1:7897  " } }, path);
    expect(loadProxyConfig(path)).toEqual({ url: "http://127.0.0.1:7897" });

    writeConfig({ proxy: { url: "   " } }, path);
    expect(loadProxyConfig(path)).toEqual({});
  });

  it("loads toolRateLimit with defaults and opt-out", () => {
    writeConfig(
      {
        toolRateLimit: {
          aggregate: { maxCalls: 5, windowSeconds: 10 },
          tools: {
            run_command: { maxCalls: 2, windowSeconds: 3 },
            run_background: false,
          },
        },
      },
      path,
    );
    expect(loadToolRateLimit(path)).toMatchObject({
      aggregate: { maxCalls: 5, windowSeconds: 10 },
      tools: {
        run_command: { maxCalls: 2, windowSeconds: 3 },
        run_background: false,
      },
    });

    writeConfig({ toolRateLimit: { enabled: false } }, path);
    expect(loadToolRateLimit(path)).toBe(false);

    writeConfig({ toolRateLimit: { aggregate: { maxCalls: 0, windowSeconds: 1.5 } } }, path);
    expect(loadToolRateLimit(path)).toMatchObject({
      aggregate: { maxCalls: 200, windowSeconds: 60 },
    });
  });

  it("loads mouseWheelRows when set, clamps to [1,10], drops invalid (#1494)", () => {
    writeConfig({ mouseWheelRows: 3 }, path);
    expect(loadMouseWheelRows(path)).toBe(3);

    writeConfig({ mouseWheelRows: 99 }, path);
    expect(loadMouseWheelRows(path)).toBe(10);

    writeConfig({ mouseWheelRows: 0 }, path);
    expect(loadMouseWheelRows(path)).toBeUndefined();

    writeConfig({ mouseWheelRows: -1 }, path);
    expect(loadMouseWheelRows(path)).toBeUndefined();

    writeConfig({ mouseWheelRows: 2.5 }, path);
    expect(loadMouseWheelRows(path)).toBeUndefined();

    writeConfig({ mouseWheelRows: "3" as unknown as number }, path);
    expect(loadMouseWheelRows(path)).toBeUndefined();

    writeConfig({}, path);
    expect(loadMouseWheelRows(path)).toBeUndefined();
  });

  it("loads proxy.bypassDeepSeekDirect when set (#1497)", () => {
    writeConfig({ proxy: { bypassDeepSeekDirect: false } }, path);
    expect(loadProxyConfig(path)).toEqual({ bypassDeepSeekDirect: false });

    writeConfig({ proxy: { bypassDeepSeekDirect: true } }, path);
    expect(loadProxyConfig(path)).toEqual({ bypassDeepSeekDirect: true });

    writeConfig({ proxy: { bypassDeepSeekDirect: "yes" } as never }, path);
    expect(loadProxyConfig(path)).toEqual({});
  });

  it("redactKey hides the middle", () => {
    expect(redactKey("sk-1234567890abcdefghij")).toBe("sk-123…ghij");
    expect(redactKey("short")).toBe("****");
    expect(redactKey("")).toBe("");
  });

  it("round-trips the full ReasonixConfig (model, effort, mcp, session, setupCompleted)", () => {
    writeConfig(
      {
        apiKey: "sk-test123abcdefghijkl",
        model: "deepseek-v4-pro",
        reasoningEffort: "medium",
        mcp: [
          "filesystem=npx -y @modelcontextprotocol/server-filesystem /tmp/safe",
          "memory=npx -y @modelcontextprotocol/server-memory",
        ],
        session: "work",
        setupCompleted: true,
      },
      path,
    );
    const loaded = readConfig(path);
    expect(loaded.model).toBe("deepseek-v4-pro");
    expect(loaded.reasoningEffort).toBe("medium");
    expect(loaded.mcp).toHaveLength(2);
    expect(loaded.session).toBe("work");
    expect(loaded.setupCompleted).toBe(true);
  });

  it("session: null in the config means the user opted out of persistence", () => {
    writeConfig({ apiKey: "sk-xxxxxxxxxxxxxxxxxxxx", session: null }, path);
    const loaded = readConfig(path);
    expect(loaded.session).toBeNull();
  });

  it("searchEnabled defaults to true with no config and no env", () => {
    expect(searchEnabled(path)).toBe(true);
  });

  it("searchEnabled honours `search: false` in the config file", () => {
    writeConfig({ apiKey: "sk-test123abcdefghijkl", search: false }, path);
    expect(searchEnabled(path)).toBe(false);
  });

  it("searchEnabled honours REASONIX_SEARCH=off/false/0", () => {
    process.env.REASONIX_SEARCH = "off";
    expect(searchEnabled(path)).toBe(false);
    process.env.REASONIX_SEARCH = "false";
    expect(searchEnabled(path)).toBe(false);
    process.env.REASONIX_SEARCH = "0";
    expect(searchEnabled(path)).toBe(false);
  });

  it("searchEnabled stays true for unrelated env values", () => {
    process.env.REASONIX_SEARCH = "on";
    expect(searchEnabled(path)).toBe(true);
  });

  it("env off beats config true", () => {
    writeConfig({ apiKey: "sk-test123abcdefghijkl", search: true }, path);
    process.env.REASONIX_SEARCH = "off";
    expect(searchEnabled(path)).toBe(false);
  });

  it("loadProjectShellAllowed returns [] when nothing stored", () => {
    expect(loadProjectShellAllowed("/some/project", path)).toEqual([]);
  });

  it("addProjectShellAllowed persists and dedups per project", () => {
    addProjectShellAllowed("/a", "npm install", path);
    addProjectShellAllowed("/a", "git commit", path);
    addProjectShellAllowed("/a", "npm install", path); // dedup
    addProjectShellAllowed("/b", "cargo add", path);
    expect(loadProjectShellAllowed("/a", path)).toEqual(["npm install", "git commit"]);
    expect(loadProjectShellAllowed("/b", path)).toEqual(["cargo add"]);
  });

  it("addProjectShellAllowed ignores empty / whitespace prefixes", () => {
    addProjectShellAllowed("/a", "", path);
    addProjectShellAllowed("/a", "   ", path);
    expect(loadProjectShellAllowed("/a", path)).toEqual([]);
  });

  it("removeProjectShellAllowed drops one entry by exact match", () => {
    addProjectShellAllowed("/a", "npm install", path);
    addProjectShellAllowed("/a", "git commit", path);
    expect(removeProjectShellAllowed("/a", "npm install", path)).toBe(true);
    expect(loadProjectShellAllowed("/a", path)).toEqual(["git commit"]);
  });

  it("removeProjectShellAllowed returns false when prefix isn't stored", () => {
    addProjectShellAllowed("/a", "npm install", path);
    expect(removeProjectShellAllowed("/a", "git commit", path)).toBe(false);
    expect(loadProjectShellAllowed("/a", path)).toEqual(["npm install"]);
  });

  it("removeProjectShellAllowed doesn't prefix-match (literal only)", () => {
    addProjectShellAllowed("/a", "git push origin main", path);
    expect(removeProjectShellAllowed("/a", "git push", path)).toBe(false);
    expect(loadProjectShellAllowed("/a", path)).toEqual(["git push origin main"]);
  });

  it("removeProjectShellAllowed scoped to project (doesn't leak across roots)", () => {
    addProjectShellAllowed("/a", "lint", path);
    addProjectShellAllowed("/b", "lint", path);
    expect(removeProjectShellAllowed("/a", "lint", path)).toBe(true);
    expect(loadProjectShellAllowed("/a", path)).toEqual([]);
    expect(loadProjectShellAllowed("/b", path)).toEqual(["lint"]);
  });

  it("clearProjectShellAllowed wipes one project, returns count, leaves others alone", () => {
    addProjectShellAllowed("/a", "lint", path);
    addProjectShellAllowed("/a", "test", path);
    addProjectShellAllowed("/b", "build", path);
    expect(clearProjectShellAllowed("/a", path)).toBe(2);
    expect(loadProjectShellAllowed("/a", path)).toEqual([]);
    expect(loadProjectShellAllowed("/b", path)).toEqual(["build"]);
  });

  it("clearProjectShellAllowed returns 0 when nothing stored", () => {
    expect(clearProjectShellAllowed("/empty", path)).toBe(0);
  });

  it("pathAllowed CRUD mirrors shellAllowed (load/add/dedup/remove/clear)", () => {
    expect(loadProjectPathAllowed("/a", path)).toEqual([]);
    addProjectPathAllowed("/a", "/Users/foo/Documents", path);
    addProjectPathAllowed("/a", "/etc", path);
    addProjectPathAllowed("/a", "/Users/foo/Documents", path); // dedup
    addProjectPathAllowed("/b", "/var/log", path);
    expect(loadProjectPathAllowed("/a", path)).toEqual(["/Users/foo/Documents", "/etc"]);
    expect(loadProjectPathAllowed("/b", path)).toEqual(["/var/log"]);
    expect(removeProjectPathAllowed("/a", "/etc", path)).toBe(true);
    expect(removeProjectPathAllowed("/a", "/etc", path)).toBe(false);
    expect(loadProjectPathAllowed("/a", path)).toEqual(["/Users/foo/Documents"]);
    expect(clearProjectPathAllowed("/a", path)).toBe(1);
    expect(loadProjectPathAllowed("/a", path)).toEqual([]);
    expect(loadProjectPathAllowed("/b", path)).toEqual(["/var/log"]);
  });

  it("pathAllowed coexists with shellAllowed on the same project entry", () => {
    addProjectShellAllowed("/proj", "npm install", path);
    addProjectPathAllowed("/proj", "/Users/foo", path);
    expect(loadProjectShellAllowed("/proj", path)).toEqual(["npm install"]);
    expect(loadProjectPathAllowed("/proj", path)).toEqual(["/Users/foo"]);
  });

  it.runIf(process.platform === "win32")(
    "matches project keys case-insensitively on Windows so cross-shell rootDir casing doesn't lose entries (#402)",
    () => {
      addProjectShellAllowed("F:\\Reasonix", "gh", path);
      expect(loadProjectShellAllowed("f:\\reasonix", path)).toContain("gh");
      expect(loadProjectShellAllowed("F:\\REASONIX", path)).toContain("gh");
      // Mutations through any-cased rootDir consolidate onto the original key.
      addProjectShellAllowed("f:\\reasonix", "deploy", path);
      expect(loadProjectShellAllowed("F:\\Reasonix", path)).toEqual(["gh", "deploy"]);
      expect(Object.keys(readConfig(path).projects ?? {})).toEqual(["F:\\Reasonix"]);
      expect(removeProjectShellAllowed("f:\\REASONIX", "gh", path)).toBe(true);
      expect(loadProjectShellAllowed("F:\\Reasonix", path)).toEqual(["deploy"]);
      expect(clearProjectShellAllowed("F:\\REASONIX", path)).toBe(1);
      expect(loadProjectShellAllowed("F:\\Reasonix", path)).toEqual([]);
    },
  );

  it.runIf(process.platform !== "win32")(
    "keeps project key matching case-sensitive on non-Windows platforms",
    () => {
      addProjectShellAllowed("/home/foo/repo", "gh", path);
      expect(loadProjectShellAllowed("/home/foo/repo", path)).toContain("gh");
      expect(loadProjectShellAllowed("/home/FOO/repo", path)).toEqual([]);
    },
  );

  it("loadEditMode defaults to 'review' when unset", () => {
    expect(loadEditMode(path)).toBe("review");
  });

  it("saveEditMode + loadEditMode round-trip 'auto'", () => {
    saveEditMode("auto", path);
    expect(loadEditMode(path)).toBe("auto");
    // Doesn't clobber other fields in the config.
    expect(readConfig(path).editMode).toBe("auto");
  });

  it("saveEditMode + loadEditMode round-trip 'yolo' (issue #644)", () => {
    saveEditMode("yolo", path);
    expect(loadEditMode(path)).toBe("yolo");
    expect(readConfig(path).editMode).toBe("yolo");
  });

  it("loadEditMode coerces unknown values back to 'review'", () => {
    writeConfig({ editMode: "garbage" as any }, path);
    expect(loadEditMode(path)).toBe("review");
  });

  it("loadEngineeringLifecycleMode defaults to 'off' when unset", () => {
    expect(loadEngineeringLifecycleMode(path)).toBe("off");
  });

  it("loadEngineeringLifecycleMode accepts off and strict", () => {
    writeConfig({ engineeringLifecycle: { mode: "off" } }, path);
    expect(loadEngineeringLifecycleMode(path)).toBe("off");
    writeConfig({ engineeringLifecycle: { mode: "strict" } }, path);
    expect(loadEngineeringLifecycleMode(path)).toBe("strict");
  });

  it("loadEngineeringLifecycleMode coerces unknown values back to 'off'", () => {
    writeConfig({ engineeringLifecycle: { mode: "garbage" as any } }, path);
    expect(loadEngineeringLifecycleMode(path)).toBe("off");
  });

  it("loadFilesystemOutlineThresholdBytes returns undefined when unset (caller applies default)", () => {
    expect(loadFilesystemOutlineThresholdBytes(path)).toBeUndefined();
  });

  it("loadFilesystemOutlineThresholdBytes accepts a positive integer", () => {
    writeConfig({ filesystem: { outlineThresholdBytes: 524288 } }, path);
    expect(loadFilesystemOutlineThresholdBytes(path)).toBe(524288);
  });

  it("loadFilesystemOutlineThresholdBytes ignores non-positive / non-numeric values", () => {
    writeConfig({ filesystem: { outlineThresholdBytes: 0 } }, path);
    expect(loadFilesystemOutlineThresholdBytes(path)).toBeUndefined();
    writeConfig({ filesystem: { outlineThresholdBytes: -1 } }, path);
    expect(loadFilesystemOutlineThresholdBytes(path)).toBeUndefined();
    writeConfig({ filesystem: { outlineThresholdBytes: "big" as any } }, path);
    expect(loadFilesystemOutlineThresholdBytes(path)).toBeUndefined();
  });

  it("loadReasoningEffort defaults to 'high' when unset (safe for vLLM / Azure)", () => {
    expect(loadReasoningEffort(path)).toBe("high");
  });

  it("saveReasoningEffort + loadReasoningEffort round-trip every supported value", () => {
    for (const e of ["low", "medium", "high", "max"] as const) {
      saveReasoningEffort(e, path);
      expect(loadReasoningEffort(path)).toBe(e);
      expect(readConfig(path).reasoningEffort).toBe(e);
    }
  });

  it("loadReasoningEffort coerces unknown values back to the safe default", () => {
    writeConfig({ reasoningEffort: "turbo" as any }, path);
    expect(loadReasoningEffort(path)).toBe("high");
  });

  it("saveReasoningEffort doesn't clobber other persisted fields", () => {
    saveEditMode("auto", path);
    saveReasoningEffort("high", path);
    expect(loadEditMode(path)).toBe("auto");
    expect(loadReasoningEffort(path)).toBe("high");
  });

  it("saveTheme + loadTheme round-trip a registered theme", () => {
    saveTheme("midnight", path);
    expect(loadTheme(path)).toBe("midnight");
    expect(readConfig(path).theme).toBe("midnight");
  });

  it("saveTheme + loadTheme round-trip auto", () => {
    saveTheme("auto", path);
    expect(loadTheme(path)).toBe("auto");
    expect(readConfig(path).theme).toBe("auto");
  });

  it("loadTheme returns undefined for invalid runtime values", () => {
    const invalidValues = ["unknown", null, false, 123, [], { name: "github-light" }];

    for (const theme of invalidValues) {
      writeConfig({ theme } as never, path);
      expect(loadTheme(path)).toBeUndefined();
    }
  });

  it("resolveThemePreference lets env override auto but not registered config themes", () => {
    expect(resolveThemePreference("auto", "light")).toBe("light");
    expect(resolveThemePreference(undefined, "midnight")).toBe("midnight");
    expect(resolveThemePreference("dark", "light")).toBe("dark");
    expect(resolveThemePreference("auto", "unknown")).toBe("dark");
  });

  it("saveTheme doesn't clobber other persisted fields", () => {
    saveEditMode("auto", path);
    saveTheme("light", path);
    expect(loadEditMode(path)).toBe("auto");
    expect(loadTheme(path)).toBe("light");
  });

  it("editModeHintShown defaults to false and toggles on markEditModeHintShown", () => {
    expect(editModeHintShown(path)).toBe(false);
    markEditModeHintShown(path);
    expect(editModeHintShown(path)).toBe(true);
    // Idempotent — calling again doesn't rewrite or clobber other fields.
    saveEditMode("auto", path);
    markEditModeHintShown(path);
    expect(editModeHintShown(path)).toBe(true);
    expect(loadEditMode(path)).toBe("auto");
  });

  it("round-trips semantic embedding config", () => {
    saveSemanticEmbeddingConfig(
      {
        provider: "openai-compat",
        openaiCompat: {
          baseUrl: "https://api.openai.com/v1",
          apiKey: "sk-openai1234567890abcd",
          model: "text-embedding-3-small",
          extraBody: { user: "reasonix" },
        },
      },
      path,
    );
    const loaded = loadSemanticEmbeddingUserConfig(path);
    expect(loaded.provider).toBe("openai-compat");
    expect(loaded.openaiCompat?.baseUrl).toBe("https://api.openai.com/v1");
    expect(loaded.openaiCompat?.extraBody).toEqual({ user: "reasonix" });
  });

  it("resolves ollama by default when semantic config is absent", () => {
    const resolved = resolveSemanticEmbeddingConfig(path);
    expect(resolved.provider).toBe("ollama");
    expect(resolved.baseUrl).toBe("http://localhost:11434");
    expect(resolved.model).toBe("nomic-embed-text");
  });

  it("resolves ollama defaults from an existing empty config file", () => {
    writeConfig({}, path);
    const resolved = resolveSemanticEmbeddingConfig(path);
    expect(resolved.provider).toBe("ollama");
    expect(resolved.baseUrl).toBe("http://localhost:11434");
    expect(resolved.model).toBe("nomic-embed-text");
  });

  it("resolves ollama defaults when semantic.provider is missing", () => {
    writeConfig(
      {
        semantic: {
          ollama: {
            model: "",
          },
          openaiCompat: {
            baseUrl: "https://api.example.com/v1/embeddings",
            apiKey: "sk-openai1234567890abcd",
            model: "bge-m3",
          },
        },
      },
      path,
    );
    const resolved = resolveSemanticEmbeddingConfig(path);
    expect(resolved.provider).toBe("ollama");
    expect(resolved.baseUrl).toBe("http://localhost:11434");
    expect(resolved.model).toBe("nomic-embed-text");
  });

  it("accepts semantic API URLs that already include /embeddings", () => {
    saveSemanticEmbeddingConfig(
      {
        provider: "openai-compat",
        openaiCompat: {
          baseUrl: "https://api.openai.com/v1/embeddings",
          apiKey: "sk-openai1234567890abcd",
          model: "text-embedding-3-small",
        },
      },
      path,
    );
    const resolved = resolveSemanticEmbeddingConfig(path);
    expect(resolved.provider).toBe("openai-compat");
    expect(resolved.baseUrl).toBe("https://api.openai.com/v1/embeddings");
  });

  it("redacts openai-compatible api keys in semantic config views", () => {
    saveSemanticEmbeddingConfig(
      {
        provider: "openai-compat",
        openaiCompat: {
          baseUrl: "https://api.openai.com/v1",
          apiKey: "sk-openai1234567890abcd",
          model: "text-embedding-3-small",
        },
      },
      path,
    );
    const view = redactSemanticEmbeddingConfig(loadSemanticEmbeddingUserConfig(path));
    expect(view.openaiCompat.apiKeySet).toBe(true);
    expect(view.openaiCompat.apiKey).not.toBe("sk-openai1234567890abcd");
    expect(view.openaiCompat.apiKey).toContain("…");
  });

  it("rejects non-object semantic extraBody", () => {
    expect(() =>
      saveSemanticEmbeddingConfig(
        {
          provider: "openai-compat",
          openaiCompat: {
            baseUrl: "https://api.openai.com/v1",
            apiKey: "sk-openai1234567890abcd",
            model: "text-embedding-3-small",
            extraBody: [] as unknown as Record<string, unknown>,
          },
        },
        path,
      ),
    ).toThrow(/JSON object/);
  });

  describe("desktopOpenTabs — issues #933, #1244", () => {
    it("returns [] when unset", () => {
      expect(loadDesktopOpenTabs(path)).toEqual([]);
    });

    it("round-trips dir + session + active", () => {
      saveDesktopOpenTabs([{ dir: "/a", session: "s-a", active: true }, { dir: "/b" }], path);
      expect(loadDesktopOpenTabs(path)).toEqual([
        { dir: "/a", session: "s-a", active: true },
        { dir: "/b" },
      ]);
    });

    it("reads the legacy bare-string format", () => {
      writeConfig({ desktopOpenTabs: ["/a", "/b"] as unknown as DesktopOpenTab[] }, path);
      expect(loadDesktopOpenTabs(path)).toEqual([{ dir: "/a" }, { dir: "/b" }]);
    });

    it("filters out empty / malformed entries on read", () => {
      writeConfig(
        {
          desktopOpenTabs: [{ dir: "/a" }, { dir: "" }, null, "/b"] as unknown as DesktopOpenTab[],
        },
        path,
      );
      expect(loadDesktopOpenTabs(path)).toEqual([{ dir: "/a" }, { dir: "/b" }]);
    });

    it("clears the key when saving an empty list", () => {
      saveDesktopOpenTabs([{ dir: "/a" }], path);
      saveDesktopOpenTabs([], path);
      expect(readConfig(path).desktopOpenTabs).toBeUndefined();
    });

    it("preserves order across multiple saves (tab reordering)", () => {
      saveDesktopOpenTabs([{ dir: "/a" }, { dir: "/b" }, { dir: "/c" }], path);
      saveDesktopOpenTabs([{ dir: "/c" }, { dir: "/a" }, { dir: "/b" }], path);
      expect(loadDesktopOpenTabs(path)).toEqual([{ dir: "/c" }, { dir: "/a" }, { dir: "/b" }]);
    });
  });

  describe("webSearchEngine", () => {
    it("preserves each known engine end-to-end (no silent tavily→default fall-through, #1309)", () => {
      for (const engine of [
        "bing",
        "searxng",
        "metaso",
        "tavily",
        "perplexity",
        "exa",
        "brave",
        "ollama",
      ] as const) {
        writeConfig({ webSearchEngine: engine }, path);
        expect(webSearchEngine(path)).toBe(engine);
      }
    });

    it("defaults to bing when unset or unknown", () => {
      expect(webSearchEngine(path)).toBe("bing");
      writeConfig({ webSearchEngine: "garbage" as unknown as "bing" }, path);
      expect(webSearchEngine(path)).toBe("bing");
    });

    it('legacy "mojeek" config value reads back as bing (read-only migration)', () => {
      // Old configs predating the bing-default swap still have "mojeek" on disk.
      // Loader maps unknown values to bing; user's config file isn't rewritten,
      // so an explicit `/search-engine mojeek` later still rejects loudly.
      writeConfig({ webSearchEngine: "mojeek" as unknown as "bing" }, path);
      expect(webSearchEngine(path)).toBe("bing");
    });
  });

  describe("loadBraveApiKey", () => {
    it("returns BRAVE_SEARCH_API_KEY env var when set", () => {
      const orig = process.env.BRAVE_SEARCH_API_KEY;
      process.env.BRAVE_SEARCH_API_KEY = "bsk-123";
      try {
        expect(loadBraveApiKey(path)).toBe("bsk-123");
      } finally {
        // biome-ignore lint/performance/noDelete: env var must be absent, not "undefined"
        if (orig === undefined) delete process.env.BRAVE_SEARCH_API_KEY;
        else process.env.BRAVE_SEARCH_API_KEY = orig;
      }
    });

    it("falls back to BRAVE_API_KEY when BRAVE_SEARCH_API_KEY is unset", () => {
      const origLong = process.env.BRAVE_SEARCH_API_KEY;
      const origShort = process.env.BRAVE_API_KEY;
      // biome-ignore lint/performance/noDelete: env var must be absent, not "undefined"
      delete process.env.BRAVE_SEARCH_API_KEY;
      process.env.BRAVE_API_KEY = "bak-456";
      try {
        expect(loadBraveApiKey(path)).toBe("bak-456");
      } finally {
        if (origLong !== undefined) process.env.BRAVE_SEARCH_API_KEY = origLong;
        // biome-ignore lint/performance/noDelete: same reason
        if (origShort === undefined) delete process.env.BRAVE_API_KEY;
        else process.env.BRAVE_API_KEY = origShort;
      }
    });

    it("falls back to config.braveApiKey when no env vars are set", () => {
      const origLong = process.env.BRAVE_SEARCH_API_KEY;
      const origShort = process.env.BRAVE_API_KEY;
      // biome-ignore lint/performance/noDelete: env var must be absent, not "undefined"
      delete process.env.BRAVE_SEARCH_API_KEY;
      // biome-ignore lint/performance/noDelete: same reason
      delete process.env.BRAVE_API_KEY;
      try {
        writeConfig({ braveApiKey: "cfg-brave" }, path);
        expect(loadBraveApiKey(path)).toBe("cfg-brave");
      } finally {
        if (origLong !== undefined) process.env.BRAVE_SEARCH_API_KEY = origLong;
        if (origShort !== undefined) process.env.BRAVE_API_KEY = origShort;
      }
    });

    it("returns undefined when nothing is set", () => {
      const origLong = process.env.BRAVE_SEARCH_API_KEY;
      const origShort = process.env.BRAVE_API_KEY;
      // biome-ignore lint/performance/noDelete: env var must be absent, not "undefined"
      delete process.env.BRAVE_SEARCH_API_KEY;
      // biome-ignore lint/performance/noDelete: same reason
      delete process.env.BRAVE_API_KEY;
      try {
        writeConfig({ braveApiKey: undefined }, path);
        expect(loadBraveApiKey(path)).toBeUndefined();
      } finally {
        if (origLong !== undefined) process.env.BRAVE_SEARCH_API_KEY = origLong;
        if (origShort !== undefined) process.env.BRAVE_API_KEY = origShort;
      }
    });
  });

  describe("subagentModels", () => {
    it("round-trips flash/pro entries", () => {
      saveSubagentModels({ explore: "pro", review: "flash" }, path);
      expect(loadSubagentModels(path)).toEqual({ explore: "pro", review: "flash" });
    });

    it("drops unknown values without touching valid entries", () => {
      writeConfig(
        {
          subagentModels: {
            explore: "pro",
            bogus: "fast" as any,
          },
        },
        path,
      );
      expect(loadSubagentModels(path)).toEqual({ explore: "pro" });
    });

    it("clearing all entries removes the field from config", () => {
      saveSubagentModels({ explore: "pro" }, path);
      saveSubagentModels({}, path);
      expect(loadSubagentModels(path)).toEqual({});
      expect(readConfig(path).subagentModels).toBeUndefined();
    });
  });
});
