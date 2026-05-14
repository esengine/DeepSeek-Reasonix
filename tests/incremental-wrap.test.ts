import { describe, expect, it } from "vitest";
import { type WrapCache, wrapIncremental } from "../src/cli/ui/cards/useIncrementalWrap.js";
import { wrapToCells } from "../src/frame/width.js";

function fullWrap(text: string, lineCells: number): string[] {
  if (text.length === 0) return [""];
  return text.split("\n").flatMap((l) => wrapToCells(l, lineCells));
}

function feed(chunks: string[], lineCells: number): { cache: WrapCache; calls: number } {
  let cache: WrapCache | null = null;
  let acc = "";
  let calls = 0;
  for (const chunk of chunks) {
    acc += chunk;
    cache = wrapIncremental(acc, lineCells, cache);
    calls += 1;
  }
  return { cache: cache!, calls };
}

describe("wrapIncremental", () => {
  it("matches full wrap when fed empty text", () => {
    const cache = wrapIncremental("", 80, null);
    expect(cache.visualLines).toEqual(fullWrap("", 80));
  });

  it("matches full wrap for single short line", () => {
    const cache = wrapIncremental("hello world", 80, null);
    expect(cache.visualLines).toEqual(fullWrap("hello world", 80));
  });

  it("matches full wrap for a long line that wraps", () => {
    const text = "a".repeat(250);
    const cache = wrapIncremental(text, 80, null);
    expect(cache.visualLines).toEqual(fullWrap(text, 80));
    expect(cache.visualLines.length).toBeGreaterThan(1);
  });

  it("matches full wrap when growing one char at a time across newlines", () => {
    const target = "first line\nsecond longer line that may wrap\nthird";
    let cache: WrapCache | null = null;
    let acc = "";
    for (const ch of target) {
      acc += ch;
      cache = wrapIncremental(acc, 20, cache);
      expect(cache.visualLines).toEqual(fullWrap(acc, 20));
    }
  });

  it("matches full wrap when adding chunks of varying size", () => {
    const chunks = [
      "Hel",
      "lo wor",
      "ld!\nA new line begins ",
      "here and grows quite long enough to wrap multiple times.\n",
      "\n",
      "final ",
      "tail",
    ];
    const { cache } = feed(chunks, 20);
    const full = chunks.join("");
    expect(cache.visualLines).toEqual(fullWrap(full, 20));
  });

  it("falls back to full recompute when text shrinks (abort)", () => {
    const grown = wrapIncremental("abcdef", 80, null);
    const shrunk = wrapIncremental("abc", 80, grown);
    expect(shrunk.visualLines).toEqual(fullWrap("abc", 80));
  });

  it("falls back to full recompute when lineCells changes (terminal resize)", () => {
    const wide = wrapIncremental("a".repeat(200), 80, null);
    const narrow = wrapIncremental("a".repeat(200), 40, wide);
    expect(narrow.visualLines).toEqual(fullWrap("a".repeat(200), 40));
    expect(narrow.visualLines.length).toBeGreaterThan(wide.visualLines.length);
  });

  it("falls back to full recompute when prev is not a prefix of next", () => {
    const a = wrapIncremental("hello", 80, null);
    const b = wrapIncremental("hellx", 80, a);
    expect(b.visualLines).toEqual(fullWrap("hellx", 80));
  });

  it("handles trailing newline", () => {
    let cache: WrapCache | null = null;
    cache = wrapIncremental("abc", 80, cache);
    cache = wrapIncremental("abc\n", 80, cache);
    expect(cache.visualLines).toEqual(fullWrap("abc\n", 80));
  });

  it("handles consecutive newlines", () => {
    let cache: WrapCache | null = null;
    cache = wrapIncremental("a", 80, cache);
    cache = wrapIncremental("a\n\n\nb", 80, cache);
    expect(cache.visualLines).toEqual(fullWrap("a\n\n\nb", 80));
  });

  it("preserves grapheme correctness when a long line is built incrementally", () => {
    const tail = "long line with emojis 🚀🚀 and CJK 你好世界";
    let cache: WrapCache | null = null;
    let acc = "";
    for (const ch of tail) {
      acc += ch;
      cache = wrapIncremental(acc, 12, cache);
    }
    expect(cache?.visualLines).toEqual(fullWrap(tail, 12));
  });

  it("freezes committed logical lines across monotonic appends", () => {
    const committed = `${"a".repeat(500)}\n`;
    let cache: WrapCache | null = wrapIncremental(committed, 80, null);
    const committedVisual = cache.visualLines.slice(0, -1);
    for (const ch of "tail content keeps growing here") {
      cache = wrapIncremental(cache.text + ch, 80, cache);
      expect(cache.visualLines.slice(0, committedVisual.length)).toEqual(committedVisual);
      expect(cache.visualLines).toEqual(fullWrap(cache.text, 80));
    }
  });
});
