import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, sep } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  buildEditToolBlocks,
  isReviewGatedEditTool,
  shouldApplyEditToolImmediately,
} from "../src/cli/ui/edit-tool-gate.js";

describe("review edit gate tool matching", () => {
  it("includes multi_edit in the same review gate as single-file edit tools", () => {
    expect(isReviewGatedEditTool("edit_file")).toBe(true);
    expect(isReviewGatedEditTool("write_file")).toBe(true);
    expect(isReviewGatedEditTool("multi_edit")).toBe(true);
    expect(isReviewGatedEditTool("read_file")).toBe(false);
  });
});

describe("shouldApplyEditToolImmediately", () => {
  it("requires the review queue after switching back to review", () => {
    expect(shouldApplyEditToolImmediately("yolo", "ask")).toBe(true);
    expect(shouldApplyEditToolImmediately("review", "ask")).toBe(false);
    expect(shouldApplyEditToolImmediately("review", "apply-all")).toBe(true);
  });
});

describe("buildEditToolBlocks", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-review-gate-"));
    writeFileSync(join(root, "existing.ts"), "export const value = 1;\n", "utf8");
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("turns multi_edit args into reviewable edit blocks without touching disk", () => {
    const blocks = buildEditToolBlocks(
      "multi_edit",
      {
        edits: [
          { path: "existing.ts", search: "value = 1", replace: "value = 2" },
          { path: "/src/new.ts", search: "old", replace: "new" },
        ],
      },
      root,
    );

    expect(blocks).toEqual([
      { path: "existing.ts", search: "value = 1", replace: "value = 2", offset: 0 },
      { path: "src/new.ts", search: "old", replace: "new", offset: 0 },
    ]);
  });

  it("keeps intercepting absolute paths that resolve under the workspace", () => {
    const blocks = buildEditToolBlocks(
      "multi_edit",
      {
        edits: [{ path: join(root, "nested", "file.ts"), search: "a", replace: "b" }],
      },
      root,
    );

    expect(blocks).toEqual([{ path: `nested${sep}file.ts`, search: "a", replace: "b", offset: 0 }]);
  });
});
