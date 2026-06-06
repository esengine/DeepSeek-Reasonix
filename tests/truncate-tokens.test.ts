import { describe, expect, it } from "vitest";
import {
  DEFAULT_MAX_RESULT_TOKENS,
  truncateForModel,
  truncateForModelByTokens,
} from "../src/mcp/registry.js";
import { countTokens } from "../src/tokenizer.js";

function hasLoneSurrogate(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    const code = s.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = s.charCodeAt(i + 1);
      if (next < 0xdc00 || next > 0xdfff) return true;
      i++;
      continue;
    }
    if (code >= 0xdc00 && code <= 0xdfff) return true;
  }
  return false;
}

describe("truncateForModelByTokens", () => {
  it("returns the string unchanged when it fits the budget", () => {
    const s = "Hello, world! This is a short string.";
    expect(truncateForModelByTokens(s, 1000)).toBe(s);
  });

  it("returns the string unchanged when length <= budget (fast path)", () => {
    // Fast path: every token is ≥ 1 char, so length ≤ budget implies
    // tokens ≤ budget. No tokenize call should be needed.
    const s = "a".repeat(50);
    expect(truncateForModelByTokens(s, 100)).toBe(s);
  });

  it("truncates when tokens exceed the budget", () => {
    // 2000 "hello " tokens → well above 100-token budget
    const s = "hello ".repeat(2000);
    const out = truncateForModelByTokens(s, 100);
    expect(out).not.toBe(s);
    // Output includes the head, a truncation marker, and a short tail
    expect(out).toMatch(/…truncated ~?\d+ tokens \(\d+ chars\)/);
    // Final token count stays reasonably close to (at or below) budget
    // plus the marker's ~48-token overhead — we allow a small slack
    // because the slice refinement is char-based and can overshoot by
    // a few tokens before the retry loop settles.
    expect(countTokens(out)).toBeLessThanOrEqual(250);
  });

  it("caps CJK text at the same token footprint as English", () => {
    // 8000 chars of Chinese — roughly 5000-8000 tokens depending on
    // which chars; old char-based cap at 32000 would let this through
    // at 2× the token cost. Token cap pulls it down.
    const s = "你好世界".repeat(2000); // 8000 chars
    const out = truncateForModelByTokens(s, 500);
    expect(countTokens(out)).toBeLessThanOrEqual(700);
    expect(out).toMatch(/…truncated/);
  });

  it("preserves both the head and the tail of the content", () => {
    const head = "START-OF-RESULT\n";
    const middle = "filler line\n".repeat(5000);
    const tail = "\nEND-OF-RESULT";
    const s = head + middle + tail;
    const out = truncateForModelByTokens(s, 500);
    // Head leading sentinel is preserved at the start
    expect(out.startsWith("START-OF-RESULT")).toBe(true);
    // Tail trailing sentinel survives via the short tail window
    expect(out.endsWith("END-OF-RESULT")).toBe(true);
    expect(out).toMatch(/…truncated/);
  });

  it("returns the empty string when budget is zero or negative", () => {
    expect(truncateForModelByTokens("anything", 0)).toBe("");
    expect(truncateForModelByTokens("anything", -5)).toBe("");
  });

  it("handles the empty string", () => {
    expect(truncateForModelByTokens("", 100)).toBe("");
    expect(truncateForModelByTokens("", 0)).toBe("");
  });

  it("default token budget is 8000", () => {
    expect(DEFAULT_MAX_RESULT_TOKENS).toBe(8_000);
  });

  it("truncated marker reports a positive dropped-tokens count", () => {
    const s = "A".repeat(40_000);
    const out = truncateForModelByTokens(s, 200);
    const match = /truncated ~?(\d+) tokens \((\d+) chars\)/.exec(out);
    expect(match).not.toBeNull();
    const droppedTokens = Number(match![1]);
    const droppedChars = Number(match![2]);
    expect(droppedTokens).toBeGreaterThan(0);
    expect(droppedChars).toBeGreaterThan(0);
  });

  it("never leaves a lone UTF-16 surrogate at a slice boundary (issue #1970)", () => {
    // Emoji are UTF-16 surrogate pairs. Pack a content so the natural
    // token-budget slice lands inside one — without alignment, the head
    // would end with a lone high surrogate and DeepSeek's serde_json
    // would reject the request with "unexpected end of hex escape".
    const emoji = "🚀";
    const filler = "x".repeat(50);
    const segment = `${emoji}${filler}`;
    const s = segment.repeat(2000);
    const out = truncateForModelByTokens(s, 400);
    expect(hasLoneSurrogate(out)).toBe(false);
    expect(JSON.stringify(out)).not.toMatch(/\\u[dD][89abAB][0-9a-fA-F]{2}/);
  });
});

describe("truncateForModel (char-based)", () => {
  it("never leaves a lone UTF-16 surrogate at a slice boundary (issue #1970)", () => {
    const emoji = "🎯";
    const filler = "y".repeat(50);
    const s = `${emoji}${filler}`.repeat(2000);
    const out = truncateForModel(s, 1000);
    expect(hasLoneSurrogate(out)).toBe(false);
    expect(JSON.stringify(out)).not.toMatch(/\\u[dD][89abAB][0-9a-fA-F]{2}/);
  });
});
