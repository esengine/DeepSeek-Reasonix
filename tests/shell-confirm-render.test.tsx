import { render } from "ink-testing-library";
import React from "react";
import { describe, expect, it } from "vitest";
import { ShellConfirm, clampCommand } from "../src/cli/ui/ShellConfirm.js";

describe("clampCommand", () => {
  it("returns the original command when line count is within max", () => {
    const cmd = "echo one\necho two\necho three";
    expect(clampCommand(cmd, 5)).toEqual({ preview: cmd, hidden: 0 });
  });

  it("keeps exactly `max` lines and reports the dropped count", () => {
    const cmd = ["a", "b", "c", "d", "e"].join("\n");
    expect(clampCommand(cmd, 3)).toEqual({ preview: "a\nb\nc", hidden: 2 });
  });

  it("treats a single-line command as not clamped", () => {
    expect(clampCommand("ls -la", 3)).toEqual({ preview: "ls -la", hidden: 0 });
  });

  it("is a no-op when max equals the line count exactly", () => {
    const cmd = "a\nb\nc";
    expect(clampCommand(cmd, 3)).toEqual({ preview: cmd, hidden: 0 });
  });
});

describe("ShellConfirm — long-command rendering (issue #680)", () => {
  it("renders the action options and footer even with a 50-line command", () => {
    const command = Array.from({ length: 50 }, (_, i) => `echo line-${i + 1}`).join("\n");
    const { lastFrame, unmount } = render(
      <ShellConfirm command={command} allowPrefix="echo" onChoose={() => {}} />,
    );
    const out = lastFrame() ?? "";
    unmount();
    expect(out).toContain("allow once");
    expect(out).toContain("allow always");
    expect(out).toContain("deny");
    expect(out).toContain("pick");
    expect(out).toContain("confirm");
    expect(out).toMatch(/more lines? hidden/);
    expect(out).toContain("echo line-1");
  });

  it("does not show the hidden-lines hint for a short command", () => {
    const { lastFrame, unmount } = render(
      <ShellConfirm command="echo hello" allowPrefix="echo" onChoose={() => {}} />,
    );
    const out = lastFrame() ?? "";
    unmount();
    expect(out).toContain("echo hello");
    expect(out).not.toMatch(/more lines? hidden/);
  });
});
