import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  applyOverlay,
  clearOverlayCache,
  loadOverlay,
} from "../src/mcp/marketplace-overlay/loader.js";
import type { RegistryEntry } from "../src/mcp/registry-types.js";

const filesystemEntry: RegistryEntry = {
  name: "filesystem",
  title: "Filesystem",
  description: "Grants the agent read/write inside a sandboxed directory.",
  source: "local",
  install: { runtime: "npm", packageId: "@modelcontextprotocol/server-filesystem", transport: "stdio" },
};

const unknownEntry: RegistryEntry = {
  name: "io.example/zzz-unknown",
  title: "Zzz Unknown",
  description: "Not in any overlay.",
  source: "official",
};

describe("marketplace overlay loader", () => {
  afterEach(() => {
    clearOverlayCache();
  });

  it("applies the overlay for a seeded entry under zh-CN", () => {
    const result = applyOverlay(filesystemEntry, "zh-CN");
    expect(result.title).toBe("文件系统");
    expect(result.description).toContain("沙箱");
    expect(result.englishTitle).toBe("Filesystem");
  });

  it("falls through to upstream when no overlay key matches", () => {
    const result = applyOverlay(unknownEntry, "zh-CN");
    expect(result.title).toBe("Zzz Unknown");
    expect(result.description).toBe("Not in any overlay.");
    expect(result.englishTitle).toBeUndefined();
  });

  it("falls through to upstream when language has no bundled overlay (EN)", () => {
    const result = applyOverlay(filesystemEntry, "EN");
    expect(result.title).toBe("Filesystem");
    expect(result.description).toBe(filesystemEntry.description);
    expect(result.englishTitle).toBeUndefined();
  });

  it("throws at load time when the JSON file is malformed", () => {
    const dir = mkdtempSync(join(tmpdir(), "overlay-malformed-"));
    const path = join(dir, "zh-CN.json");
    writeFileSync(path, "{ not valid json");
    expect(() => loadOverlay("zh-CN", path)).toThrow(/malformed/);
  });

  it("throws at load time when an entry is missing required fields", () => {
    const dir = mkdtempSync(join(tmpdir(), "overlay-shape-"));
    const path = join(dir, "zh-CN.json");
    writeFileSync(path, JSON.stringify({ foo: { title: "x" } }));
    expect(() => loadOverlay("zh-CN", path)).toThrow(/title \+ description/);
  });

  it("caches per language so repeated reads do not re-parse", () => {
    const first = loadOverlay("zh-CN");
    const second = loadOverlay("zh-CN");
    expect(second).toBe(first);
  });
});
