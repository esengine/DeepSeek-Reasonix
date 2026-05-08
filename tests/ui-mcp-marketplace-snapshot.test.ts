import { describe, expect, it } from "vitest";
import { buildMarketplacePickerSnapshot } from "../src/cli/ui/McpMarketplace.js";
import { specStringFor } from "../src/mcp/registry-fetch.js";
import type { RegistryEntry, RegistryInstall } from "../src/mcp/registry-types.js";

const officialInstall: RegistryInstall = {
  runtime: "npm",
  packageId: "@official/mcp-time",
  transport: "stdio",
};

const officialEntry: RegistryEntry = {
  name: "@official/mcp-time",
  title: "Time",
  description: "Returns the current time.",
  source: "official",
  install: officialInstall,
  popularity: 1234,
};

const smitheryEntry: RegistryEntry = {
  name: "vendor/searchy",
  title: "Searchy",
  description: "Searches the web.",
  source: "smithery",
};

const localEntry: RegistryEntry = {
  name: "local/scratch",
  title: "Scratch",
  description: "Local-only test entry.",
  source: "local",
  install: { runtime: "npm", packageId: "scratch", transport: "stdio" },
};

describe("buildMarketplacePickerSnapshot", () => {
  it("maps entries into picker items with source-tagged badges", () => {
    const snap = buildMarketplacePickerSnapshot({
      filtered: [officialEntry, smitheryEntry, localEntry],
      installedSpecs: [],
      query: "",
      status: "ready",
      hasMore: false,
    });
    expect(snap.pickerKind).toBe("mcp-marketplace");
    expect(snap.items.map((i) => i.id)).toEqual([
      "@official/mcp-time",
      "vendor/searchy",
      "local/scratch",
    ]);
    expect(snap.items.map((i) => i.badge)).toEqual(["official", "smithery", "local"]);
    expect(snap.items[0]!.meta).toBe("★ 1,234");
    expect(snap.items[1]!.meta).toBeUndefined();
  });

  it("flips badge to `installed` when entry's spec is in installedSpecs", () => {
    const spec = specStringFor(officialEntry.name, officialInstall);
    const snap = buildMarketplacePickerSnapshot({
      filtered: [officialEntry, smitheryEntry],
      installedSpecs: [spec],
      query: "",
      status: "ready",
      hasMore: false,
    });
    expect(snap.items[0]!.badge).toBe("installed");
    expect(snap.items[1]!.badge).toBe("smithery");
  });

  it("carries query, hasMore, and the full picker action set", () => {
    const snap = buildMarketplacePickerSnapshot({
      filtered: [officialEntry],
      installedSpecs: [],
      query: "time",
      status: "1 match",
      hasMore: true,
    });
    expect(snap.query).toBe("time");
    expect(snap.title).toBe("MCP marketplace · 1 match");
    expect(snap.hasMore).toBe(true);
    expect(snap.actions).toEqual(["install", "uninstall", "refine", "load-more", "cancel"]);
  });

  it("truncates long descriptions for the subtitle", () => {
    const long = "x".repeat(300);
    const snap = buildMarketplacePickerSnapshot({
      filtered: [{ ...officialEntry, description: long }],
      installedSpecs: [],
      query: "",
      status: "",
      hasMore: false,
    });
    expect(snap.items[0]!.subtitle).toHaveLength(200);
  });
});
