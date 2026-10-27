import { afterEach, expect, it, vi } from "vitest";
import type { TextContextMenuRequest } from "./host";

// Each case reloads the module because host() caches its bridge on first use.

const request: TextContextMenuRequest = { editable: true, selected: true, canUndo: true, canRedo: false, quote: true, x: 12, y: 34 };
afterEach(() => vi.unstubAllGlobals());

it("reports unsupported browser and older-shell menus as null", async () => {
  for (const window of [{}, { reasonixHost: { shell: "electron", platform: "darwin", titleBar: false } }]) {
    vi.resetModules();
    vi.stubGlobal("window", window);
    const { host } = await import("./host");
    expect(await host().showTextContextMenu(request)).toBeNull();
  }
});

it.each(["", "quote", "undo", "redo", "selectAll"] as const)("preserves native menu result %j without treating dismissal as unsupported", async (result) => {
  vi.resetModules();
  const showTextContextMenu = vi.fn().mockResolvedValue(result);
  vi.stubGlobal("window", { reasonixHost: { shell: "electron", platform: "darwin", titleBar: false, showTextContextMenu } });
  const { host } = await import("./host");
  expect(await host().showTextContextMenu(request)).toBe(result);
  expect(showTextContextMenu).toHaveBeenCalledExactlyOnceWith(request);
});

it("does not turn native invocation failures into an unsupported-browser fallback", async () => {
  vi.resetModules();
  vi.stubGlobal("window", { reasonixHost: { shell: "electron", platform: "darwin", titleBar: false, showTextContextMenu: vi.fn().mockRejectedValue(new Error("native menu refused")) } });
  const { host } = await import("./host");
  await expect(host().showTextContextMenu(request)).rejects.toThrow("native menu refused");
});
