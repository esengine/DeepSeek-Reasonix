/** Synthetic "go to parent" entry for @ pickers (#1019) — pure path-math helpers used by both the desktop composer and the CLI at-mention browser. */

import { describe, expect, it } from "vitest";

/** Replicates `parentOfAtQuery` in desktop/src/ui/composer.tsx — returning the parent directory (with trailing slash) of the @ query, or null when there's nowhere to go. */
function parentOfAtQuery(query: string): string | null {
  const normalized = query.replace(/\\/g, "/");
  const trailingSlash = normalized.endsWith("/");
  const lastSlash = normalized.lastIndexOf("/");
  if (lastSlash < 0) return null;
  const dirContext = trailingSlash ? normalized.slice(0, -1) : normalized.slice(0, lastSlash);
  if (!dirContext) return null;
  const parentIdx = dirContext.lastIndexOf("/");
  return parentIdx >= 0 ? `${dirContext.slice(0, parentIdx)}/` : "";
}

/** Replicates `parentBrowseEntry` in src/cli/ui/useCompletionPickers.ts — returning the insertPath to wire into the CLI synthetic ".." entry. Browse mode only fires when the current dir is non-empty so we never call this at the root. */
function parentBrowseInsertPath(currentDir: string): string {
  const idx = currentDir.lastIndexOf("/");
  return idx >= 0 ? currentDir.slice(0, idx) : "";
}

describe("parentOfAtQuery (desktop)", () => {
  it("returns null for a bare filter at the root", () => {
    expect(parentOfAtQuery("App")).toBeNull();
    expect(parentOfAtQuery("")).toBeNull();
  });

  it("returns '' (workspace root) for a one-level query", () => {
    expect(parentOfAtQuery("src/")).toBe("");
    expect(parentOfAtQuery("src/App")).toBe("");
  });

  it("strips one segment when nested", () => {
    expect(parentOfAtQuery("pkg/module/")).toBe("pkg/");
    expect(parentOfAtQuery("pkg/module/Foo")).toBe("pkg/");
    expect(parentOfAtQuery("a/b/c/d/")).toBe("a/b/c/");
    expect(parentOfAtQuery("frontend/components/")).toBe("frontend/");
  });

  it("normalises Windows separators", () => {
    expect(parentOfAtQuery("frontend\\components\\")).toBe("frontend/");
    expect(parentOfAtQuery("a\\b\\c\\App")).toBe("a/b/");
  });
});

describe("parentBrowseInsertPath (CLI)", () => {
  it("returns '' for a single-segment browse dir (back to workspace root)", () => {
    expect(parentBrowseInsertPath("src")).toBe("");
    expect(parentBrowseInsertPath("frontend")).toBe("");
  });

  it("pops one segment for nested dirs", () => {
    expect(parentBrowseInsertPath("pkg/module")).toBe("pkg");
    expect(parentBrowseInsertPath("a/b/c")).toBe("a/b");
  });
});
