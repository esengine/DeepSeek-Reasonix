import React from "react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ShellConfirm } from "../src/cli/ui/ShellConfirm.js";
import { setLanguageRuntime } from "../src/i18n/index.js";
import { render } from "./helpers/ink-test.js";

function makeShellPrompt(command: string): import("@reasonix/core-utils").ApprovalPrompt {
  return {
    id: 1,
    kind: "shell",
    tone: "warn",
    title: "Run command",
    subtitle: command,
    preview: command,
    meta: {},
    actions: [
      { id: "run_once", label: "Run once", kind: "allow_once" },
      { id: "always_allow", label: "Always allow", kind: "allow_always" },
      {
        id: "deny",
        label: "Deny",
        kind: "reject",
        secondaryInput: { hint: "Reason", required: false },
      },
    ],
    data: { prefix: command.split(" ")[0] ?? "" },
  };
}

describe("ShellConfirm — renders with ApprovalPrompt", () => {
  beforeEach(() => setLanguageRuntime("EN"));
  afterEach(() => setLanguageRuntime("EN"));

  it("renders the action options and footer", () => {
    const { lastFrame, unmount } = render(
      <ShellConfirm prompt={makeShellPrompt("echo hello")} onChoose={() => {}} />,
    );
    const out = lastFrame() ?? "";
    unmount();
    expect(out).toContain("Run once");
    expect(out).toContain("Always allow");
    expect(out).toContain("Deny");
    expect(out).toContain("echo hello");
  });

  it("triggers deny phase when a secondary-input action is submitted", () => {
    const { lastFrame, stdin, unmount } = render(
      <ShellConfirm prompt={makeShellPrompt("rm -rf /")} onChoose={() => {}} />,
    );
    // Move to "deny" option and submit
    stdin.write("[B[B"); // two down arrows
    stdin.write("\r"); // enter
    const out = lastFrame() ?? "";
    unmount();
    expect(out).toContain("Deny");
  });

  it("translates title + action labels under zh-CN (#1560)", () => {
    setLanguageRuntime("zh-CN");
    const { lastFrame, unmount } = render(
      <ShellConfirm prompt={makeShellPrompt("python script.py")} onChoose={() => {}} />,
    );
    const out = lastFrame() ?? "";
    unmount();
    expect(out).toContain("运行命令");
    expect(out).toContain("运行一次");
    expect(out).toContain("始终允许");
    expect(out).toContain("拒绝");
    expect(out).not.toContain("Run once");
    expect(out).not.toContain("Always allow");
  });
});
