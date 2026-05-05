import { describe, expect, it, vi } from "vitest";
import { replaceMcpServerSummary, sameMcpServerSummary } from "../src/cli/ui/mcp-server-list.js";
import type { McpServerSummary } from "../src/cli/ui/slash/types.js";
import type { BridgeEnv, McpClientHost } from "../src/mcp/registry.js";

function fakeServer(label: string, spec: string): McpServerSummary {
  return {
    label,
    spec,
    toolCount: 3,
    report: {
      protocolVersion: "2024-11-05",
      serverInfo: { name: label, version: "1.0" },
      capabilities: {},
      tools: { supported: true, items: [] },
      resources: { supported: false, reason: "" },
      prompts: { supported: false, reason: "" },
      elapsedMs: 50,
    },
    host: {} as McpClientHost,
    bridgeEnv: {} as BridgeEnv,
    readResource: vi.fn(),
    getPrompt: vi.fn(),
  };
}

describe("sameMcpServerSummary", () => {
  it("returns true for the same object reference", () => {
    const a = fakeServer("fs", "fs=npx");
    expect(sameMcpServerSummary(a, a)).toBe(true);
  });

  it("returns true for different objects with matching label and spec", () => {
    const a = fakeServer("fs", "fs=npx");
    const b = fakeServer("fs", "fs=npx");
    expect(sameMcpServerSummary(a, b)).toBe(true);
  });

  it("returns false when label differs", () => {
    const a = fakeServer("fs", "fs=npx");
    const b = fakeServer("git", "git=npx");
    expect(sameMcpServerSummary(a, b)).toBe(false);
  });

  it("returns false when spec differs", () => {
    const a = fakeServer("fs", "fs=npx");
    const b = fakeServer("fs", "fs=uvx");
    expect(sameMcpServerSummary(a, b)).toBe(false);
  });
});

describe("replaceMcpServerSummary", () => {
  it("replaces by exact reference match", () => {
    const original = fakeServer("fs", "fs=npx");
    const updated = fakeServer("fs", "fs=npx");
    updated.toolCount = 5;
    const servers = [original];

    const result = replaceMcpServerSummary(servers, original, updated);

    expect(result).toHaveLength(1);
    expect(result[0]).toBe(updated);
    expect(result[0].toolCount).toBe(5);
  });

  it("replaces by label/spec match when object references differ", () => {
    const original = fakeServer("fs", "fs=npx");
    const updated = { ...original, toolCount: 7 };
    const sameIdentity = fakeServer("fs", "fs=npx");
    const servers = [sameIdentity];

    const result = replaceMcpServerSummary(servers, original, updated);

    expect(result).toHaveLength(1);
    expect(result[0]).toBe(updated);
    expect(result[0].toolCount).toBe(7);
  });

  it("preserves non-matching servers", () => {
    const fs = fakeServer("fs", "fs=npx");
    const git = fakeServer("git", "git=npx");
    const updated = { ...fs, toolCount: 5 };
    const servers = [fs, git];

    const result = replaceMcpServerSummary(servers, fs, updated);

    expect(result).toHaveLength(2);
    expect(result[0]).toBe(updated);
    expect(result[1]).toBe(git);
  });

  it("does not mutate the original array", () => {
    const original = fakeServer("fs", "fs=npx");
    const updated = { ...original, toolCount: 5 };
    const servers = [original];

    const result = replaceMcpServerSummary(servers, original, updated);

    expect(result).not.toBe(servers);
    expect(servers[0]).toBe(original);
    expect(servers[0].toolCount).toBe(3);
  });

  it("handles stale-reference scenario: second update still matches by label/spec", () => {
    const original = fakeServer("fs", "fs=npx");

    // First replacement — object reference changes
    const updatedOnce = { ...original, toolCount: 5 };
    const servers = replaceMcpServerSummary([original], original, updatedOnce);
    expect(servers[0]).toBe(updatedOnce);

    // Second replacement using the ORIGINAL reference (now stale) but a newer object
    const updatedTwice = { ...original, toolCount: 9 };
    const result = replaceMcpServerSummary(servers, original, updatedTwice);

    // Must match via label/spec since `original !== servers[0]`
    expect(result).toHaveLength(1);
    expect(result[0]).toBe(updatedTwice);
    expect(result[0].toolCount).toBe(9);
  });
});
