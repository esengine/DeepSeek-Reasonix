import { describe, expect, it } from "vitest";
import {
  clipToCells,
  graphemeWidth,
  graphemes,
  padToCells,
  stringWidth,
  wrapToCells,
} from "../src/cli/ui/text-width.js";

describe("graphemeWidth", () => {
  it("returns 1 for ASCII", () => {
    for (const ch of "abcXYZ0123!@#") {
      expect(graphemeWidth(ch)).toBe(1);
    }
  });
  it("returns 2 for CJK", () => {
    for (const ch of "你好世界中文漢字") {
      expect(graphemeWidth(ch)).toBe(2);
    }
  });
  it("returns 2 for Hiragana / Katakana / Hangul", () => {
    expect(graphemeWidth("あ")).toBe(2);
    expect(graphemeWidth("ア")).toBe(2);
    expect(graphemeWidth("한")).toBe(2);
  });
  it("returns 2 for common emoji", () => {
    expect(graphemeWidth("😀")).toBe(2);
    expect(graphemeWidth("🎉")).toBe(2);
  });
  it("returns 0 for combining marks / ZWJ / variation selectors", () => {
    expect(graphemeWidth("\u0301")).toBe(0);
    expect(graphemeWidth("\u200D")).toBe(0);
    expect(graphemeWidth("\uFE0F")).toBe(0);
  });
  it("returns 0 for control chars", () => {
    expect(graphemeWidth("\x00")).toBe(0);
    expect(graphemeWidth("\x1B")).toBe(0);
  });
});

describe("graphemes / stringWidth", () => {
  it("clusters ZWJ emoji as one grapheme", () => {
    const family = "👨‍👩‍👧";
    expect(graphemes(family).length).toBe(1);
    expect(stringWidth(family)).toBe(2);
  });
  it("sums widths for mixed scripts", () => {
    expect(stringWidth("hello 你好")).toBe(5 + 1 + 4);
  });
  it("handles combining diacriticals", () => {
    expect(stringWidth("e\u0301")).toBe(1);
  });
});

describe("clipToCells", () => {
  it("returns the string untouched when it fits", () => {
    expect(clipToCells("hello", 10)).toBe("hello");
    expect(clipToCells("你好", 10)).toBe("你好");
  });
  it("appends an ellipsis when truncated", () => {
    const out = clipToCells("hello world", 8);
    expect(out.endsWith("…")).toBe(true);
    expect(stringWidth(out)).toBeLessThanOrEqual(8);
    expect(out).toBe("hello w…");
  });
  it("never splits a wide grapheme across the cap", () => {
    expect(stringWidth(clipToCells("一二三四五", 5))).toBeLessThanOrEqual(5);
    expect(clipToCells("一二三四五", 5)).toBe("一二…");
  });
  it("preserves ZWJ emoji clusters when clipping", () => {
    const clipped = clipToCells("hi 👨‍👩‍👧 there", 6);
    expect(stringWidth(clipped)).toBeLessThanOrEqual(6);
    expect(clipped.endsWith("…")).toBe(true);
  });
  it("returns empty string at zero cap", () => {
    expect(clipToCells("anything", 0)).toBe("");
  });
});

describe("padToCells", () => {
  it("right-pads to the requested cell width for ASCII", () => {
    expect(padToCells("hi", 6)).toBe("hi    ");
    expect(stringWidth(padToCells("hi", 6))).toBe(6);
  });
  it("counts CJK as 2 cells when padding — padEnd would over-pad these", () => {
    // "你好" = 2 chars, 4 cells. Need 2 spaces (not 8) to reach 6 cells.
    expect(padToCells("你好", 6)).toBe("你好  ");
    expect(stringWidth(padToCells("你好", 6))).toBe(6);
  });
  it("returns the input untouched when it already fills or exceeds the cells", () => {
    expect(padToCells("hello", 5)).toBe("hello");
    expect(padToCells("一二三", 4)).toBe("一二三"); // 6 cells > 4 → no pad, no trim
  });
});

describe("wrapToCells", () => {
  it('returns [""] for empty input so paragraph breaks survive', () => {
    expect(wrapToCells("", 10)).toEqual([""]);
  });
  it("returns one chunk when input fits", () => {
    expect(wrapToCells("hello", 10)).toEqual(["hello"]);
  });
  it("breaks ASCII on cell boundary", () => {
    expect(wrapToCells("hello world!", 5)).toEqual(["hello", " worl", "d!"]);
  });
  it("never splits CJK across a cell boundary", () => {
    const out = wrapToCells("一二三四五", 5);
    for (const chunk of out) {
      expect(stringWidth(chunk)).toBeLessThanOrEqual(5);
    }
    expect(out.join("")).toBe("一二三四五");
  });
  it("preserves ZWJ emoji as one cluster across wraps", () => {
    const out = wrapToCells("a👨‍👩‍👧b", 3);
    expect(out.join("")).toBe("a👨‍👩‍👧b");
  });
});
