import { describe, expect, it } from "vitest";
import type { McpClient } from "../src/mcp/client.js";
import { bridgeMcpTools, canonicalizeSchemaForCache } from "../src/mcp/registry.js";
import type { ListToolsResult } from "../src/mcp/types.js";

function client(tools: ListToolsResult["tools"]): McpClient {
  return {
    listTools: async () => ({ tools }),
    callTool: async () => ({ content: [{ type: "text", text: "ok" }] }),
  } as unknown as McpClient;
}

describe("MCP cache canonicalization", () => {
  it("sorts bridged tools by final registered name", async () => {
    const bridged = await bridgeMcpTools(
      client([
        { name: "zeta", inputSchema: { type: "object" } },
        { name: "alpha", inputSchema: { type: "object" } },
      ]),
      { namePrefix: "srv_" },
    );

    expect(bridged.registeredNames).toEqual(["srv_alpha", "srv_zeta"]);
    expect(bridged.registry.specs().map((spec) => spec.function.name)).toEqual([
      "srv_alpha",
      "srv_zeta",
    ]);
  });

  it("stabilizes object keys and required arrays while preserving enum order", () => {
    const canonical = canonicalizeSchemaForCache({
      required: ["b", "a"],
      properties: {
        b: { enum: ["z", "a"], type: "string" },
        a: { type: "string" },
      },
      type: "object",
    });

    expect(JSON.stringify(canonical)).toBe(
      JSON.stringify({
        properties: {
          a: { type: "string" },
          b: { enum: ["z", "a"], type: "string" },
        },
        required: ["a", "b"],
        type: "object",
      }),
    );
  });

  it("sorts dependentRequired inner arrays for cache stability", () => {
    const canonical = canonicalizeSchemaForCache({
      dependentRequired: {
        b: ["z", "a"],
        a: ["y", "x"],
      },
      type: "object",
    });

    expect(JSON.stringify(canonical)).toBe(
      JSON.stringify({
        dependentRequired: {
          a: ["x", "y"],
          b: ["a", "z"],
        },
        type: "object",
      }),
    );
  });

  it("handles nested arrays within dependentRequired", () => {
    const canonical = canonicalizeSchemaForCache({
      dependentRequired: {
        credit_card: ["number", "expiry", "cvv"],
        name: ["first", "last"],
      },
      type: "object",
    });

    const parsed = JSON.parse(JSON.stringify(canonical));
    expect(Object.keys(parsed.dependentRequired)).toEqual(["credit_card", "name"]);
    expect(parsed.dependentRequired.credit_card).toEqual(["cvv", "expiry", "number"]);
    expect(parsed.dependentRequired.name).toEqual(["first", "last"]);
  });
});
