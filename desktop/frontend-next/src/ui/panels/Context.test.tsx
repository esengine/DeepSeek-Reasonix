import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Context } from "./Context";
import type { ContextBreakdown } from "../../port/port";

// A 1M window running under the default economic limit: the session folds at
// 160k, which is 16% of what the model could hold. Everything here turns on the
// two numbers being different.
const wide = (over: Partial<ContextBreakdown> = {}): ContextBreakdown => ({
  used: 82_000, window: 1_000_000, compact_at: 160_000,
  boundary: "economic", capacity_at: 800_000,
  system: 20_000, tools: 12_000, user: 10_000, reply: 20_000, output: 20_000,
  ...over,
});

const draw = (ctx: ContextBreakdown) => renderToStaticMarkup(<Context ctx={ctx} legend />);

// The bar's width is the whole claim: it is what a reader takes as "how far
// along am I", and it was measured against the wrong number.
const width = (html: string) => {
  const bars = html.match(/class="ctxbar"[^>]*>(.*?)<\/div>/s)?.[1] ?? "";
  return [...bars.matchAll(/width:\s*([\d.]+)%/g)].reduce((a, m) => a + Number(m[1]), 0);
};

describe("the context gauge's denominator", () => {
  // The defect this panel was carrying: 82k of 1M reads as 8% full, and the
  // session compacts at 160k with the bar still nearly empty. The reader
  // concludes the host folded for no reason.
  it("counts down to the fold point, not to the window", () => {
    expect(width(draw(wide()))).toBeCloseTo(51.25, 1);
  });

  it("names the deadline and what share of it is gone", () => {
    const html = draw(wide());
    expect(html).toContain("下次维护");
    expect(html).toContain("160k");
    expect(html).toContain("51%");
  });

  // Capacity does not disappear: it answers a different question — whether the
  // model is simply too small — and it is the only place a relay's wrong window
  // can be corrected.
  it("keeps the window as a second, quieter figure", () => {
    const html = draw(wide());
    expect(html).toContain("模型容量");
    expect(html).toContain("ctxcapbar");
    expect(html).toContain("1.0M");
  });

  // A full bar at the fold point and a full bar at the window are the same
  // pixels saying different things; only one of them is a deadline.
  it("does not let the capacity bar borrow the maintenance reading", () => {
    const html = draw(wide());
    const cap = html.match(/class="ctxcapbar"[^>]*>.*?width:\s*([\d.]+)%/s)?.[1];
    expect(Number(cap)).toBeCloseTo(8.2, 1);
  });
});

// The fold point was a bare number on a screen whose other number was twenty
// times larger. Nothing said which of the two bounds produced it, so the reader
// supplied the only explanation available: that the host folds for no reason.
describe("why the fold point is where it is", () => {
  it("marks the fold point on the window it is a fraction of", () => {
    const html = draw(wide());
    const notch = html.match(/class="ctxcapbar"[^>]*>.*?left:\s*([\d.]+)%/s)?.[1];
    expect(Number(notch)).toBeCloseTo(16, 1);
  });

  it("says the bound is absolute and where the window's own line sits", () => {
    const html = draw(wide());
    expect(html).toContain("ctxwhy");
    expect(html).toContain("不随窗口放大");
    expect(html).toContain("800k");
  });

  // A fold at the window's own share explains itself; repeating it there would
  // be a footnote on every session that never needed one.
  it("stays silent when the window's own share is what fires", () => {
    const html = draw(wide({ window: 200_000, compact_at: 160_000, boundary: "capacity", capacity_at: 160_000 }));
    expect(html).not.toContain("ctxwhy");
  });
});

describe("what the gauge says as the fold point approaches", () => {
  // The kernel tells the model to work narrower at 75% of the trigger and to
  // land what it knows at 92%. The panel reuses those, so the screen and the
  // conversation are never under two different pressures.
  it("stays quiet below the kernel's first rung", () => {
    const html = draw(wide({ used: 100_000 }));
    expect(html).not.toContain('data-press');
  });

  it("says the model has been told to narrow at the first rung", () => {
    const html = draw(wide({ used: 124_000 }));
    expect(html).toContain('data-press="near"');
    expect(html).toContain("收窄");
  });

  it("says maintenance is close at the second", () => {
    const html = draw(wide({ used: 150_000 }));
    expect(html).toContain('data-press="soon"');
  });

  // Compaction is routine maintenance. Painted as a warning it reads as a
  // malfunction, and every long session looks broken.
  it("never dresses routine maintenance as a fault", () => {
    const html = draw(wide({ used: 158_000 }));
    expect(html).not.toContain('data-lvl="warn"');
    expect(html).not.toContain('data-lvl="err"');
  });
});

describe("when there is no fold point to count down to", () => {
  // A ratio placed past the window is how maintenance is retired. There is no
  // deadline left, so inventing one would be a countdown to nothing.
  it("draws the window alone once the fold point is out of reach", () => {
    const html = draw(wide({ compact_at: 1_200_000 }));
    expect(html).not.toContain("下次维护");
    // The one ceiling left still says which one it is. What it must not do is
    // call itself a deadline.
    expect(html).toContain("模型容量");
    expect(width(html)).toBeCloseTo(8.2, 1);
  });

  // An undeclared window is what turns maintenance off entirely, and it says so
  // rather than drawing a gauge with no denominator.
  it("keeps saying why an undeclared window draws nothing", () => {
    const html = draw(wide({ window: 0, compact_at: 0 }));
    expect(html).toContain("不会自动压缩");
    expect(html).not.toContain("ctxbar");
  });
});

// One usage figure, two ceilings, each answering for itself. Merging them into
// a single percentage of something unstated is the failure this guards: "6%"
// alone cannot say whether it is 6% of the fold point or of the window.
describe("usage is one number and the ceilings are two questions", () => {
  const usage = (html: string) => [...html.matchAll(/82k/g)].length;

  it("draws the usage figure once", () => {
    expect(usage(draw(wide()))).toBe(1);
  });

  it("keeps both ceilings, each with its own denominator named", () => {
    const html = draw(wide());
    expect(html).toContain("下次维护");
    expect(html).toContain("160k");
    expect(html).toContain("模型容量");
    expect(html).toContain("1.0M");
  });

  it("gives each ceiling its own share, not one shared percentage", () => {
    const html = draw(wide());
    expect(html).toContain("51%");   // of the fold point
    expect(html).toContain("8%");    // of the window
  });
});
