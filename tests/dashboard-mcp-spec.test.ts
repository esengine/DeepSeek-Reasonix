import { describe, expect, it } from "vitest";
import { mcpSpecCommand, mcpSpecLabel, normalizeMcpSpec } from "../dashboard/src/lib/mcp-spec.js";

describe("dashboard MCP spec display helpers", () => {
  it("keeps legacy string specs unchanged", () => {
    const spec = "fs=npx -y @modelcontextprotocol/server-filesystem";

    expect(normalizeMcpSpec(spec)).toBe(spec);
    expect(mcpSpecLabel(spec)).toBe("fs");
    expect(mcpSpecCommand(spec)).toBe("npx -y @modelcontextprotocol/server-filesystem");
  });

  it("accepts object specs from attached dashboard payloads without throwing", () => {
    const spec = { raw: "browser=npx -y @mcp/browser", status: "configured" };

    expect(normalizeMcpSpec(spec)).toBe("browser=npx -y @mcp/browser");
    expect(mcpSpecLabel(spec)).toBe("browser");
    expect(mcpSpecCommand(spec)).toBe("npx -y @mcp/browser");
  });

  it("derives a display string from structured command specs", () => {
    const spec = { name: "git", command: "uvx", args: ["mcp-git", "--repository", "."] };

    expect(normalizeMcpSpec(spec)).toBe("git=uvx mcp-git --repository .");
    expect(mcpSpecLabel(spec)).toBe("git");
    expect(mcpSpecCommand(spec)).toBe("uvx mcp-git --repository .");
  });

  it("drops unrecognized object specs instead of stringifying them", () => {
    const spec = { env: { API_KEY: "secret" } };

    expect(normalizeMcpSpec(spec)).toBeNull();
    expect(mcpSpecLabel(spec)).toBe("");
    expect(mcpSpecCommand(spec)).toBe("");
  });
});
