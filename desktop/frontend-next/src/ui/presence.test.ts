// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { onPresence, present } from "./presence";
import { effectsMode, onEffectsChange, setEffectsMode } from "../state/prefs";

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("presence", () => {
  it("reports an audience only while the window is shown and focused", () => {
    const focus = vi.spyOn(document, "hasFocus").mockReturnValue(true);
    const seen: boolean[] = [];
    const off = onPresence((v) => seen.push(v));
    expect(present()).toBe(true);
    focus.mockReturnValue(false);
    window.dispatchEvent(new Event("blur"));
    focus.mockReturnValue(true);
    window.dispatchEvent(new Event("focus"));
    off();
    expect(seen).toEqual([false, true]);
  });

  it("says nothing when nothing changed", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    const seen: boolean[] = [];
    const off = onPresence((v) => seen.push(v));
    window.dispatchEvent(new Event("focus"));
    document.dispatchEvent(new Event("visibilitychange"));
    off();
    expect(seen).toEqual([]);
  });
});

describe("visual effects preference", () => {
  it("defaults to full and reads only 'reduced' as reduced", () => {
    expect(effectsMode()).toBe("full");
    localStorage.setItem("rx-effects", "bogus");
    expect(effectsMode()).toBe("full");
    setEffectsMode("reduced");
    expect(effectsMode()).toBe("reduced");
  });

  it("tells subscribers when it changes", () => {
    const fn = vi.fn();
    const off = onEffectsChange(fn);
    setEffectsMode("reduced");
    off();
    setEffectsMode("full");
    expect(fn).toHaveBeenCalledTimes(1);
  });
});
