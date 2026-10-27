// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

beforeEach(() => {
  localStorage.clear();
  vi.resetModules();
});
afterEach(() => vi.restoreAllMocks());

// Dynamic imports exercise a fresh preference cache after a window reload.
describe("thinking preview persistence", () => {
  it("migrates a saved peek into collapsed thinking with previews, preserving sibling modes", async () => {
    localStorage.setItem("rx-transcript-fold", JSON.stringify({ thinking: "peek", activity: "open", steps: "changed" }));
    const prefs = await import("./prefs");
    expect(prefs.foldModes()).toMatchObject({ thinking: "folded", activity: "open", steps: "changed" });
    expect(prefs.showsThinkingPreview()).toBe(true);
    prefs.setShowsThinkingPreview(false);
    vi.resetModules();
    const reloaded = await import("./prefs");
    expect(reloaded.showsThinkingPreview()).toBe(false);
    expect(reloaded.foldModes()).toMatchObject({ thinking: "folded", activity: "open", steps: "changed" });
  });

  it("persists the preview independently of later folding choices", async () => {
    const prefs = await import("./prefs");
    prefs.setShowsThinkingPreview(true);
    prefs.setFoldModes({ thinking: "live", activity: "folded" });
    vi.resetModules();
    const reloaded = await import("./prefs");
    expect(reloaded.showsThinkingPreview()).toBe(true);
    expect(reloaded.foldModes()).toMatchObject({ thinking: "live", activity: "folded" });
  });

  it.each(["not-json", "null", "[]", '{"thinking":"unknown","thinkingPreview":"true"}'])("handles unreadable or invalid preferences: %s", async (raw) => {
    localStorage.setItem("rx-transcript-fold", raw);
    const prefs = await import("./prefs");
    expect(prefs.foldModes()).toEqual(prefs.FOLD_DEFAULTS);
    expect(prefs.showsThinkingPreview()).toBe(false);
  });

  it("keeps a migrated choice usable when storage cannot be written", async () => {
    localStorage.setItem("rx-transcript-fold", '{"thinking":"peek"}');
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new DOMException("Unavailable", "SecurityError"); });
    const prefs = await import("./prefs");
    expect(prefs.showsThinkingPreview()).toBe(true);
    prefs.setShowsThinkingPreview(false);
    prefs.setFoldModes({ thinking: "live" });
    expect(prefs.showsThinkingPreview()).toBe(false);
    expect(prefs.foldModes().thinking).toBe("live");
  });
});
