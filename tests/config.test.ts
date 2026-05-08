import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  addProjectShellAllowed,
  clearProjectShellAllowed,
  editModeHintShown,
  isPlausibleKey,
  loadApiKey,
  loadEditMode,
  loadIndexConfig,
  loadIndexUserConfig,
  loadProjectShellAllowed,
  loadReasoningEffort,
  loadSemanticEmbeddingUserConfig,
  markEditModeHintShown,
  readConfig,
  redactKey,
  redactSemanticEmbeddingConfig,
  removeProjectShellAllowed,
  resolveSemanticEmbeddingConfig,
  saveApiKey,
  saveEditMode,
  saveIndexConfig,
  saveReasoningEffort,
  saveSemanticEmbeddingConfig,
  searchEnabled,
  writeConfig,
} from "../src/config.js";

describe("config", () => {
  let dir: string;
  let path: string;
  const originalEnv = process.env.DEEPSEEK_API_KEY;
  const originalSearch = process.env.REASONIX_SEARCH;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-test-"));
    path = join(dir, "config.json");
    // biome-ignore lint/performance/noDelete: the string "undefined" leaks into process.env otherwise
    delete process.env.DEEPSEEK_API_KEY;
    // biome-ignore lint/performance/noDelete: same reason
    delete process.env.REASONIX_SEARCH;
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
  });

  it("readConfig returns {} when file is missing", () => {
    expect(readConfig(path)).toEqual({});
  });

  it("writeConfig + readConfig round-trip", () => {
    writeConfig({ apiKey: "sk-test123abcdefghijkl" }, path);
    expect(readConfig(path).apiKey).toBe("sk-test123abcdefghijkl");
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

  it("loadApiKey returns undefined when nothing set", () => {
    expect(loadApiKey(path)).toBeUndefined();
  });

  it("isPlausibleKey accepts valid sk- keys", () => {
    expect(isPlausibleKey("sk-1234567890abcdef")).toBe(true);
    expect(isPlausibleKey("sk-abcDEF_123-456789012")).toBe(true);
  });

  it("isPlausibleKey rejects bad input", () => {
    expect(isPlausibleKey("")).toBe(false);
    expect(isPlausibleKey("hello")).toBe(false);
    expect(isPlausibleKey("sk-short")).toBe(false);
    expect(isPlausibleKey("token-1234567890abcdef")).toBe(false);
  });

  it("redactKey hides the middle", () => {
    expect(redactKey("sk-1234567890abcdefghij")).toBe("sk-123…ghij");
    expect(redactKey("short")).toBe("****");
    expect(redactKey("")).toBe("");
  });

  it("round-trips the full ReasonixConfig (preset, mcp, session, setupCompleted)", () => {
    writeConfig(
      {
        apiKey: "sk-test123abcdefghijkl",
        preset: "smart",
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
    expect(loaded.preset).toBe("smart");
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

  it("loadEditMode coerces unknown values back to 'review'", () => {
    writeConfig({ editMode: "garbage" as any }, path);
    expect(loadEditMode(path)).toBe("review");
  });

  it("loadReasoningEffort defaults to 'max' when unset", () => {
    expect(loadReasoningEffort(path)).toBe("max");
  });

  it("saveReasoningEffort + loadReasoningEffort round-trip 'high'", () => {
    saveReasoningEffort("high", path);
    expect(loadReasoningEffort(path)).toBe("high");
    expect(readConfig(path).reasoningEffort).toBe("high");
  });

  it("loadReasoningEffort coerces unknown values back to 'max'", () => {
    writeConfig({ reasoningEffort: "turbo" as any }, path);
    expect(loadReasoningEffort(path)).toBe("max");
  });

  it("saveReasoningEffort doesn't clobber other persisted fields", () => {
    saveEditMode("auto", path);
    saveReasoningEffort("high", path);
    expect(loadEditMode(path)).toBe("auto");
    expect(loadReasoningEffort(path)).toBe("high");
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
});
