import { describe, expect, it } from "vitest";
import {
  applyImportedMcpServersToConfig,
  applyMcpSpecUpdateToConfig,
  classifyMcpStatusReason,
  getAllMcpSpecs,
  normalizeImportedMcpServer,
  stripLegacyMcpConfigForRaw,
} from "../src/cli/commands/desktop.js";
import type { ReasonixConfig } from "../src/config.js";

describe("getAllMcpSpecs", () => {
  it("returns legacy cfg.mcp specs", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp", "git=uvx mcp-server-git"],
    };
    const specs = getAllMcpSpecs(cfg);
    expect(specs).toHaveLength(2);
    expect(specs).toContain("fs=npx -y @scope/fs /tmp");
    expect(specs).toContain("git=uvx mcp-server-git");
  });

  it("returns mcpServers specs when legacy mcp is absent", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        github: {
          command: "npx",
          args: ["-y", "@modelcontextprotocol/server-github"],
        },
      },
    };
    const specs = getAllMcpSpecs(cfg);
    expect(specs.length).toBeGreaterThan(0);
    expect(specs.some((s) => s.startsWith("github="))).toBe(true);
  });

  it("merges both legacy mcp and mcpServers", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp"],
      mcpServers: {
        github: {
          command: "npx",
          args: ["-y", "@modelcontextprotocol/server-github"],
        },
      },
    };
    const specs = getAllMcpSpecs(cfg);
    expect(specs.length).toBeGreaterThanOrEqual(2);
    expect(specs.some((s) => s.startsWith("fs="))).toBe(true);
    expect(specs.some((s) => s.startsWith("github="))).toBe(true);
  });

  it("mcpServers wins on name conflict", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp"],
      mcpServers: {
        fs: {
          command: "node",
          args: ["server.js"],
        },
      },
    };
    const specs = getAllMcpSpecs(cfg);
    const fsSpec = specs.find((s) => s.startsWith("fs="));
    expect(fsSpec).toContain("node");
    expect(fsSpec).not.toContain("npx");
  });

  it("returns empty array when neither mcp nor mcpServers present", () => {
    const cfg: ReasonixConfig = {};
    const specs = getAllMcpSpecs(cfg);
    expect(specs).toEqual([]);
  });

  it("removes the edited anonymous legacy spec by normalized raw form", () => {
    const cfg: ReasonixConfig = {
      mcp: ["  npx   -y   @scope/fs   /tmp  ", "git=uvx mcp-server-git"],
    };

    stripLegacyMcpConfigForRaw(cfg, "npx -y @scope/fs /tmp");

    expect(cfg.mcp).toEqual(["git=uvx mcp-server-git"]);
  });
});

describe("normalizeImportedMcpServer", () => {
  it("normalizes stdio servers and drops invalid optional fields", () => {
    const normalized = normalizeImportedMcpServer({
      name: " fs ",
      transport: "stdio",
      command: " npx ",
      args: ["-y", 42, "@scope/fs"],
      env: { TOKEN: "abc", EMPTY: "", BAD: 1 },
      cwd: " /workspace ",
      disabled: true,
      requestTimeoutMs: 12_000,
    });

    expect(normalized?.name).toBe("fs");
    expect(normalized?.config).toMatchObject({
      transport: "stdio",
      command: "npx",
      args: ["-y", "@scope/fs"],
      env: { TOKEN: "abc" },
      cwd: "/workspace",
      disabled: true,
      requestTimeoutMs: 12_000,
    });
  });

  it("normalizes URL transports and headers", () => {
    const normalized = normalizeImportedMcpServer({
      name: "remote",
      transport: "streamable-http",
      url: " https://mcp.example.test/api ",
      headers: { Authorization: "Bearer token", Empty: "", Bad: 1 },
      requestTimeoutMs: Number.POSITIVE_INFINITY,
    });

    expect(normalized?.config).toMatchObject({
      transport: "streamable-http",
      url: "https://mcp.example.test/api",
      headers: { Authorization: "Bearer token" },
    });
    expect(normalized?.config.requestTimeoutMs).toBeUndefined();
  });

  it("rejects unnamed servers and missing transport payloads", () => {
    expect(
      normalizeImportedMcpServer({ name: " ", transport: "stdio", command: "npx" }),
    ).toBeNull();
    expect(normalizeImportedMcpServer({ name: "fs", transport: "stdio" })).toBeNull();
    expect(normalizeImportedMcpServer({ name: "remote", transport: "sse" })).toBeNull();
  });
});

describe("desktop MCP config mutations", () => {
  it("imports valid servers into mcpServers and removes matching legacy entries", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp", " npx   -y   @scope/fs   /tmp ", "keep=uvx keep"],
      mcpDisabled: ["fs", "keep"],
      mcpEnv: { fs: { TOKEN: "old" }, keep: { TOKEN: "keep" } },
      mcpServers: {
        existing: { command: "uvx", args: ["existing"] },
      },
    };

    const result = applyImportedMcpServersToConfig(cfg, [
      { name: "fs", transport: "stdio", command: "npx", args: ["-y", "@scope/fs", "/tmp"] },
    ]);

    expect(cfg.mcpServers?.existing).toEqual({ command: "uvx", args: ["existing"] });
    expect(cfg.mcpServers?.fs).toMatchObject({
      transport: "stdio",
      command: "npx",
      args: ["-y", "@scope/fs", "/tmp"],
    });
    expect(cfg.mcp).toEqual(["keep=uvx keep"]);
    expect(cfg.mcpDisabled).toEqual(["keep"]);
    expect(cfg.mcpEnv).toEqual({ keep: { TOKEN: "keep" } });
    expect(result.forceSpecs).toEqual(["fs=npx -y @scope/fs /tmp"]);
  });

  it("rejects imports with no valid servers", () => {
    const cfg: ReasonixConfig = {};

    expect(() =>
      applyImportedMcpServersToConfig(cfg, [{ name: "fs", transport: "stdio" }]),
    ).toThrow("no valid servers received");
    expect(cfg).toEqual({});
  });

  it("updates renamed servers by deleting the old canonical name and legacy duplicates", () => {
    const cfg: ReasonixConfig = {
      mcp: ["old=npx old-server", "new=node server.js", " node   server.js ", "keep=uvx keep"],
      mcpDisabled: ["old", "new", "keep"],
      mcpEnv: { old: { TOKEN: "old" }, new: { TOKEN: "new" }, keep: { TOKEN: "keep" } },
      mcpServers: {
        old: { command: "npx", args: ["old-server"] },
        keep: { command: "uvx", args: ["keep"] },
      },
    };

    const result = applyMcpSpecUpdateToConfig(cfg, "old=npx old-server", {
      name: "new",
      transport: "stdio",
      command: "node",
      args: ["server.js"],
    });

    expect(cfg.mcpServers?.old).toBeUndefined();
    expect(cfg.mcpServers?.new).toMatchObject({
      transport: "stdio",
      command: "node",
      args: ["server.js"],
    });
    expect(cfg.mcpServers?.keep).toEqual({ command: "uvx", args: ["keep"] });
    expect(cfg.mcp).toEqual(["keep=uvx keep"]);
    expect(cfg.mcpDisabled).toEqual(["keep"]);
    expect(cfg.mcpEnv).toEqual({ keep: { TOKEN: "keep" } });
    expect(result).toEqual({
      updatedRaw: "new=node server.js",
      forceSpecs: ["old=npx old-server", "new=node server.js"],
    });
  });

  it("rejects invalid spec updates without mutating config", () => {
    const cfg: ReasonixConfig = {
      mcpServers: { fs: { command: "npx", args: ["old"] } },
    };

    expect(() => applyMcpSpecUpdateToConfig(cfg, "fs=npx old", { name: "fs" })).toThrow(
      "invalid server config",
    );
    expect(cfg).toEqual({ mcpServers: { fs: { command: "npx", args: ["old"] } } });
  });
});

describe("classifyMcpStatusReason", () => {
  it("maps common failure strings to stable UI hints", () => {
    expect(classifyMcpStatusReason(undefined)).toBeUndefined();
    expect(classifyMcpStatusReason("No bearer token configured")).toBe("missing-token");
    expect(classifyMcpStatusReason("HTTP 401 unauthorized")).toBe("auth");
    expect(classifyMcpStatusReason("spawn npx ENOENT")).toBe("command");
    expect(classifyMcpStatusReason("fetch failed due to DNS timeout")).toBe("network");
    expect(classifyMcpStatusReason("protocol closed during initialize")).toBe("unknown");
  });
});
