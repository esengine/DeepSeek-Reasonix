import { describe, expect, it } from "vitest";
import css from "./turn.css?raw";

const rule = (selector: string) => {
  const at = css.indexOf(`${selector} {`);
  return at < 0 ? "" : css.slice(css.indexOf("{", at) + 1, css.indexOf("}", at));
};

describe("the turn follows the boards", () => {
  it("puts the refusal at the left of a pending request and the grants at the right", () => {
    const deny = rule(".flow .call[data-approval] .apv-ft .btn[data-deny]");
    expect(deny).toMatch(/order:\s*-1/);
    expect(deny).toMatch(/margin-right:\s*auto/);
  });

  it("sets the turn's close at the right edge with a tick", () => {
    expect(rule(".rc[data-worked]")).toMatch(/justify-content:\s*flex-end/);
    expect(rule(".rc[data-worked] .rc-tick")).toMatch(/width:\s*14px/);
  });

  it("paints a stuck, open activity heading in the conversation's own colour so rows scroll under it unseen", () => {
    expect(rule(".activity-group[open] > summary")).toMatch(/background:\s*var\(--surface\)/);
  });
});
