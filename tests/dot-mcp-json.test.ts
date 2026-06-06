import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { DOT_MCP_JSON, loadDotMcpJson } from "../src/mcp/dot-mcp-json.js";

describe("loadDotMcpJson", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-dotmcp-"));
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("returns undefined when .mcp.json is absent", () => {
    expect(loadDotMcpJson(root)).toBeUndefined();
  });

  it("reads a Claude-shape mcpServers block (stdio + http + sse)", () => {
    writeFileSync(
      join(root, DOT_MCP_JSON),
      JSON.stringify({
        mcpServers: {
          local: { type: "stdio", command: "node", args: ["server.js"] },
          gh: { type: "http", url: "https://api.githubcopilot.com/mcp/" },
          events: { type: "sse", url: "https://example.com/sse" },
        },
      }),
      "utf8",
    );
    const out = loadDotMcpJson(root);
    expect(out).toBeDefined();
    expect(Object.keys(out!)).toEqual(["local", "gh", "events"]);
    expect(out!.local!.command).toBe("node");
    expect(out!.gh!.url).toBe("https://api.githubcopilot.com/mcp/");
  });

  it("returns undefined for malformed JSON", () => {
    writeFileSync(join(root, DOT_MCP_JSON), "{ not json", "utf8");
    expect(loadDotMcpJson(root)).toBeUndefined();
  });

  it("returns undefined when mcpServers is missing", () => {
    writeFileSync(join(root, DOT_MCP_JSON), JSON.stringify({ other: "value" }), "utf8");
    expect(loadDotMcpJson(root)).toBeUndefined();
  });

  it("skips non-object entries inside mcpServers", () => {
    writeFileSync(
      join(root, DOT_MCP_JSON),
      JSON.stringify({
        mcpServers: {
          bad: "not an object",
          good: { type: "stdio", command: "node" },
        },
      }),
      "utf8",
    );
    const out = loadDotMcpJson(root);
    expect(Object.keys(out!)).toEqual(["good"]);
  });
});
