// @vitest-environment jsdom

import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { cleanup, render } from "@testing-library/react";
import React from "react";
import { afterEach, describe, expect, it } from "vitest";
import {
  type UndoAppliedEvent,
  codeUndoInfo,
  formatUndoContextMessage,
} from "../src/cli/ui/undo-context.js";
import { useEditHistory } from "../src/cli/ui/useEditHistory.js";
import { type EditBlock, applyEditBlocks, snapshotBeforeEdits } from "../src/code/edit-blocks.js";

afterEach(() => {
  cleanup();
});

describe("useEditHistory undo context", () => {
  it("notifies the caller when /undo reverts an edit batch", () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-undo-context-"));
    const target = join(root, "demo.txt");
    writeFileSync(target, "before\n", "utf8");

    let api: ReturnType<typeof useEditHistory> | undefined;
    function Harness() {
      api = useEditHistory({ rootDir: root });
      return null;
    }

    render(<Harness />);

    const blocks: EditBlock[] = [
      { path: "demo.txt", search: "before\n", replace: "after\n", offset: 0 },
    ];
    const snaps = snapshotBeforeEdits(blocks, root);
    const results = applyEditBlocks(blocks, root);
    api!.recordEdit("auto", blocks, results, snaps);

    const result = api!.codeUndo([]);

    expect(codeUndoInfo(result)).toContain("reverted demo.txt from batch #1");
    expect(readFileSync(target, "utf8")).toBe("before\n");
    expect(result.contextEvent).toEqual({
      batchId: 1,
      source: "auto",
      paths: ["demo.txt"],
    });
    expect(formatUndoContextMessage(result.contextEvent as UndoAppliedEvent)).toContain(
      "The user ran /undo and reverted edit batch #1",
    );
  });

  it("does not return a context message when /undo has nothing left to restore", () => {
    let api: ReturnType<typeof useEditHistory> | undefined;
    function Harness() {
      api = useEditHistory({ rootDir: mkdtempSync(join(tmpdir(), "reasonix-undo-empty-")) });
      return null;
    }

    render(<Harness />);

    const result = api!.codeUndo([]);
    expect(codeUndoInfo(result)).toContain("nothing to undo");
    expect(result.contextEvent).toBeUndefined();
  });
});
