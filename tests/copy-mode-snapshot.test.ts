import { describe, expect, it } from "vitest";
import { buildSnapshot, isYankable, yankRange } from "../src/cli/ui/copy-mode/snapshot.js";
import type { Card } from "../src/cli/ui/state/cards.js";

const userCard = (id: string, text: string): Card => ({ id, ts: 0, kind: "user", text });
const streamingCard = (id: string, text: string, done = true): Card => ({
  id,
  ts: 0,
  kind: "streaming",
  text,
  done,
});
const toolCard = (): Card => ({
  id: "tool-1",
  ts: 0,
  kind: "tool",
  name: "shell",
  args: {},
  output: "",
  done: true,
  elapsedMs: 0,
});

describe("buildSnapshot", () => {
  it("emits header + text + blank separators per text card", () => {
    const cards = [userCard("u1", "hello\nworld"), streamingCard("s1", "hi back")];
    const snap = buildSnapshot(cards);
    const kinds = snap.map((l) => l.kind);
    expect(kinds).toEqual(["header", "text", "text", "blank", "header", "text"]);
  });

  it("skips non-text cards (tool/diff/etc)", () => {
    const snap = buildSnapshot([userCard("u1", "ask"), toolCard(), streamingCard("s1", "answer")]);
    expect(snap.filter((l) => l.kind === "text").map((l) => l.text)).toEqual(["ask", "answer"]);
  });

  it("preserves blank lines inside a card", () => {
    const snap = buildSnapshot([streamingCard("s1", "para1\n\npara2")]);
    const text = snap.filter((l) => l.kind === "text").map((l) => l.text);
    expect(text).toEqual(["para1", "", "para2"]);
  });

  it("returns empty array for cards with no text content", () => {
    expect(buildSnapshot([])).toEqual([]);
    expect(buildSnapshot([toolCard()])).toEqual([]);
  });
});

describe("yankRange", () => {
  const cards = [userCard("u1", "question"), streamingCard("s1", "line A\nline B\nline C")];
  const snap = buildSnapshot(cards);

  it("excludes header rows from the yanked output", () => {
    const yanked = yankRange(snap, 0, snap.length - 1);
    expect(yanked).toContain("question");
    expect(yanked).toContain("line A");
    expect(yanked).not.toContain("───");
  });

  it("works with reversed indices (cursor above anchor)", () => {
    const a = yankRange(snap, 0, 4);
    const b = yankRange(snap, 4, 0);
    expect(a).toBe(b);
  });

  it("trims surrounding empty lines", () => {
    const yanked = yankRange(snap, 0, snap.length - 1);
    expect(yanked.startsWith("\n")).toBe(false);
    expect(yanked.endsWith("\n")).toBe(false);
  });

  it("yanks just text rows when range spans across cards", () => {
    const lastTextLineOfCard1 = snap.findIndex((l) => l.kind === "text" && l.text === "question");
    const firstTextLineOfCard2 = snap.findIndex((l) => l.kind === "text" && l.text === "line A");
    const yanked = yankRange(snap, lastTextLineOfCard1, firstTextLineOfCard2);
    expect(yanked).toBe("question\n\nline A");
  });
});

describe("isYankable", () => {
  it("rejects header lines", () => {
    const snap = buildSnapshot([userCard("u1", "hi")]);
    expect(isYankable(snap[0])).toBe(false);
    expect(isYankable(snap[1])).toBe(true);
  });

  it("rejects undefined input safely", () => {
    expect(isYankable(undefined)).toBe(false);
  });
});
