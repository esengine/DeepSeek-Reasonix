import { describe, expect, it } from "vitest";
import { getAllMcpSpecs } from "../src/cli/commands/desktop.js";
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
});
