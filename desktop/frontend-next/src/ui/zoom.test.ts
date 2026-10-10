// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { stepZoom } from "./zoom";
import { zoomDirection } from "./zoomkeys";
import { MockPort } from "../port/mock";

const RANGE = { min: 0.8, max: 1.8, step: 0.05 };

describe("the development fixture behaves like the kernel", () => {
  it("clamps a dragged value to the bounds it announces", async () => {
    const port = new MockPort();
    const { min, max } = (await port.appearance()).zoomRange!;
    expect((await port.saveAppearance({ zoom: max + 0.4 })).zoom).toBe(max);
    expect((await port.saveAppearance({ zoom: min - 0.3 })).zoom).toBe(min);
    expect((await port.saveAppearance({ zoom: 1.25 })).zoom).toBe(1.25);
  });
});

describe("stepping the interface scale", () => {
  it("walks the presets with the range ends as the outer rungs", () => {
    expect(stepZoom(1, RANGE, 1)).toBe(1.15);
    expect(stepZoom(1.15, RANGE, 1)).toBe(1.3);
    expect(stepZoom(1.3, RANGE, 1)).toBe(1.8);
    expect(stepZoom(1, RANGE, -1)).toBe(0.9);
    expect(stepZoom(0.9, RANGE, -1)).toBe(0.8);
  });

  it("treats unset as standard", () => {
    expect(stepZoom(undefined, RANGE, 1)).toBe(1.15);
    expect(stepZoom(0, RANGE, -1)).toBe(0.9);
  });

  it("stops at both ends", () => {
    expect(stepZoom(1.8, RANGE, 1)).toBe(1.8);
    expect(stepZoom(0.8, RANGE, -1)).toBe(0.8);
  });

  it("moves from a slider value between rungs to the next rung in that direction", () => {
    expect(stepZoom(1.07, RANGE, 1)).toBe(1.15);
    expect(stepZoom(1.07, RANGE, -1)).toBe(1);
    expect(stepZoom(1.45, RANGE, -1)).toBe(1.3);
  });

  it("resets to standard", () => {
    expect(stepZoom(1.45, RANGE, 0)).toBe(1);
    expect(stepZoom(undefined, RANGE, 0)).toBe(1);
  });

  it("does nothing before the kernel has announced a range", () => {
    expect(stepZoom(1, undefined, 1)).toBeUndefined();
  });
});

const press = (init: Partial<KeyboardEventInit>, mac = false) =>
  zoomDirection({ key: "", code: "", ctrlKey: !mac, metaKey: mac, altKey: false, shiftKey: false, ...init });

describe("which press is an interface scale chord", () => {
  it("reads the main keys", () => {
    expect(press({ key: "=", code: "Equal" })).toBe(1);
    expect(press({ key: "+", code: "Equal", shiftKey: true })).toBe(1);
    expect(press({ key: "-", code: "Minus" })).toBe(-1);
    expect(press({ key: "0", code: "Digit0" })).toBe(0);
  });

  it("reads the numeric pad", () => {
    expect(press({ key: "+", code: "NumpadAdd" })).toBe(1);
    expect(press({ key: "-", code: "NumpadSubtract" })).toBe(-1);
    expect(press({ key: "0", code: "Numpad0" })).toBe(0);
  });

  it("reads layouts whose key is not the Latin one by its position", () => {
    expect(press({ key: "à", code: "Digit0" })).toBe(0);
    expect(press({ key: "=", code: "Equal", shiftKey: true })).toBe(1);
    expect(press({ key: "-", code: "Digit6" })).toBe(-1);
    expect(press({ key: "ß", code: "Minus" })).toBe(-1);
  });

  it("uses the platform modifier alone", () => {
    expect(press({ key: "=", code: "Equal", ctrlKey: false })).toBeNull();
    expect(press({ key: "=", code: "Equal", altKey: true })).toBeNull();
    expect(press({ key: "=", code: "Equal", metaKey: true })).toBeNull();
    expect(press({ key: ")", code: "Digit0", shiftKey: true })).toBeNull();
    expect(press({ key: "r", code: "KeyR" })).toBeNull();
  });
});
