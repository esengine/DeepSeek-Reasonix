// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => { vi.restoreAllMocks(); vi.resetModules(); });
const key = (patch = {}) => ({ key: "Enter", code: "Enter", ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...patch });

describe("platform send chords", () => {
  it.each(["Windows", "Macintosh"])("uses the native modifier on %s and ignores extra modifiers", async (platform) => {
    vi.spyOn(navigator, "userAgent", "get").mockReturnValue(platform);
    vi.resetModules();
    const { sendsOnKey } = await import("./sendkey");
    const native = platform === "Macintosh" ? { metaKey: true } : { ctrlKey: true };
    const other = platform === "Macintosh" ? { ctrlKey: true } : { metaKey: true };
    expect(sendsOnKey(key(native), "modifier_enter")).toBe(true);
    expect(sendsOnKey(key(native), "modifier_enter", true)).toBe(true);
    expect(sendsOnKey(key(other), "modifier_enter")).toBe(false);
    expect(sendsOnKey(key({ ...native, shiftKey: true }), "modifier_enter")).toBe(false);
    expect(sendsOnKey(key({ ...native, altKey: true }), "modifier_enter")).toBe(false);
    expect(sendsOnKey(key(), "enter")).toBe(true);
    expect(sendsOnKey(key({ altKey: true }), "enter")).toBe(true);
    expect(sendsOnKey(key({ shiftKey: true }), "enter")).toBe(false);
    expect(sendsOnKey(key(), "enter", true)).toBe(false);
  });
});
