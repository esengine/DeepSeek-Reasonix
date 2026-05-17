/** Per-subdirectory REASONIX.md walker + injection (#1033). */

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  findSubdirMemoryAncestors,
  formatSubdirMemorySection,
  readSubdirMemoryContent,
} from "../src/memory/subdir.js";
import { ToolRegistry } from "../src/tools.js";
import { registerFilesystemTools } from "../src/tools/filesystem.js";

describe("findSubdirMemoryAncestors", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-subdir-mem-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("returns [] for a file directly under rootDir (project memory handles it)", () => {
    writeFileSync(join(root, "REASONIX.md"), "root rules");
    writeFileSync(join(root, "foo.ts"), "");
    expect(findSubdirMemoryAncestors(join(root, "foo.ts"), root)).toEqual([]);
  });

  it("finds the closest ancestor memory for a file in a subdir", () => {
    mkdirSync(join(root, "frontend"), { recursive: true });
    writeFileSync(join(root, "frontend", "REASONIX.md"), "use pnpm");
    writeFileSync(join(root, "frontend", "App.tsx"), "");
    expect(findSubdirMemoryAncestors(join(root, "frontend", "App.tsx"), root)).toEqual([
      join(root, "frontend", "REASONIX.md"),
    ]);
  });

  it("returns multiple ancestors innermost-first when both subdirs carry memory", () => {
    mkdirSync(join(root, "pkg", "module"), { recursive: true });
    writeFileSync(join(root, "pkg", "REASONIX.md"), "package rules");
    writeFileSync(join(root, "pkg", "module", "REASONIX.md"), "module rules");
    writeFileSync(join(root, "pkg", "module", "deep.ts"), "");
    expect(findSubdirMemoryAncestors(join(root, "pkg", "module", "deep.ts"), root)).toEqual([
      join(root, "pkg", "module", "REASONIX.md"),
      join(root, "pkg", "REASONIX.md"),
    ]);
  });

  it("skips dirs that have no memory file", () => {
    mkdirSync(join(root, "a", "b", "c"), { recursive: true });
    writeFileSync(join(root, "a", "REASONIX.md"), "only at a");
    writeFileSync(join(root, "a", "b", "c", "x.ts"), "");
    expect(findSubdirMemoryAncestors(join(root, "a", "b", "c", "x.ts"), root)).toEqual([
      join(root, "a", "REASONIX.md"),
    ]);
  });

  it("excludes the rootDir's own REASONIX.md from the walk", () => {
    mkdirSync(join(root, "sub"), { recursive: true });
    writeFileSync(join(root, "REASONIX.md"), "root rules");
    writeFileSync(join(root, "sub", "REASONIX.md"), "sub rules");
    writeFileSync(join(root, "sub", "x.ts"), "");
    const ancestors = findSubdirMemoryAncestors(join(root, "sub", "x.ts"), root);
    expect(ancestors).toEqual([join(root, "sub", "REASONIX.md")]);
    expect(ancestors).not.toContain(join(root, "REASONIX.md"));
  });

  it("returns [] for an absolute path that escapes rootDir", () => {
    const outside = mkdtempSync(join(tmpdir(), "reasonix-subdir-out-"));
    try {
      writeFileSync(join(outside, "foo.ts"), "");
      expect(findSubdirMemoryAncestors(join(outside, "foo.ts"), root)).toEqual([]);
    } finally {
      rmSync(outside, { recursive: true, force: true });
    }
  });

  it("also recognises AGENTS.md / AGENT.md alongside REASONIX.md", () => {
    mkdirSync(join(root, "a"), { recursive: true });
    mkdirSync(join(root, "b"), { recursive: true });
    writeFileSync(join(root, "a", "AGENTS.md"), "use agents file");
    writeFileSync(join(root, "b", "AGENT.md"), "singular agent file");
    writeFileSync(join(root, "a", "x.ts"), "");
    writeFileSync(join(root, "b", "y.ts"), "");
    expect(findSubdirMemoryAncestors(join(root, "a", "x.ts"), root)).toEqual([
      join(root, "a", "AGENTS.md"),
    ]);
    expect(findSubdirMemoryAncestors(join(root, "b", "y.ts"), root)).toEqual([
      join(root, "b", "AGENT.md"),
    ]);
  });
});

describe("readSubdirMemoryContent", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-subdir-read-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("returns trimmed content", () => {
    const p = join(root, "REASONIX.md");
    writeFileSync(p, "  hello world  \n\n");
    expect(readSubdirMemoryContent(p)).toBe("hello world");
  });

  it("returns null for an empty or whitespace-only file", () => {
    const p = join(root, "REASONIX.md");
    writeFileSync(p, "  \n\n");
    expect(readSubdirMemoryContent(p)).toBeNull();
  });

  it("returns null for a missing file", () => {
    expect(readSubdirMemoryContent(join(root, "nope.md"))).toBeNull();
  });

  it("truncates beyond PROJECT_MEMORY_MAX_CHARS with a marker", () => {
    const p = join(root, "REASONIX.md");
    writeFileSync(p, "x".repeat(8100));
    const out = readSubdirMemoryContent(p);
    expect(out).not.toBeNull();
    expect(out!.length).toBeLessThan(8100 + 100);
    expect(out).toContain("… (truncated 100 chars)");
  });
});

describe("formatSubdirMemorySection", () => {
  it("includes the display path and content in a single block", () => {
    const out = formatSubdirMemorySection("frontend/REASONIX.md", "use pnpm");
    expect(out).toContain("frontend/REASONIX.md");
    expect(out).toContain("use pnpm");
    expect(out.startsWith("[module memory:")).toBe(true);
  });
});

describe("read_file injects subdir memory on first read per session", () => {
  let root: string;
  let tools: ToolRegistry;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-fs-mem-"));
    mkdirSync(join(root, "frontend"), { recursive: true });
    writeFileSync(join(root, "frontend", "REASONIX.md"), "use pnpm, never npm");
    writeFileSync(join(root, "frontend", "App.tsx"), "export const App = () => null;");
    tools = new ToolRegistry();
    registerFilesystemTools(tools, { rootDir: root });
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("prepends [module memory:…] on first read of a file under that subdir", async () => {
    const out = await tools.dispatch("read_file", JSON.stringify({ path: "frontend/App.tsx" }));
    expect(out).toContain("[module memory: frontend/REASONIX.md]");
    expect(out).toContain("use pnpm, never npm");
    expect(out).toContain("export const App");
  });

  it("does NOT repeat the memory on subsequent reads in the same subdir", async () => {
    await tools.dispatch("read_file", JSON.stringify({ path: "frontend/App.tsx" }));
    const second = await tools.dispatch("read_file", JSON.stringify({ path: "frontend/App.tsx" }));
    expect(second).not.toContain("[module memory:");
    expect(second).toContain("export const App");
  });

  it("does not inject memory for a file directly under rootDir", async () => {
    writeFileSync(join(root, "root.ts"), "//");
    const out = await tools.dispatch("read_file", JSON.stringify({ path: "root.ts" }));
    expect(out).not.toContain("[module memory:");
  });

  it("respects REASONIX_MEMORY=off and skips injection entirely", async () => {
    const prev = process.env.REASONIX_MEMORY;
    process.env.REASONIX_MEMORY = "off";
    try {
      const tools2 = new ToolRegistry();
      registerFilesystemTools(tools2, { rootDir: root });
      const out = await tools2.dispatch("read_file", JSON.stringify({ path: "frontend/App.tsx" }));
      expect(out).not.toContain("[module memory:");
    } finally {
      if (prev === undefined) {
        // biome-ignore lint/performance/noDelete: env restore
        delete process.env.REASONIX_MEMORY;
      } else {
        process.env.REASONIX_MEMORY = prev;
      }
    }
  });
});
