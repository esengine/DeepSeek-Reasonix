import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, sep } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  buildEditToolBlocks,
  buildEditToolBlocksForReview,
  isReviewGatedEditTool,
  shouldApplyEditToolImmediately,
} from "../src/cli/ui/edit-tool-gate.js";

describe("review edit gate tool matching", () => {
  it("includes delete tools in the same review gate as single-file edit tools", () => {
    expect(isReviewGatedEditTool("edit_file")).toBe(true);
    expect(isReviewGatedEditTool("write_file")).toBe(true);
    expect(isReviewGatedEditTool("multi_edit")).toBe(true);
    expect(isReviewGatedEditTool("delete_range")).toBe(true);
    expect(isReviewGatedEditTool("delete_symbol")).toBe(true);
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

  it("turns delete_range args into a reviewable deletion block", async () => {
    writeFileSync(join(root, "range.ts"), "before\nSTART\nremove\nEND\nafter\n", "utf8");

    const blocks = await buildEditToolBlocksForReview(
      "delete_range",
      { path: "range.ts", start_anchor: "START\n", end_anchor: "END\n" },
      root,
    );

    expect(blocks).toEqual([
      { path: "range.ts", search: "START\nremove\nEND\n", replace: "", offset: 0 },
    ]);
  });

  it("turns delete_symbol args into a reviewable deletion block", async () => {
    writeFileSync(
      join(root, "symbols.ts"),
      [
        "export function keep() {",
        "  return 1;",
        "}",
        "",
        "/** Remove this class. */",
        "@sealed",
        "export class RemoveMe {",
        "  value = 2;",
        "}",
        "",
      ].join("\n"),
      "utf8",
    );

    const blocks = await buildEditToolBlocksForReview(
      "delete_symbol",
      { path: "symbols.ts", name: "RemoveMe", kind: "class" },
      root,
    );

    expect(blocks).toEqual([
      {
        path: "symbols.ts",
        search: "/** Remove this class. */\n@sealed\nexport class RemoveMe {\n  value = 2;\n}\n",
        replace: "",
        offset: 0,
      },
    ]);
  });
});
