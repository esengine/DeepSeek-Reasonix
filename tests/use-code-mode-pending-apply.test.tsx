// @vitest-environment jsdom

import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { cleanup, render } from "@testing-library/react";
import React, { useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useCodeMode } from "../src/cli/ui/hooks/useCodeMode.js";
import type { EditBlock } from "../src/code/edit-blocks.js";
import { pendingEditsPath, savePendingEdits } from "../src/code/pending-edits.js";

function block(overrides: Partial<EditBlock> = {}): EditBlock {
  return {
    path: "demo.txt",
    search: "before\n",
    replace: "after\n",
    offset: 0,
    ...overrides,
  };
}

describe("useCodeMode pending edit apply", () => {
  let home: string;
  let root: string;

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), "reasonix-pending-home-"));
    root = mkdtempSync(join(tmpdir(), "reasonix-pending-root-"));
    vi.stubEnv("USERPROFILE", home);
    vi.stubEnv("HOME", home);
    vi.spyOn(require("node:os"), "homedir").mockReturnValue(home);
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    rmSync(home, { recursive: true, force: true });
    rmSync(root, { recursive: true, force: true });
  });

  it("loads a saved pending edit when /apply runs with an empty in-memory queue", () => {
    const session = "code-demo";
    writeFileSync(join(root, "demo.txt"), "before\n", "utf8");
    savePendingEdits(session, [block()]);

    let api: ReturnType<typeof useCodeMode> | undefined;
    function Harness() {
      const pendingEdits = useRef<EditBlock[]>([]);
      api = useCodeMode({
        codeMode: true,
        pendingEdits,
        currentRootDir: root,
        session,
        syncPendingCount: () => undefined,
        recordEdit: () => undefined,
      });
      return null;
    }

    render(<Harness />);

    const info = api!.codeApply();

    expect(info).toContain("1/1 applied");
    expect(readFileSync(join(root, "demo.txt"), "utf8")).toBe("after\n");
    expect(existsSync(pendingEditsPath(session))).toBe(false);
  });

  it("loads a saved pending edit when /discard runs with an empty in-memory queue", () => {
    const session = "code-demo-discard";
    writeFileSync(join(root, "demo.txt"), "before\n", "utf8");
    savePendingEdits(session, [block()]);

    let api: ReturnType<typeof useCodeMode> | undefined;
    function Harness() {
      const pendingEdits = useRef<EditBlock[]>([]);
      api = useCodeMode({
        codeMode: true,
        pendingEdits,
        currentRootDir: root,
        session,
        syncPendingCount: () => undefined,
        recordEdit: () => undefined,
      });
      return null;
    }

    render(<Harness />);

    const info = api!.codeDiscard();

    expect(info).toContain("discarded 1 pending");
    expect(readFileSync(join(root, "demo.txt"), "utf8")).toBe("before\n");
    expect(existsSync(pendingEditsPath(session))).toBe(false);
  });
});
