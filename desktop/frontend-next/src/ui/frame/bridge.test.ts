import { describe, expect, it } from "vitest";
import { DropCounter, RateGate, clampHeight, decodeFrameMessage, FRAME_PROTOCOL, SLOT_HEIGHTS } from "./bridge";

describe("what a frame may say", () => {
  it("reads the five messages the protocol has", () => {
    expect(decodeFrameMessage({ type: "ready" })).toEqual({ type: "ready" });
    expect(decodeFrameMessage({ type: "size", height: 240 })).toEqual({ type: "size", height: 240 });
    expect(decodeFrameMessage({ type: "action", actionId: "refresh" })).toEqual({ type: "action", actionId: "refresh" });
    expect(decodeFrameMessage({ type: "action", actionId: "move", args: { card: "a", to: "done" } })).toEqual({ type: "action", actionId: "move", args: { card: "a", to: "done" } });
    expect(decodeFrameMessage({ type: "submit", values: { title: "x", n: 2 } })).toEqual({ type: "submit", values: { title: "x", n: 2 } });
    expect(decodeFrameMessage({ type: "state", value: { tab: 2 } })).toEqual({ type: "state", value: { tab: 2 } });
    expect(FRAME_PROTOCOL).toBe("reasonix.frame.v1");
  });

  it("refuses anything else, an extra field, or a value past its bound", () => {
    const refused: unknown[] = [
      null, "ready", 3, [], {},
      { type: "approve" }, { type: "prompt", text: "rm -rf" }, { type: "open", url: "https://x" },
      { type: "ready", extra: 1 },
      { type: "size" }, { type: "size", height: "240" }, { type: "size", height: Number.NaN }, { type: "size", height: -1 },
      { type: "action" }, { type: "action", actionId: "" }, { type: "action", actionId: "a".repeat(65) }, { type: "action", actionId: "/other:act" },
      { type: "action", actionId: "a", args: { n: 1 } }, { type: "action", actionId: "a", args: ["x"] },
      { type: "action", actionId: "a", args: Object.fromEntries(Array.from({ length: 17 }, (_, i) => [`k${i}`, "v"])) },
      { type: "action", actionId: "a", args: { k: "v".repeat(1025) } },
      { type: "submit", values: [] }, { type: "submit", values: "x" }, { type: "submit", values: { big: "x".repeat(64 * 1024) } },
      { type: "state" }, { type: "state", value: "x".repeat(16 * 1024) },
    ];
    for (const data of refused) expect(decodeFrameMessage(data), JSON.stringify(data)?.slice(0, 60)).toBeNull();
  });
});

describe("how often a frame may say it", () => {
  it("lets each kind through at its own rate within a sliding second", () => {
    const gate = new RateGate();
    const sizes = Array.from({ length: 12 }, () => gate.allow("size", 1000));
    expect(sizes.filter(Boolean)).toHaveLength(10);
    expect(Array.from({ length: 6 }, () => gate.allow("action", 1000)).filter(Boolean)).toHaveLength(5);
    expect([gate.allow("submit", 1000), gate.allow("submit", 1500)]).toEqual([true, false]);
    expect(gate.allow("submit", 2001)).toBe(true);
    expect(gate.allow("ready", 1000)).toBe(true);
  });

  it("closes on the third dropped message in ten seconds, not on drops spread wider", () => {
    const spread = new DropCounter();
    expect([spread.record(0), spread.record(6000), spread.record(12000)]).toEqual([false, false, false]);
    const burst = new DropCounter();
    expect([burst.record(0), burst.record(100), burst.record(9000)]).toEqual([false, false, true]);
  });
});

describe("how tall a frame stands", () => {
  it("keeps to its place's range, and fills a tab", () => {
    expect(clampHeight(10, SLOT_HEIGHTS.sidebar)).toBe(120);
    expect(clampHeight(5000, SLOT_HEIGHTS.sidebar)).toBe(640);
    expect(clampHeight(300, SLOT_HEIGHTS.sidebar)).toBe(300);
    expect(clampHeight(500, SLOT_HEIGHTS["composer-trailing"])).toBe(160);
    expect(clampHeight(500, SLOT_HEIGHTS["pane-tab"])).toBeUndefined();
  });
});
