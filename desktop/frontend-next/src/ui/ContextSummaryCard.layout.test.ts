import { describe, expect, it } from "vitest";
import overview from "../styles/overview.css?raw";
import items from "../styles/ctxitems.css?raw";

const css = overview + "\n" + items;

const ruleFor = (selector: string): string => {
  const at = css.indexOf(selector + " {");
  if (at < 0) return "";
  return css.slice(at, css.indexOf("}", at));
};

// The card has no padding of its own: every child sets its own side margin, so
// a child that sets none sits against the card's edge and is clipped by it.
describe("the context card's expanded rows", () => {
  it("keep the card's side margin, like the rows above them", () => {
    const rule = ruleFor(".chrome-context-card:not(.studio-overview-card) > .chrome-context-items");
    expect(rule).toMatch(/margin:\s*[^;]*14px[^;]*;/);
    expect(rule).not.toMatch(/margin:\s*\d+px\s+0\b/);
  });

  it("never let a long tool name push the figure out of the card", () => {
    const span = ruleFor(".chrome-context-items span");
    expect(span).toMatch(/text-overflow:\s*ellipsis/);
    expect(span).toMatch(/min-width:\s*0/);
    expect(ruleFor(".chrome-context-items li")).toMatch(/min-width:\s*0/);
    expect(ruleFor("\n.chrome-context-items")).toMatch(/grid-template-columns:\s*minmax\(0,\s*1fr\)/);
  });
});
