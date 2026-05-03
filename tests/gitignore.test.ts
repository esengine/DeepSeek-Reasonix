import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import ignore from "ignore";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  type GitignoreLayer,
  ignoredByLayers,
  loadGitignoreAt,
  loadGitignoreAtSync,
} from "../src/gitignore.js";

describe("ignoredByLayers", () => {
  it("returns false when there are no layers", () => {
    expect(ignoredByLayers([], "/proj/foo.log", false)).toBe(false);
  });

  it("matches a literal directory pattern under a single layer", () => {
    const layers: GitignoreLayer[] = [{ dirAbs: "/proj", ig: ignore().add("node_modules/") }];
    expect(ignoredByLayers(layers, "/proj/node_modules", true)).toBe(true);
    expect(ignoredByLayers(layers, "/proj/src/index.ts", false)).toBe(false);
  });

  it("honors intra-file negation within a single layer", () => {
    const layers: GitignoreLayer[] = [{ dirAbs: "/proj", ig: ignore().add("*.log\n!keep.log") }];
    expect(ignoredByLayers(layers, "/proj/foo.log", false)).toBe(true);
    expect(ignoredByLayers(layers, "/proj/keep.log", false)).toBe(false);
  });

  it("toggles dir-only matching via the isDir flag", () => {
    const layers: GitignoreLayer[] = [{ dirAbs: "/proj", ig: ignore().add("build/") }];
    expect(ignoredByLayers(layers, "/proj/build", true)).toBe(true);
    expect(ignoredByLayers(layers, "/proj/build", false)).toBe(false);
  });

  it("skips a layer whose dirAbs does not contain the queried path", () => {
    const layers: GitignoreLayer[] = [
      { dirAbs: "/proj/sub", ig: ignore().add("*") }, // would match anything inside /proj/sub
    ];
    expect(ignoredByLayers(layers, "/proj/other/file.ts", false)).toBe(false);
  });

  it("falls through to a later layer when an earlier layer is out of scope", () => {
    const layers: GitignoreLayer[] = [
      { dirAbs: "/proj/sub", ig: ignore().add("*") }, // out of scope for the query
      { dirAbs: "/proj", ig: ignore().add("dist/") }, // matches
    ];
    expect(ignoredByLayers(layers, "/proj/dist/main.js", false)).toBe(true);
  });

  it("returns false when no layer ignores the path", () => {
    const layers: GitignoreLayer[] = [
      { dirAbs: "/proj", ig: ignore().add("node_modules/\n*.log") },
    ];
    expect(ignoredByLayers(layers, "/proj/src/index.ts", false)).toBe(false);
  });
});

describe("loadGitignoreAt / loadGitignoreAtSync", () => {
  let dir: string;
  beforeEach(() => {
    dir = mkdtempSync(path.join(tmpdir(), "reasonix-gitignore-"));
  });
  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("loadGitignoreAt returns null when .gitignore is missing", async () => {
    expect(await loadGitignoreAt(dir)).toBeNull();
  });

  it("loadGitignoreAtSync returns null when .gitignore is missing", () => {
    expect(loadGitignoreAtSync(dir)).toBeNull();
  });

  it("loadGitignoreAt returns an Ignore that honors patterns from disk", async () => {
    writeFileSync(path.join(dir, ".gitignore"), "*.log\n!keep.log\n");
    const ig = await loadGitignoreAt(dir);
    expect(ig).not.toBeNull();
    expect(ig!.ignores("foo.log")).toBe(true);
    expect(ig!.ignores("keep.log")).toBe(false);
  });

  it("loadGitignoreAtSync returns an Ignore that honors patterns from disk", () => {
    writeFileSync(path.join(dir, ".gitignore"), "node_modules/\n");
    const ig = loadGitignoreAtSync(dir);
    expect(ig).not.toBeNull();
    expect(ig!.ignores("node_modules/foo")).toBe(true);
    expect(ig!.ignores("src/index.ts")).toBe(false);
  });
});
