import { describe, expect, it } from "vitest";
import {
  type CardStreamItem,
  VISIBLE_BUFFER_ROWS,
  computeCardStreamItems,
} from "../src/cli/ui/layout/CardStream.js";

type C = { id: string };

function liveIds<T extends C>(items: CardStreamItem<T>[]): string[] {
  return items
    .filter((i): i is { kind: "card"; card: T } => i.kind === "card")
    .map((i) => i.card.id);
}

describe("computeCardStreamItems", () => {
  it("renders every card live when all fit inside outer+buffer", () => {
    const cards: C[] = [{ id: "a" }, { id: "b" }, { id: "c" }];
    const heights = new Map([
      ["a", 10],
      ["b", 10],
      ["c", 10],
    ]);
    const out = computeCardStreamItems(cards, heights, 0, 40);
    expect(liveIds(out)).toEqual(["a", "b", "c"]);
  });

  it("collapses far-above cards into a spacer when scrolled to the bottom", () => {
    const cards: C[] = Array.from({ length: 20 }, (_, i) => ({ id: `c${i}` }));
    const heights = new Map(cards.map((c) => [c.id, 10]));
    const out = computeCardStreamItems(cards, heights, 180, 20);
    const live = liveIds(out);
    // bottom cards visible, top cards collapsed
    expect(live).toContain("c19");
    expect(live).not.toContain("c0");
    expect(out.some((i) => i.kind === "spacer")).toBe(true);
  });

  it("keeps a card with no cached height live (so it can be measured)", () => {
    const cards: C[] = [{ id: "a" }, { id: "new" }];
    const heights = new Map([["a", 1000]]); // a is far below winStart
    const out = computeCardStreamItems(cards, heights, 1500, 20);
    expect(liveIds(out)).toContain("new");
  });

  // #700: in pinned mode, scrollRows tracks maxScroll = inner.height -
  // outer.height. A boundary card can flip live↔spacer on every render as
  // outer.height wiggles by a row or two, which changes inner.height by Δ,
  // which changes maxScroll back, ad infinitum. Quantizing the window to
  // VISIBLE_BUFFER_ROWS buckets means sub-bucket wiggles produce the SAME
  // items array — the dep array still notices the change, but the result
  // is referentially-different-but-equal so downstream metrics settle.
  it("does not toggle which cards are live for sub-bucket scrollRows shifts", () => {
    const cards: C[] = Array.from({ length: 30 }, (_, i) => ({ id: `c${i}` }));
    const heights = new Map(cards.map((c) => [c.id, 10]));
    const outerHeight = 20;
    // Pick a bucket-aligned base so scrollRows..base+(BUCKET-1) all sit in
    // the same bucket — that's the invariant the quantization guarantees.
    const baseScroll = 9 * VISIBLE_BUFFER_ROWS;
    const base = liveIds(computeCardStreamItems(cards, heights, baseScroll, outerHeight));
    for (let delta = 1; delta < VISIBLE_BUFFER_ROWS; delta++) {
      const wiggled = liveIds(
        computeCardStreamItems(cards, heights, baseScroll + delta, outerHeight),
      );
      expect(wiggled, `scrollRows ${baseScroll}+${delta} changed live set`).toEqual(base);
    }
  });

  it("does not toggle which cards are live for sub-bucket outerHeight shifts", () => {
    const cards: C[] = Array.from({ length: 30 }, (_, i) => ({ id: `c${i}` }));
    const heights = new Map(cards.map((c) => [c.id, 10]));
    const scroll = 270;
    const base = liveIds(computeCardStreamItems(cards, heights, scroll, 20));
    for (const h of [21, 22, 25, 30, 40]) {
      const wiggled = liveIds(computeCardStreamItems(cards, heights, scroll, h));
      expect(wiggled, `outerHeight ${h} changed live set`).toEqual(base);
    }
  });
});
