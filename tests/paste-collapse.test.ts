import { describe, expect, it } from "vitest";
import { INLINE_PASTE_THRESHOLD, shouldInlinePaste } from "../src/cli/ui/PromptInput.js";
import {
  DEFAULT_PASTE_CHAR_THRESHOLD,
  DEFAULT_PASTE_HEAD_LINES,
  DEFAULT_PASTE_LINE_THRESHOLD,
  formatLongPaste,
} from "../src/cli/ui/paste-collapse.js";

describe("shouldInlinePaste — short single-line pastes render verbatim (#397)", () => {
  it("a single word inlines", () => {
    expect(shouldInlinePaste("hello")).toBe(true);
  });

  it("a number inlines", () => {
    expect(shouldInlinePaste("42")).toBe(true);
  });

  it("a sentence-length string still inlines", () => {
    expect(shouldInlinePaste("the quick brown fox jumps over the lazy dog")).toBe(true);
  });

  it("anything containing a newline becomes a sentinel chip", () => {
    expect(shouldInlinePaste("line1\nline2")).toBe(false);
    expect(shouldInlinePaste("hi\n")).toBe(false);
  });

  it("a single-line paste past the threshold becomes a sentinel chip", () => {
    expect(shouldInlinePaste("x".repeat(INLINE_PASTE_THRESHOLD))).toBe(true);
    expect(shouldInlinePaste("x".repeat(INLINE_PASTE_THRESHOLD + 1))).toBe(false);
  });
});

describe("formatLongPaste", () => {
  it("passes through short input verbatim", () => {
    const r = formatLongPaste("hello world");
    expect(r.collapsed).toBe(false);
    expect(r.displayText).toBe("hello world");
    expect(r.originalChars).toBe(11);
    expect(r.originalLines).toBe(1);
  });

  it("passes through a multi-line but small-ish input", () => {
    const r = formatLongPaste("line 1\nline 2\nline 3");
    expect(r.collapsed).toBe(false);
    expect(r.displayText).toBe("line 1\nline 2\nline 3");
    expect(r.originalLines).toBe(3);
  });

  it("collapses when line count exceeds threshold", () => {
    const input = Array.from({ length: 60 }, (_, i) => `line ${i + 1}`).join("\n");
    const r = formatLongPaste(input);
    expect(r.collapsed).toBe(true);
    expect(r.originalLines).toBe(60);
    expect(r.displayText).toMatch(/pasted/);
    expect(r.displayText).toMatch(/60 lines/);
    // Header + first 10 lines + "… (50 more lines)" footer.
    expect(r.displayText).toContain("line 1");
    expect(r.displayText).toContain("line 10");
    expect(r.displayText).not.toContain("line 11");
    expect(r.displayText).toMatch(/50 more lines/);
  });

  it("collapses when char count exceeds threshold even with few lines", () => {
    const input = "x".repeat(3000); // 1 line, 3000 chars
    const r = formatLongPaste(input);
    expect(r.collapsed).toBe(true);
    expect(r.originalChars).toBe(3000);
    expect(r.displayText).toMatch(/pasted/);
    expect(r.displayText).toMatch(/2\.9 KB|3 KB/);
  });

  it("renders bytes in KB when >= 1024", () => {
    const r = formatLongPaste(Array.from({ length: 100 }, () => "x".repeat(30)).join("\n"));
    expect(r.collapsed).toBe(true);
    // 100 * 31 = ~3.0 KB
    expect(r.displayText).toMatch(/(KB|MB)/);
  });

  it("handles single-line remaining — 'more line' not 'more lines'", () => {
    const lines = Array.from({ length: DEFAULT_PASTE_HEAD_LINES + 1 }, (_, i) => `l${i}`);
    // Still below the line threshold by default (head+1), so trigger via chars.
    const big = `${lines.join("\n")}\n${"x".repeat(DEFAULT_PASTE_CHAR_THRESHOLD + 100)}`;
    const r = formatLongPaste(big);
    expect(r.collapsed).toBe(true);
  });

  it("respects custom thresholds", () => {
    const r = formatLongPaste("line1\nline2\nline3", { lineThreshold: 2 });
    expect(r.collapsed).toBe(true);
    expect(r.displayText).toMatch(/3 lines/);
  });

  it("respects custom headLines", () => {
    const lines = Array.from({ length: 100 }, (_, i) => `l${i}`);
    const r = formatLongPaste(lines.join("\n"), { headLines: 3 });
    expect(r.collapsed).toBe(true);
    expect(r.displayText).toContain("l0");
    expect(r.displayText).toContain("l2");
    expect(r.displayText).not.toContain("l3");
    expect(r.displayText).toMatch(/97 more lines/);
  });

  it("reports 0 'more lines' when headLines covers all", () => {
    // Trigger collapse via chars, with very large headLines.
    const big = `line1\n${"x".repeat(DEFAULT_PASTE_CHAR_THRESHOLD + 100)}`;
    const r = formatLongPaste(big, { headLines: 999 });
    expect(r.collapsed).toBe(true);
    // When head covers everything, no footer is appended.
    expect(r.displayText).not.toMatch(/more line/);
  });

  it("exposes default thresholds", () => {
    expect(DEFAULT_PASTE_LINE_THRESHOLD).toBe(40);
    expect(DEFAULT_PASTE_CHAR_THRESHOLD).toBe(2000);
    expect(DEFAULT_PASTE_HEAD_LINES).toBe(10);
  });
});
