import { describe, expect, it } from "vitest";

import { shortArgs } from "./args";
import { categoryOf, labelFor } from "./icons";

const PROXY_CALL = JSON.stringify({
  action: "call",
  capability_id: "tool:remember",
  arguments: { description: "网关的 401 是瞬时态", body: "退避重试即可", scope: "project" },
});

describe("a proxied call reads as the capability it reached", () => {
  it("summarises the nested call rather than the proxy envelope", () => {
    expect(shortArgs(PROXY_CALL)).toBe("网关的 401 是瞬时态");
  });

  it("still reads an ordinary call from its own top level", () => {
    expect(shortArgs(JSON.stringify({ command: "go test ./..." }))).toBe("go test ./...");
  });

  it("says nothing when neither level names anything readable", () => {
    expect(shortArgs(JSON.stringify({ action: "list" }))).toBe("");
    expect(shortArgs(JSON.stringify({ action: "call", arguments: { limit: 5 } }))).toBe("");
  });

  // The proxy is one tool with one name; what it reached decides the heading
  // and the hue. Reading the proxy's own name draws every capability as MCP.
  it("takes its heading and category from the resolved name", () => {
    expect(categoryOf("remember")).toBe("mem");
    expect(categoryOf("use_capability")).toBe("mcp");
    expect(labelFor("remember")).not.toBe(labelFor("use_capability"));
  });
});
