import { describe, expect, it } from "vitest";
import {
  type SceneTraceCard,
  buildTraceFrame,
  cardsForHeight,
  parseRecentCards,
  summarizeCard,
} from "../src/cli/ui/hooks/useSceneTrace.js";
import type { Card } from "../src/cli/ui/state/cards.js";

function userCard(text: string): Card {
  return { id: "u1", ts: 0, kind: "user", text };
}

function toolCard(name: string, done: boolean): Card {
  return {
    id: "t1",
    ts: 0,
    kind: "tool",
    name,
    args: {},
    output: "",
    done,
    elapsedMs: 0,
  };
}

function buildEmpty(extra: Partial<{ model: string; busy: boolean; composerText: string }> = {}) {
  return buildTraceFrame({ cardCount: 0, busy: false, cards: [], ...extra }, 80, 24);
}

describe("summarizeCard", () => {
  it("returns the first line for a user card", () => {
    expect(summarizeCard(userCard("hello\nworld"))).toBe("hello");
  });

  it("clips long first lines and appends an ellipsis", () => {
    const s = summarizeCard(userCard("x".repeat(200)));
    expect(s).toHaveLength(70);
    expect(s?.endsWith("…")).toBe(true);
  });

  it("returns the tool name with a running marker when not done", () => {
    expect(summarizeCard(toolCard("bash", false))).toBe("bash …");
  });

  it("returns the tool name plain when done", () => {
    expect(summarizeCard(toolCard("bash", true))).toBe("bash");
  });

  it("returns undefined for no card", () => {
    expect(summarizeCard(undefined)).toBeUndefined();
  });
});

describe("parseRecentCards", () => {
  it("returns [] for undefined / empty / malformed input", () => {
    expect(parseRecentCards(undefined)).toEqual([]);
    expect(parseRecentCards("")).toEqual([]);
    expect(parseRecentCards("not-json")).toEqual([]);
    expect(parseRecentCards('{"not":"array"}')).toEqual([]);
  });

  it("decodes a JSON array of {kind, summary} objects", () => {
    const json = JSON.stringify([
      { kind: "user", summary: "hi" },
      { kind: "streaming", summary: "hello back" },
    ]);
    expect(parseRecentCards(json)).toEqual([
      { kind: "user", summary: "hi" },
      { kind: "streaming", summary: "hello back" },
    ]);
  });

  it("skips items missing kind or summary fields", () => {
    const json = JSON.stringify([
      { kind: "user", summary: "ok" },
      { kind: "tool" }, // missing summary
      { summary: "no kind" }, // missing kind
      "string item",
      null,
      { kind: "warn", summary: "trailer" },
    ]);
    expect(parseRecentCards(json)).toEqual([
      { kind: "user", summary: "ok" },
      { kind: "warn", summary: "trailer" },
    ]);
  });
});

describe("cardsForHeight", () => {
  function makeCards(n: number): SceneTraceCard[] {
    return Array.from({ length: n }, (_, i) => ({ kind: "user", summary: String(i) }));
  }

  it("returns the last (rows - 3) cards by default", () => {
    const cards = makeCards(30);
    const fit = cardsForHeight(cards, 24);
    expect(fit).toHaveLength(21);
    expect(fit[0]?.summary).toBe("9");
    expect(fit.at(-1)?.summary).toBe("29");
  });

  it("caps at the hard ceiling (24) even on tall terminals", () => {
    const cards = makeCards(100);
    const fit = cardsForHeight(cards, 200);
    expect(fit).toHaveLength(24);
  });

  it("returns at least 1 card slot even on absurdly short terminals", () => {
    const cards = makeCards(5);
    expect(cardsForHeight(cards, 0)).toHaveLength(1);
    expect(cardsForHeight(cards, 2)).toHaveLength(1);
  });

  it("returns all cards when there are fewer than the available slots", () => {
    const cards = makeCards(3);
    expect(cardsForHeight(cards, 24)).toHaveLength(3);
  });
});

describe("buildTraceFrame", () => {
  it("returns a column box with title + placeholder + status + composer when no cards", () => {
    const f = buildEmpty();
    expect(f.schemaVersion).toBe(1);
    expect(f.cols).toBe(80);
    expect(f.rows).toBe(24);
    expect(f.root.kind).toBe("box");
    if (f.root.kind !== "box") return;
    expect(f.root.layout?.direction).toBe("column");
    expect(f.root.layout?.paddingX).toBe(1);
    expect(f.root.children).toHaveLength(4);
  });

  it("renders the model name in the title row when given", () => {
    const f = buildEmpty({ model: "deepseek-chat" });
    expect(f.root.kind).toBe("box");
    if (f.root.kind !== "box") return;
    const title = f.root.children[0];
    if (title?.kind !== "text") return;
    const flat = title.runs.map((r) => r.text).join("");
    expect(flat).toContain("reasonix");
    expect(flat).toContain("deepseek-chat");
  });

  it("shows a placeholder card row when no cards exist", () => {
    const f = buildEmpty();
    if (f.root.kind !== "box") return;
    const card = f.root.children[1];
    if (card?.kind !== "text") return;
    expect(card.runs.map((r) => r.text).join("")).toContain("no cards yet");
  });

  it("stacks one row per card between the title and status rows", () => {
    const cards: SceneTraceCard[] = [
      { kind: "user", summary: "hi" },
      { kind: "streaming", summary: "hello back" },
      { kind: "user", summary: "follow up" },
    ];
    const f = buildTraceFrame({ cardCount: 3, busy: false, cards }, 80, 24);
    if (f.root.kind !== "box") return;
    expect(f.root.children).toHaveLength(3 + 3);
    const firstCard = f.root.children[1];
    const lastCard = f.root.children[3];
    if (firstCard?.kind !== "text" || lastCard?.kind !== "text") return;
    expect(firstCard.runs[2]?.text).toBe("hi");
    expect(lastCard.runs[2]?.text).toBe("follow up");
  });

  it("paints the user-card icon cyan and the text bold", () => {
    const f = buildTraceFrame(
      { cardCount: 1, busy: false, cards: [{ kind: "user", summary: "hello" }] },
      80,
      24,
    );
    if (f.root.kind !== "box") return;
    const cardRow = f.root.children[1];
    if (cardRow?.kind !== "text") return;
    expect(cardRow.runs[0]?.style?.color).toBe("cyan");
    expect(cardRow.runs[0]?.text).toBe(">");
    expect(cardRow.runs[2]?.text).toBe("hello");
    expect(cardRow.runs[2]?.style?.bold).toBe(true);
  });

  it("paints status as green when idle and yellow when busy", () => {
    const idle = buildTraceFrame({ cardCount: 3, busy: false, cards: [] }, 80, 24);
    const busy = buildTraceFrame({ cardCount: 3, busy: true, cards: [] }, 80, 24);
    if (idle.root.kind !== "box" || busy.root.kind !== "box") return;
    // children: title, placeholder, status, composer
    const idleStatus = idle.root.children[2];
    const busyStatus = busy.root.children[2];
    if (idleStatus?.kind !== "text" || busyStatus?.kind !== "text") return;
    expect(idleStatus.runs[2]?.style?.color).toBe("green");
    expect(busyStatus.runs[2]?.style?.color).toBe("yellow");
  });

  it("appends activity to the status row when given", () => {
    const f = buildTraceFrame(
      { cardCount: 3, busy: true, cards: [], activity: "awaiting tools" },
      80,
      24,
    );
    if (f.root.kind !== "box") return;
    const status = f.root.children[2];
    if (status?.kind !== "text") return;
    expect(status.runs.map((r) => r.text).join("")).toContain("awaiting tools");
  });

  it("composer row is a dim placeholder when composerText is empty / undefined", () => {
    for (const f of [buildEmpty(), buildEmpty({ composerText: "" })]) {
      if (f.root.kind !== "box") return;
      const composer = f.root.children[3];
      if (composer?.kind !== "text") return;
      expect(composer.runs[0]?.text).toBe("❯ ");
      expect(composer.runs[0]?.style?.color).toBe("cyan");
      expect(composer.runs[1]?.style?.dim).toBe(true);
    }
  });

  it("composer row shows typed text plus a cursor block when composerText is non-empty", () => {
    const f = buildTraceFrame(
      { cardCount: 1, busy: false, cards: [], composerText: "hello" },
      80,
      24,
    );
    if (f.root.kind !== "box") return;
    const composer = f.root.children[3];
    if (composer?.kind !== "text") return;
    expect(composer.runs[0]?.text).toBe("❯ ");
    expect(composer.runs[1]?.text).toBe("hello");
    expect(composer.runs[2]?.text).toBe("▮");
    expect(composer.runs[2]?.style?.color).toBe("cyan");
  });

  function composerRunsAt(cursor: number | undefined): string[] {
    const f = buildTraceFrame(
      { cardCount: 1, busy: false, cards: [], composerText: "hello", composerCursor: cursor },
      80,
      24,
    );
    if (f.root.kind !== "box") throw new Error("expected box");
    const composer = f.root.children[3];
    if (composer?.kind !== "text") throw new Error("expected text");
    return composer.runs.map((r) => r.text);
  }

  it("splits composer text around the cursor block at an interior offset", () => {
    expect(composerRunsAt(2)).toEqual(["❯ ", "he", "▮", "llo"]);
  });

  it("places the cursor at the start when offset is 0", () => {
    expect(composerRunsAt(0)).toEqual(["❯ ", "▮", "hello"]);
  });

  it("places the cursor at the end when offset equals text length", () => {
    expect(composerRunsAt(5)).toEqual(["❯ ", "hello", "▮"]);
  });

  it("clamps an out-of-range cursor to the text bounds", () => {
    expect(composerRunsAt(99)).toEqual(["❯ ", "hello", "▮"]);
    expect(composerRunsAt(-3)).toEqual(["❯ ", "▮", "hello"]);
  });

  it("falls back to end-of-text when composerCursor is undefined", () => {
    expect(composerRunsAt(undefined)).toEqual(["❯ ", "hello", "▮"]);
  });
});
