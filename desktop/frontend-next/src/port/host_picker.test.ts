import { afterEach, describe, expect, it, vi } from "vitest";

// What is held here is that the three answers a picker can give stay apart: a
// path, "" for a dismissed panel, and null for a shell with no picker at all.
// A caller that conflates the last two asks again and the panel opens twice.

const workspaceInfo = (current: string) => ({ current, canSwitch: true, canIsolate: false, recents: [] });

async function portOver(win: Record<string, unknown>, current = "/w/current") {
  vi.resetModules();
  vi.stubGlobal("window", win);
  vi.stubGlobal("fetch", async () => ({ ok: true, json: async () => workspaceInfo(current) }));
  const { SsePort } = await import("./sse");
  return new SsePort("", "r1");
}

afterEach(() => vi.unstubAllGlobals());

describe("asking for a folder", () => {
  it("opens the Electron panel on the workspace the kernel reports", async () => {
    const opened: string[] = [];
    const port = await portOver({
      reasonixHost: {
        shell: "electron",
        platform: "darwin",
        titleBar: false,
        pickFolder: async (startIn: string) => {
          opened.push(startIn);
          return "/picked";
        },
      },
    });
    expect(await port.pickFolder()).toBe("/picked");
    // The shell owns the dialog; which workspace runs is the kernel's answer.
    expect(opened).toEqual(["/w/current"]);
  });

  it("reads a dismissed Electron panel as an answer, not a missing picker", async () => {
    const port = await portOver({
      reasonixHost: {
        shell: "electron",
        platform: "linux",
        titleBar: true,
        pickFolder: async () => "",
      },
    });
    expect(await port.pickFolder()).toBe("");
  });

  it("still opens where the kernel cannot say which workspace runs", async () => {
    vi.resetModules();
    const opened: string[] = [];
    vi.stubGlobal("window", {
      reasonixHost: {
        shell: "electron",
        platform: "darwin",
        titleBar: false,
        pickFolder: async (startIn: string) => {
          opened.push(startIn);
          return "/picked";
        },
      },
    });
    vi.stubGlobal("fetch", async () => ({ ok: false, status: 503, text: async () => "" }));
    const { SsePort } = await import("./sse");
    expect(await new SsePort("", "r1").pickFolder()).toBe("/picked");
    expect(opened).toEqual([""]);
  });

  it("answers null in a browser tab, which has no panel to open", async () => {
    const port = await portOver({});
    expect(await port.pickFolder()).toBeNull();
  });
});
