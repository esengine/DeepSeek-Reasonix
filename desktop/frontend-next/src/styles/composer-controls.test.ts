import { describe, expect, it } from "vitest";
import composer from "../design/composer.css?raw";

const rule = (selector: string) => {
  const at = composer.indexOf(`\n${selector} {`);
  if (at < 0) return "";
  return composer.slice(at, composer.indexOf("}", at));
};
const px = (block: string, prop: string) => Number(new RegExp(`(?:^|[\\s;])${prop}:\\s*([0-9.]+)px`).exec(block)?.[1] ?? NaN);
const PICKERS = ".compose :is(.mode, .studio-branch, .model-picker)";

describe("the input box's toolbar controls", () => {
  it("are 28px tall: the attach, side and refine buttons, the branch chip, the permission chip", () => {
    expect(px(rule(".compose .studio-attach"), "width")).toBe(28);
    expect(px(rule(".compose .studio-attach"), "height")).toBe(28);
    expect(px(rule(PICKERS), "height")).toBe(28);
    expect(px(rule(PICKERS), "min-height")).toBe(28);
  });

  it("set the pickers in the 12px meta size, borderless until hovered", () => {
    expect(rule(PICKERS)).toMatch(/font:\s*var\(--w-reg\) var\(--fs-meta\)\/1 var\(--ui\)/);
    expect(rule(PICKERS)).toMatch(/border:\s*1px solid transparent/);
    expect(rule(`${PICKERS}:hover:not(:disabled)`)).toMatch(/background:\s*var\(--overlay\)/);
  });

  it("leave the send and stop buttons at 32px circles", () => {
    expect(px(rule(".compose .btn.send, .compose .btn.stop"), "width")).toBe(32);
    expect(px(rule(".compose .btn.send, .compose .btn.stop"), "height")).toBe(32);
    expect(rule(".compose .btn.send, .compose .btn.stop")).toMatch(/border-radius:\s*50%/);
  });
});
