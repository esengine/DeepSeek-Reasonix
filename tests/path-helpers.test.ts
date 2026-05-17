/** Regression for #942 — write_file interceptor must NOT strip the slash off a real absolute system path. */

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve, sep } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { looksLikeAbsoluteSystemPath, pathIsUnder } from "../src/tools/filesystem.js";

describe("looksLikeAbsoluteSystemPath", () => {
  it("flags POSIX system roots", () => {
    expect(looksLikeAbsoluteSystemPath("/Users/x/.claude/skills/foo.md")).toBe(true);
    expect(looksLikeAbsoluteSystemPath("/home/x/foo")).toBe(true);
    expect(looksLikeAbsoluteSystemPath("/etc/hosts")).toBe(true);
    expect(looksLikeAbsoluteSystemPath("/tmp/x")).toBe(true);
  });

  it("flags Windows drive-letter absolutes", () => {
    expect(looksLikeAbsoluteSystemPath("C:\\Users\\x\\foo.md")).toBe(true);
    expect(looksLikeAbsoluteSystemPath("D:/projects/foo")).toBe(true);
  });

  it("does NOT flag sandbox-shorthand leading slashes", () => {
    expect(looksLikeAbsoluteSystemPath("/src/foo.ts")).toBe(false);
    expect(looksLikeAbsoluteSystemPath("/foo.md")).toBe(false);
    expect(looksLikeAbsoluteSystemPath("foo.md")).toBe(false);
    expect(looksLikeAbsoluteSystemPath("./foo.md")).toBe(false);
  });
});

describe("pathIsUnder", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-path-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("recognises a child path", () => {
    expect(pathIsUnder(join(root, "a", "b.ts"), root)).toBe(true);
  });

  it("recognises the root itself", () => {
    expect(pathIsUnder(root, root)).toBe(true);
  });

  it("rejects a sibling", () => {
    const other = mkdtempSync(join(tmpdir(), "reasonix-other-"));
    try {
      expect(pathIsUnder(join(other, "a"), root)).toBe(false);
    } finally {
      rmSync(other, { recursive: true, force: true });
    }
  });

  it("rejects a `..`-escape", () => {
    expect(pathIsUnder(resolve(root, "..", "elsewhere"), root)).toBe(false);
  });
});

describe("write_file interceptor logic — issue #942 reproduction", () => {
  // Mirrors the rewritten interceptor in src/cli/ui/App.tsx: a path that
  // looksLikeAbsoluteSystemPath AND falls outside rootDir must NOT be
  // turned into a rootDir-relative path. The interceptor returns null in
  // that case so the native write_file tool fn handles it through the
  // safePath approval gate.
  function resolveInterceptedRelPath(rawPath: string, rootForEdit: string): string | null {
    if (!rawPath) return null;
    const absRoot = resolve(rootForEdit);
    if (looksLikeAbsoluteSystemPath(rawPath)) {
      const abs = resolve(rawPath);
      if (!pathIsUnder(abs, absRoot)) return null;
      const rel = abs === absRoot ? "" : abs.slice(absRoot.length + 1);
      return rel || null;
    }
    let stripped = rawPath;
    while (stripped.startsWith("/") || stripped.startsWith("\\")) {
      stripped = stripped.slice(1);
    }
    return stripped || null;
  }

  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-root-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("defers an outside-root absolute system path back to the tool fn (returns null)", () => {
    const outsidePath =
      process.platform === "win32"
        ? "C:\\Users\\someone\\.claude\\skills\\foo.md"
        : "/Users/someone/.claude/skills/foo.md";
    expect(resolveInterceptedRelPath(outsidePath, root)).toBeNull();
  });

  it("keeps intercepting when an absolute path resolves under rootDir", () => {
    const inside = join(root, "sub", "foo.md");
    const rel = resolveInterceptedRelPath(inside, root);
    expect(rel).toBe(`sub${sep}foo.md`);
  });

  it("still treats a model-style leading slash as sandbox-relative", () => {
    expect(resolveInterceptedRelPath("/src/foo.ts", root)).toBe("src/foo.ts");
  });
});
