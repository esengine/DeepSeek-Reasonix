// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockHub } from "../port/mock_hub";

// The chrome's folder mark is a door into the project. What matters is the
// folder it names — the pane's own workspace root — and that it stays shut when
// the shell cannot enter a folder at all.

const ROOT = "D:/project/prj-github";

// Only what the chrome asks a shell at render time, plus the verb under test.
function shellWith(openWorkspace?: (root: string) => Promise<string | null>) {
  return { shell: "electron", platform: "win32", titleBar: false, isWindowMaximised: async () => false, openWorkspace };
}

async function draw(shell: Record<string, unknown>, host?: string) {
  vi.resetModules();
  (window as unknown as { reasonixHost: unknown }).reasonixHost = shell;
  const { Chrome } = await import("./Chrome");
  const hub = new MockHub();
  const openWorkspace = vi.spyOn(hub, "openWorkspace");
  render(
    <Chrome
      port={null}
      status={{ preset: "balanced", toolApprovalMode: "ask", workspaceRoot: ROOT, sessionPath: ROOT + "/s.jsonl" } as never}
      title="会话"
      steer={0}
      onSettings={vi.fn()}
      onBrowser={vi.fn()}
      browser={false}
      account={null}
      rail
      theme="dark"
      onRail={vi.fn()}
      onTheme={vi.fn()}
      onFind={vi.fn()}
      hub={hub}
      host={host}
      onError={vi.fn()}
    />,
  );
  return { button: await screen.findByRole("button", { name: "打开项目文件夹" }), openWorkspace };
}

afterEach(() => {
  cleanup();
  delete (window as unknown as { reasonixHost?: unknown }).reasonixHost;
});

describe("the chrome's project mark", () => {
  it("enters the project the pane is in", async () => {
    const { button, openWorkspace } = await draw(shellWith(async () => null));
    expect((button as HTMLButtonElement).disabled).toBe(false);
    await userEvent.click(button);
    expect(openWorkspace).toHaveBeenCalledWith(ROOT);
  });

  it("stays shut when the shell cannot enter a folder", async () => {
    const { button, openWorkspace } = await draw(shellWith(undefined));
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(button);
    expect(openWorkspace).not.toHaveBeenCalled();
  });

  it("stays shut when the focused pane is driven from another machine", async () => {
    const { button, openWorkspace } = await draw(shellWith(async () => null), "另一台机器");
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(button);
    expect(openWorkspace).not.toHaveBeenCalled();
  });
});
