import { describe, expect, it } from "vitest";
import studio from "./studio.css?raw";

const rule = (selector: string) => {
  const at = studio.indexOf(`\n${selector} {`);
  if (at < 0) return "";
  return studio.slice(at, studio.indexOf("}", at));
};
const px = (block: string, prop: string) => Number(new RegExp(`(?:^|[\\s;])${prop}:\\s*([0-9.]+)px`).exec(block)?.[1] ?? NaN);

describe("the input box's toolbar controls", () => {
  it("are 28px tall: the attach, side and refine buttons, the branch chip, the permission chip", () => {
    expect(px(rule(".studio-attach"), "width")).toBe(28);
    expect(px(rule(".studio-attach"), "height")).toBe(28);
    expect(px(rule(".compose .studio-branch"), "min-height")).toBe(28);
    expect(px(rule(".compose .policy > .mode"), "min-height")).toBe(28);
    expect(px(rule(".compose .mode"), "min-height")).toBe(28);
    expect(px(rule(".studio-mode-picker"), "min-height")).toBe(28);
  });

  it("leave the send button at 32px", () => {
    expect(px(rule(".btn.send"), "width")).toBe(32);
    expect(px(rule(".btn.send"), "height")).toBe(32);
  });
});
