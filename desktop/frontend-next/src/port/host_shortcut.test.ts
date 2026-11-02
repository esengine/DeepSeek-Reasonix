import { afterEach, expect, it, vi } from "vitest";

// Host selection is cached at module load/use; each test needs a fresh module.

afterEach(() => vi.unstubAllGlobals());

it("does not grant browser or network viewers a host-machine shortcut", async () => {
  vi.resetModules();
  vi.stubGlobal("window", {});
  const { host } = await import("./host");
  expect(await host().globalShortcut()).toEqual({ binding: "", status: "unsupported" });
  expect(await host().setGlobalShortcut("CommandOrControl+Shift+Space")).toEqual({ binding: "", status: "unsupported" });
});

it("asks the local Electron shell immediately, without browser storage", async () => {
  vi.resetModules();
  const state = { binding: "Control+Shift+Space", status: "registered" };
  const apply = vi.fn(async () => state);
  vi.stubGlobal("window", { reasonixHost: { shell: "electron", platform: "linux", globalShortcut: async () => state, setGlobalShortcut: apply } });
  const { host } = await import("./host");
  expect(await host().globalShortcut()).toEqual(state);
  expect(await host().setGlobalShortcut(state.binding)).toEqual(state);
  expect(apply).toHaveBeenCalledWith(state.binding);
});
