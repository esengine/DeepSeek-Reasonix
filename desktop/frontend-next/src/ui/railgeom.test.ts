import { describe, expect, it } from "vitest";
import { fold, grow, nearest, PAD, place, viewBand, within } from "./railgeom";

describe("where the message rail draws each mark", () => {
  it("spreads marks over the whole column by their place in the conversation, not bunched in the middle", () => {
    expect(place([0, 0.5, 1], 300)).toEqual([PAD, 150, 300 - PAD]);
  });

  it("leaves room at the top for the earlier-messages marker", () => {
    expect(place([0], 300, 34)[0]).toBe(PAD + 34);
  });

  it("draws marks apart while they can be told apart, and joins the crowded ones into runs", () => {
    const view = fold([20, 40, 42, 44, 46, 80]);
    expect(view.map((v) => [v.from, v.to, v.single])).toEqual([[0, 0, true], [1, 4, false], [5, 5, true]]);
    expect(view[1]).toMatchObject({ top: 40, bottom: 46 });
  });

  it("breaks a crowd into several runs rather than one bar down the whole rail", () => {
    const tops = Array.from({ length: 200 }, (_, i) => 20 + i);
    const view = fold(tops);
    expect(view.length).toBeGreaterThan(2);
    expect(view.every((v) => v.bottom - v.top <= 56)).toBe(true);
    expect(view.at(-1)!.to).toBe(199);
  });

  it("magnifies the pointed line most and its two neighbours on each side less", () => {
    expect([0, 1, 2, 3, -1].map(grow)).toEqual([12, 6, 2, 0, 0]);
  });

  it("places the viewport on the same scale as the marks, and draws nothing when everything fits", () => {
    expect(viewBand(0, 600, 600, 300)).toBeNull();
    expect(viewBand(1200, 600, 2400, 300)).toEqual({ top: PAD + 0.5 * (300 - PAD * 2), height: 0.25 * (300 - PAD * 2) });
  });

  it("picks by height: a run under the pointer, and inside it the mark at that height", () => {
    const tops = [20, 40, 42, 44, 46, 80];
    const view = fold(tops);
    expect(nearest(view, 43)).toBe(1);
    expect(nearest(view, 75)).toBe(2);
    expect(nearest(view, 200)).toBe(-1);
    expect(within(tops, view[1], 44.4)).toBe(3);
  });
});
