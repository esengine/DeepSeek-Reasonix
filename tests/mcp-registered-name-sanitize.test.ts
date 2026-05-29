import { describe, expect, it } from "vitest";
import type { McpClient } from "../src/mcp/client.js";
import { bridgeMcpTools, sanitizeRegisteredName } from "../src/mcp/registry.js";
import type { ListToolsResult } from "../src/mcp/types.js";

const API_TOOL_NAME = /^[a-zA-Z0-9_-]{1,64}$/;

function client(tools: ListToolsResult["tools"], onCall?: (name: string) => void): McpClient {
  return {
    listTools: async () => ({ tools }),
    callTool: async (name: string) => {
      onCall?.(name);
      return { content: [{ type: "text", text: "ok" }] };
    },
  } as unknown as McpClient;
}

describe("MCP registered-name sanitization", () => {
  it("rewrites '/' to an API-safe name and records the rename", async () => {
    const bridged = await bridgeMcpTools(
      client([{ name: "unity/health", inputSchema: { type: "object" } }]),
    );

    expect(bridged.registeredNames).toEqual(["unity_health"]);
    expect(bridged.renamed).toEqual([{ from: "unity/health", to: "unity_health" }]);
  });

  it("dispatches to the original server endpoint, not the sanitized name", async () => {
    let calledWith: string | undefined;
    const bridged = await bridgeMcpTools(
      client([{ name: "unity/health", inputSchema: { type: "object" } }], (n) => {
        calledWith = n;
      }),
    );

    await bridged.registry.dispatch("unity_health", "{}");
    expect(calledWith).toBe("unity/health");
  });

  it("auto-suffixes a colliding sanitized name (deterministic via shared registry)", async () => {
    const first = await bridgeMcpTools(client([{ name: "a_b", inputSchema: { type: "object" } }]));
    const second = await bridgeMcpTools(
      client([{ name: "a/b", inputSchema: { type: "object" } }]),
      { registry: first.registry },
    );

    expect(first.registeredNames).toEqual(["a_b"]);
    expect(second.registeredNames).toEqual(["a_b_2"]);
    expect(second.renamed).toEqual([{ from: "a/b", to: "a_b_2" }]);
    expect(first.registry.has("a_b")).toBe(true);
    expect(first.registry.has("a_b_2")).toBe(true);
  });

  it("caps an over-length name at 64 chars while staying in the API charset", async () => {
    const longName = "x".repeat(70);
    const bridged = await bridgeMcpTools(
      client([{ name: longName, inputSchema: { type: "object" } }]),
    );

    const [registered] = bridged.registeredNames;
    expect(registered).toBeDefined();
    expect(registered!.length).toBeLessThanOrEqual(64);
    expect(registered).toMatch(API_TOOL_NAME);
    expect(bridged.renamed).toHaveLength(1);
  });

  it("keeps every registered MCP name within the API charset", async () => {
    const bridged = await bridgeMcpTools(
      client([
        { name: "unity/changes/apply", inputSchema: { type: "object" } },
        { name: "unity/project/info", inputSchema: { type: "object" } },
        { name: "read_file", inputSchema: { type: "object" } },
      ]),
    );

    for (const name of bridged.registeredNames) {
      expect(name).toMatch(API_TOOL_NAME);
    }
  });

  it("leaves an already-valid name untouched and out of renamed", async () => {
    const bridged = await bridgeMcpTools(
      client([{ name: "read_file", inputSchema: { type: "object" } }]),
    );

    expect(bridged.registeredNames).toEqual(["read_file"]);
    expect(bridged.renamed).toEqual([]);
  });

  it("sanitizeRegisteredName replaces only out-of-charset characters", () => {
    expect(sanitizeRegisteredName("unity/health")).toBe("unity_health");
    expect(sanitizeRegisteredName("a.b c:d")).toBe("a_b_c_d");
    expect(sanitizeRegisteredName("already-valid_NAME-9")).toBe("already-valid_NAME-9");
  });
});
