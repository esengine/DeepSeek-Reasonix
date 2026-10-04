import { afterEach, expect, it, vi } from "vitest";

async function hostOver(bridge: Record<string, unknown>) {
  vi.resetModules();
  vi.stubGlobal("window", { reasonixHost: { shell: "electron", platform: "darwin", titleBar: false, ...bridge } });
  return (await import("./host")).host();
}

afterEach(() => vi.unstubAllGlobals());

it("offers reveal only when the shell can show both a pane's file and a project", async () => {
  expect((await hostOver({ revealPath: async () => null, revealWorkspace: async () => null })).revealsFiles()).toBe(true);
  expect((await hostOver({ revealPath: async () => null })).revealsFiles()).toBe(false);
});
