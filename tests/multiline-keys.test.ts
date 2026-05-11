import { describe, expect, it } from "vitest";
import { collapseLinesForDisplay } from "../src/cli/ui/PromptInput.js";
import {
  type MultilineKey,
  lineAndColumn,
  processMultilineKey,
} from "../src/cli/ui/multiline-keys.js";

function key(overrides: Partial<MultilineKey> = {}): MultilineKey {
  return { input: "", ...overrides };
}

describe("processMultilineKey — inserts at cursor", () => {
  it("inserts a printable char at the cursor (mid-string)", () => {
    // "heo", cursor after "he" → insert "ll" in the middle
    const r = processMultilineKey("heo", 2, key({ input: "ll" }));
    expect(r.next).toBe("hello");
    expect(r.cursor).toBe(4);
  });

  it("appends when cursor is at end", () => {
    const r = processMultilineKey("hel", 3, key({ input: "l" }));
    expect(r.next).toBe("hell");
    expect(r.cursor).toBe(4);
  });

  it("inserts at position 0", () => {
    const r = processMultilineKey("bc", 0, key({ input: "a" }));
    expect(r.next).toBe("abc");
    expect(r.cursor).toBe(1);
  });

  it("multi-char paste burst inserts as a block", () => {
    const r = processMultilineKey("ab", 1, key({ input: "XYZ" }));
    expect(r.next).toBe("aXYZb");
    expect(r.cursor).toBe(4);
  });

  it("pasted newline-containing input surfaces as a pasteRequest (not direct insert)", () => {
    // 0.8 changed paste handling: multi-char input with a newline is
    // routed up as `pasteRequest` so the parent can register the
    // blob and insert ONE sentinel codepoint instead of inlining
    // the whole content. Direct insertion only happens for typed
    // input without a newline.
    const r = processMultilineKey("a", 1, key({ input: "\nmore" }));
    expect(r.next).toBeNull();
    expect(r.submit).toBe(false);
    expect(r.pasteRequest?.content).toBe("\nmore");
  });
});

describe("processMultilineKey — submit + newline", () => {
  it("Enter submits by default", () => {
    const r = processMultilineKey("hi", 2, key({ return: true }));
    expect(r.submit).toBe(true);
    expect(r.submitValue).toBe("hi");
  });

  it("Shift+Enter inserts a newline at the cursor", () => {
    const r = processMultilineKey("abc", 1, key({ return: true, shift: true }));
    expect(r.next).toBe("a\nbc");
    expect(r.cursor).toBe(2);
    expect(r.submit).toBe(false);
  });

  it("Ctrl+J inserts a newline at the cursor (ASCII LF form)", () => {
    const r = processMultilineKey("abc", 2, key({ input: "\n" }));
    expect(r.next).toBe("ab\nc");
    expect(r.cursor).toBe(3);
  });

  it("Ctrl+J normalized as ctrl+'j' also inserts a newline", () => {
    const r = processMultilineKey("abc", 2, key({ input: "j", ctrl: true }));
    expect(r.next).toBe("ab\nc");
  });

  it("Enter with trailing \\\\ at end-of-buffer → bash continuation", () => {
    const v = "line1\\";
    const r = processMultilineKey(v, v.length, key({ return: true }));
    expect(r.submit).toBe(false);
    expect(r.next).toBe("line1\n");
    expect(r.cursor).toBe(6);
  });

  it("Enter with \\\\ mid-buffer (cursor not at end) does NOT trigger continuation", () => {
    // User has "foo\\bar" and hits Enter with cursor after "foo\\" — that's
    // a real edit, not a continuation marker. Submit instead.
    const r = processMultilineKey("foo\\bar", 4, key({ return: true }));
    expect(r.submit).toBe(true);
    expect(r.next).toBeNull();
  });

  it("plain Enter on an empty buffer still submits", () => {
    const r = processMultilineKey("", 0, key({ return: true }));
    expect(r.submit).toBe(true);
    expect(r.submitValue).toBe("");
  });
});

describe("processMultilineKey — deletion", () => {
  it("Backspace deletes the char BEFORE the cursor, cursor moves back", () => {
    const r = processMultilineKey("abcd", 2, key({ backspace: true }));
    expect(r.next).toBe("acd");
    expect(r.cursor).toBe(1);
  });

  it("Backspace at cursor 0 is a no-op", () => {
    const r = processMultilineKey("abc", 0, key({ backspace: true }));
    expect(r.next).toBeNull();
    expect(r.cursor).toBeNull();
  });

  it("Delete behaves like Backspace (unified — some Windows terminals report Backspace as delete)", () => {
    const r = processMultilineKey("abcd", 2, key({ delete: true }));
    expect(r.next).toBe("acd");
    expect(r.cursor).toBe(1);
  });

  it("Delete at cursor 0 is a no-op (nothing before the cursor)", () => {
    const r = processMultilineKey("abc", 0, key({ delete: true }));
    expect(r.next).toBeNull();
  });

  it("raw DEL byte (0x7f) in key.input is treated as backspace", () => {
    const r = processMultilineKey("abcd", 2, key({ input: "\x7f" }));
    expect(r.next).toBe("acd");
    expect(r.cursor).toBe(1);
  });

  it("raw BS byte (0x08) in key.input is treated as backspace", () => {
    const r = processMultilineKey("abcd", 2, key({ input: "\b" }));
    expect(r.next).toBe("acd");
    expect(r.cursor).toBe(1);
  });

  it("Backspace across a newline removes the newline", () => {
    const r = processMultilineKey("a\nb", 2, key({ backspace: true }));
    expect(r.next).toBe("ab");
    expect(r.cursor).toBe(1);
  });
});

describe("processMultilineKey — cursor motion", () => {
  it("←/→ clamp to the buffer", () => {
    expect(processMultilineKey("abc", 2, key({ leftArrow: true })).cursor).toBe(1);
    expect(processMultilineKey("abc", 0, key({ leftArrow: true })).cursor).toBe(0);
    expect(processMultilineKey("abc", 2, key({ rightArrow: true })).cursor).toBe(3);
    expect(processMultilineKey("abc", 3, key({ rightArrow: true })).cursor).toBe(3);
  });

  it("↑/↓ are NOOP — reserved for chat scroll at the App level", () => {
    expect(processMultilineKey("hello", 3, key({ upArrow: true }))).toEqual({
      next: null,
      cursor: null,
      submit: false,
    });
    expect(processMultilineKey("hello", 3, key({ downArrow: true }))).toEqual({
      next: null,
      cursor: null,
      submit: false,
    });
    expect(processMultilineKey("", 0, key({ upArrow: true })).historyHandoff).toBeUndefined();
    expect(processMultilineKey("", 0, key({ downArrow: true })).historyHandoff).toBeUndefined();
    expect(processMultilineKey("hello\nworld", 9, key({ upArrow: true })).cursor).toBeNull();
  });

  it("Ctrl+P / Ctrl+N on single-line / empty buffer hand off to history recall", () => {
    expect(processMultilineKey("", 0, key({ ctrl: true, input: "p" }))).toEqual({
      next: null,
      cursor: null,
      submit: false,
      historyHandoff: "prev",
    });
    expect(processMultilineKey("", 0, key({ ctrl: true, input: "n" }))).toEqual({
      next: null,
      cursor: null,
      submit: false,
      historyHandoff: "next",
    });
    expect(processMultilineKey("hello", 3, key({ ctrl: true, input: "p" })).historyHandoff).toBe(
      "prev",
    );
    expect(processMultilineKey("hello", 3, key({ ctrl: true, input: "n" })).historyHandoff).toBe(
      "next",
    );
  });

  it("Ctrl+P moves cursor to the previous line in a multi-line buffer (readline parity)", () => {
    //  line 0: "hello" (cols 0-5)
    //  line 1: "world" (cols 0-5)
    //  cursor at col 3 on line 1 = index 9
    const v = "hello\nworld";
    const up = processMultilineKey(v, 9, key({ ctrl: true, input: "p" }));
    expect(up.cursor).toBe(3);
    expect(up.historyHandoff).toBeUndefined();
  });

  it("Ctrl+P clamps column when the previous line is shorter", () => {
    const v = "hi\nworld";
    const up = processMultilineKey(v, 7, key({ ctrl: true, input: "p" }));
    expect(up.cursor).toBe(2);
  });

  it("Ctrl+N moves cursor to the next line, preserving column", () => {
    const v = "hello\nworld";
    const down = processMultilineKey(v, 2, key({ ctrl: true, input: "n" }));
    expect(down.cursor).toBe(8);
  });

  it("Ctrl+N clamps column when the next line is shorter", () => {
    const v = "world\nhi";
    const down = processMultilineKey(v, 4, key({ ctrl: true, input: "n" }));
    expect(down.cursor).toBe(8);
  });

  it("Ctrl+P at line 0 of a multi-line buffer falls back to history (no cursor move available)", () => {
    const v = "first\nsecond";
    const up = processMultilineKey(v, 3, key({ ctrl: true, input: "p" }));
    expect(up.historyHandoff).toBe("prev");
    expect(up.cursor).toBeNull();
  });

  it("Ctrl+N at last line of a multi-line buffer falls back to history", () => {
    const v = "first\nsecond";
    const down = processMultilineKey(v, 8, key({ ctrl: true, input: "n" }));
    expect(down.historyHandoff).toBe("next");
    expect(down.cursor).toBeNull();
  });

  it("raw `\\x1b[A` / `\\x1b[B` escape sequences are NOOP (chat-scroll handled at App level)", () => {
    expect(processMultilineKey("", 0, { input: "\x1b[A" })).toEqual({
      next: null,
      cursor: null,
      submit: false,
    });
    expect(processMultilineKey("", 0, { input: "\x1b[B" })).toEqual({
      next: null,
      cursor: null,
      submit: false,
    });
  });

  it("raw `\\x1b[C` rightArrow / `\\x1b[D` leftArrow still move the cursor", () => {
    expect(processMultilineKey("abc", 0, { input: "\x1b[C" }).cursor).toBe(1);
    expect(processMultilineKey("abc", 2, { input: "\x1b[D" }).cursor).toBe(1);
  });

  it("ESC-stripped arrow fallbacks (`[C`, `[D`) — Windows ConPTY case", () => {
    // PowerShell + ConPTY consumes the leading \x1b and routes the
    // remaining `[C` through useInput as plain text. Without the
    // ESC-less fallback, pressing right-arrow at end of a line would
    // insert literal `[C` instead of moving the cursor across the
    // newline boundary.
    expect(processMultilineKey("ab\ncd", 2, { input: "[C" }).cursor).toBe(3);
    expect(processMultilineKey("ab\ncd", 3, { input: "[D" }).cursor).toBe(2);
  });

  it("Ctrl+A jumps to start of current line, Ctrl+E to end", () => {
    const v = "one\ntwo\nthree";
    // cursor mid-"two" at index 5 (o in two)
    expect(processMultilineKey(v, 5, key({ input: "a", ctrl: true })).cursor).toBe(4);
    expect(processMultilineKey(v, 5, key({ input: "e", ctrl: true })).cursor).toBe(7);
  });
});

describe("processMultilineKey — parent-owned keys are ignored", () => {
  it("Tab / Escape / PageUp / PageDown are dropped", () => {
    expect(processMultilineKey("x", 1, key({ tab: true }))).toEqual({
      next: null,
      cursor: null,
      submit: false,
    });
    expect(processMultilineKey("x", 1, key({ escape: true })).next).toBeNull();
    expect(processMultilineKey("x", 1, key({ pageUp: true })).next).toBeNull();
    expect(processMultilineKey("x", 1, key({ pageDown: true })).next).toBeNull();
  });

  it("unhandled Ctrl-<letter> chords are dropped (no accidental insert)", () => {
    const r = processMultilineKey("x", 1, key({ input: "c", ctrl: true }));
    expect(r.next).toBeNull();
    expect(r.cursor).toBeNull();
  });

  it("Meta (Alt) key events are dropped", () => {
    const r = processMultilineKey("x", 1, key({ input: "a", meta: true }));
    expect(r.next).toBeNull();
  });
});

describe("processMultilineKey — buffer-wide navigation + clear shortcuts", () => {
  it("PageUp jumps cursor to position 0 (top of buffer)", () => {
    const v = "line1\nline2\nline3\nline4";
    const r = processMultilineKey(v, 18, key({ pageUp: true }));
    expect(r.cursor).toBe(0);
  });

  it("PageDown jumps cursor to value.length (end of buffer)", () => {
    const v = "line1\nline2\nline3\nline4";
    const r = processMultilineKey(v, 0, key({ pageDown: true }));
    expect(r.cursor).toBe(v.length);
  });

  it("PageUp at top is a no-op (no cursor churn)", () => {
    const r = processMultilineKey("hello", 0, key({ pageUp: true }));
    expect(r).toEqual({ next: null, cursor: null, submit: false });
  });

  it("PageDown at end is a no-op", () => {
    const v = "hello";
    const r = processMultilineKey(v, v.length, key({ pageDown: true }));
    expect(r).toEqual({ next: null, cursor: null, submit: false });
  });

  it("Ctrl+U clears the entire buffer", () => {
    const r = processMultilineKey("a long\nmulti-line\npaste", 7, key({ input: "u", ctrl: true }));
    expect(r.next).toBe("");
    expect(r.cursor).toBe(0);
  });

  it("Ctrl+U on empty buffer is a no-op", () => {
    const r = processMultilineKey("", 0, key({ input: "u", ctrl: true }));
    expect(r).toEqual({ next: null, cursor: null, submit: false });
  });

  it("Ctrl+W deletes the word before the cursor", () => {
    // cursor at end of "hello world", deletes "world"
    const r = processMultilineKey("hello world", 11, key({ input: "w", ctrl: true }));
    expect(r.next).toBe("hello ");
    expect(r.cursor).toBe(6);
  });

  it("Ctrl+W eats trailing whitespace then the previous word", () => {
    // cursor after "hello   " (3 spaces). Should delete the spaces AND "hello".
    const r = processMultilineKey("hello   ", 8, key({ input: "w", ctrl: true }));
    expect(r.next).toBe("");
    expect(r.cursor).toBe(0);
  });

  it("Ctrl+W mid-word stops at the word's start", () => {
    // cursor in middle of "hello", deletes "hel"
    const r = processMultilineKey("hello world", 3, key({ input: "w", ctrl: true }));
    expect(r.next).toBe("lo world");
    expect(r.cursor).toBe(0);
  });

  it("Ctrl+W at start of buffer is a no-op", () => {
    const r = processMultilineKey("hello", 0, key({ input: "w", ctrl: true }));
    expect(r).toEqual({ next: null, cursor: null, submit: false });
  });

  it("Ctrl+W spans newlines — deletes the previous line's last word from line start", () => {
    // cursor at start of "world" line (index 6), Ctrl+W deletes "hello\n"
    const r = processMultilineKey("hello\nworld", 6, key({ input: "w", ctrl: true }));
    expect(r.next).toBe("world");
    expect(r.cursor).toBe(0);
  });
});

describe("processMultilineKey — Home/End/Ctrl+K/Alt-word", () => {
  it("Home jumps to start of current line (multi-line)", () => {
    // mid "two", index 5 → start of "two" at index 4
    const r = processMultilineKey("one\ntwo\nthree", 5, key({ home: true }));
    expect(r.cursor).toBe(4);
  });

  it("End jumps to end of current line (multi-line)", () => {
    // mid "two", index 5 → end of "two" at index 7
    const r = processMultilineKey("one\ntwo\nthree", 5, key({ end: true }));
    expect(r.cursor).toBe(7);
  });

  it("Ctrl+K kills from cursor to end of current line", () => {
    const r = processMultilineKey("hello world\nbye", 5, key({ input: "k", ctrl: true }));
    expect(r.next).toBe("hello\nbye");
    expect(r.cursor).toBe(5);
  });

  it("Ctrl+K at end-of-line is a no-op (does not eat the newline)", () => {
    const r = processMultilineKey("hello\nbye", 5, key({ input: "k", ctrl: true }));
    expect(r).toEqual({ next: null, cursor: null, submit: false });
  });

  it("Alt+B moves cursor to start of previous word", () => {
    const r = processMultilineKey("hello world", 11, key({ input: "b", meta: true }));
    expect(r.cursor).toBe(6);
    expect(r.next).toBeNull();
  });

  it("Alt+B at start of buffer is a no-op", () => {
    const r = processMultilineKey("hello", 0, key({ input: "b", meta: true }));
    expect(r).toEqual({ next: null, cursor: null, submit: false });
  });

  it("Alt+F moves cursor to end of next word", () => {
    const r = processMultilineKey("hello world", 0, key({ input: "f", meta: true }));
    expect(r.cursor).toBe(5);
    expect(r.next).toBeNull();
  });

  it("Alt+F at end of buffer is a no-op", () => {
    const v = "hello";
    const r = processMultilineKey(v, v.length, key({ input: "f", meta: true }));
    expect(r).toEqual({ next: null, cursor: null, submit: false });
  });

  it("Alt+Backspace deletes the previous word (Ctrl+W alias)", () => {
    const r = processMultilineKey("hello world", 11, key({ meta: true, backspace: true }));
    expect(r.next).toBe("hello ");
    expect(r.cursor).toBe(6);
  });
});

describe("processMultilineKey — paste burst handling", () => {
  it("paste with embedded \\n surfaces a pasteRequest, does NOT submit even with key.return set", () => {
    // Repro of the reported bug: Ink occasionally sets key.return on
    // a paste whose trailing \n looks like Enter. Pre-fix this would
    // submit the partial buffer mid-paste. Now the reducer hands the
    // paste up as a `pasteRequest` and never touches `submit`.
    const r = processMultilineKey("", 0, key({ input: "line1\nline2\nline3", return: true }));
    expect(r.submit).toBe(false);
    expect(r.next).toBeNull();
    expect(r.pasteRequest?.content).toBe("line1\nline2\nline3");
  });

  it("paste normalizes CRLF and bare CR to LF (Windows clipboard / web copy)", () => {
    const r1 = processMultilineKey("", 0, key({ input: "a\r\nb\r\nc" }));
    expect(r1.pasteRequest?.content).toBe("a\nb\nc");
    const r2 = processMultilineKey("", 0, key({ input: "x\ry\rz" }));
    expect(r2.pasteRequest?.content).toBe("x\ny\nz");
  });

  it("strips bracketed-paste markers if they leak through (DECSET 2004 supported terminals)", () => {
    const wrapped = "\u001b[200~hello\nworld\u001b[201~";
    const r = processMultilineKey("", 0, key({ input: wrapped }));
    expect(r.pasteRequest?.content).toBe("hello\nworld");
    expect(r.submit).toBe(false);
  });

  it("strips ESC-less paste markers (Windows PowerShell + ConPTY case)", () => {
    // Ink's parse-keypress eats the leading \x1b, leaving bare `[200~` /
    // `[201~` in `input`. Without the fallback strip the literal
    // `[201~` ends up inserted into the user's prompt buffer.
    const r = processMultilineKey("", 0, key({ input: "[200~hello\nworld[201~" }));
    expect(r.pasteRequest?.content).toBe("hello\nworld");
    expect(r.submit).toBe(false);
  });

  it("real Enter (input='', return=true) still submits — not mistaken for paste", () => {
    const r = processMultilineKey("foo", 3, key({ input: "", return: true }));
    expect(r.submit).toBe(true);
    expect(r.submitValue).toBe("foo");
  });

  it("single-char Ctrl+J still inserts one newline directly (no pasteRequest)", () => {
    const r = processMultilineKey("ab", 1, key({ input: "j", ctrl: true }));
    expect(r.next).toBe("a\nb");
    expect(r.pasteRequest).toBeUndefined();
  });
});

describe("collapseLinesForDisplay — big-paste mitigation", () => {
  it("returns all lines unchanged when under the threshold", () => {
    const lines = ["a", "b", "c"];
    const out = collapseLinesForDisplay(lines, 1);
    expect(out).toHaveLength(3);
    expect(out.every((x) => x.kind === "line")).toBe(true);
  });

  it("collapses long buffers into head + cursor + tail with skip markers", () => {
    // 30 lines, cursor on line 15 (middle). Should render first 3,
    // cursor line, last 2 — plus skip markers between the runs.
    const lines = Array.from({ length: 30 }, (_, i) => `line${i}`);
    const out = collapseLinesForDisplay(lines, 15);
    const kinds = out.map((x) => x.kind);
    // Shape: line×3, skip, line(cursor), skip, line×2
    expect(kinds).toEqual(["line", "line", "line", "skip", "line", "skip", "line", "line"]);
    // Cursor-line preserves its original index so the `you ›` prefix
    // and the cursor column still line up with the correct row.
    const cursorItem = out.find((x) => x.kind === "line" && x.originalIndex === 15);
    expect(cursorItem).toBeDefined();
  });

  it("does NOT inject a skip marker when runs are adjacent", () => {
    // Cursor on line 2 (already inside head=0..2). Head covers 0..2,
    // tail covers 28..29. The cursor overlaps the head, so no
    // middle skip is needed — only the gap between head and tail.
    const lines = Array.from({ length: 30 }, (_, i) => `${i}`);
    const out = collapseLinesForDisplay(lines, 2);
    const kinds = out.map((x) => x.kind);
    expect(kinds).toEqual(["line", "line", "line", "skip", "line", "line"]);
  });

  it("hidden count adds up to total - visible", () => {
    const lines = Array.from({ length: 100 }, (_, i) => `${i}`);
    const out = collapseLinesForDisplay(lines, 50);
    const hidden = out.reduce(
      (sum, item) => sum + (item.kind === "skip" ? item.linesHidden : 0),
      0,
    );
    const visible = out.filter((x) => x.kind === "line").length;
    expect(hidden + visible).toBe(100);
  });
});

describe("lineAndColumn", () => {
  it("maps a cursor offset to {line, col}", () => {
    expect(lineAndColumn("abc", 2)).toEqual({ line: 0, col: 2 });
    expect(lineAndColumn("abc\ndef", 4)).toEqual({ line: 1, col: 0 });
    expect(lineAndColumn("abc\ndef", 6)).toEqual({ line: 1, col: 2 });
    expect(lineAndColumn("a\n\nb", 2)).toEqual({ line: 1, col: 0 });
    expect(lineAndColumn("a\n\nb", 3)).toEqual({ line: 2, col: 0 });
  });

  it("clamps cursor values past value.length", () => {
    expect(lineAndColumn("abc", 99)).toEqual({ line: 0, col: 3 });
  });
});

describe("processMultilineKey — Ctrl+X opens external editor (issue #647)", () => {
  it("Ctrl+X returns openExternalEditor flag without mutating the buffer", () => {
    const action = processMultilineKey("draft", 3, key({ ctrl: true, input: "x" }));
    expect(action.openExternalEditor).toBe(true);
    expect(action.next).toBeNull();
    expect(action.cursor).toBeNull();
    expect(action.submit).toBe(false);
  });

  it("plain 'x' (no ctrl) still inserts as a character", () => {
    const action = processMultilineKey("a", 1, key({ input: "x" }));
    expect(action.openExternalEditor).toBeUndefined();
    expect(action.next).toBe("ax");
  });
});
