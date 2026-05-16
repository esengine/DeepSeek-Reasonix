import { describe, expect, it } from "vitest";
import {
  type SceneSessionItem,
  type SceneSlashMatch,
  type SceneTraceCard,
  buildSetupFrame,
  buildTraceFrame,
  cardsForHeight,
  parseRecentCards,
  parseSessions,
  parseSlashMatches,
  slashWindow,
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

  it("renders the title as a row with brand on the left and model on the right separated by a fill spacer", () => {
    const f = buildEmpty({ model: "deepseek-chat" });
    if (f.root.kind !== "box") throw new Error("expected box root");
    const title = f.root.children[0];
    if (title?.kind !== "box") throw new Error("expected title to be a box");
    expect(title.layout?.direction).toBe("row");
    expect(title.children).toHaveLength(3);
    const [brand, spacer, model] = title.children;
    if (brand?.kind !== "text") throw new Error("expected brand text");
    expect(brand.runs.map((r) => r.text).join("")).toContain("reasonix");
    if (spacer?.kind !== "box") throw new Error("expected spacer box");
    expect(spacer.layout?.width).toBe("fill");
    if (model?.kind !== "text") throw new Error("expected model text");
    expect(model.runs.map((r) => r.text).join("")).toBe("deepseek-chat");
  });

  it("omits the model node from the title row when no model is given", () => {
    const f = buildEmpty();
    if (f.root.kind !== "box") throw new Error("expected box root");
    const title = f.root.children[0];
    if (title?.kind !== "box") throw new Error("expected title to be a box");
    expect(title.children).toHaveLength(2);
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

  it("keeps the status row as a single text node when no wallet balance is given", () => {
    const f = buildTraceFrame({ cardCount: 1, busy: false, cards: [] }, 80, 24);
    if (f.root.kind !== "box") return;
    const status = f.root.children[2];
    expect(status?.kind).toBe("text");
  });

  it("renders a wallet segment on the right when balance + currency are given", () => {
    const f = buildTraceFrame(
      { cardCount: 1, busy: false, cards: [], walletBalance: 184.2, walletCurrency: "CNY" },
      80,
      24,
    );
    if (f.root.kind !== "box") return;
    const status = f.root.children[2];
    if (status?.kind !== "box") throw new Error("expected row box with wallet");
    expect(status.layout?.direction).toBe("row");
    expect(status.children).toHaveLength(3);
    const [left, spacer, right] = status.children;
    if (left?.kind !== "text") throw new Error("expected left text");
    expect(left.runs.map((r) => r.text).join("")).toContain("1 cards");
    if (spacer?.kind !== "box") throw new Error("expected spacer box");
    expect(spacer.layout?.width).toBe("fill");
    if (right?.kind !== "text") throw new Error("expected right text");
    const rightFlat = right.runs.map((r) => r.text).join("");
    expect(rightFlat).toContain("wallet");
    expect(rightFlat).toContain("¥184.20");
  });

  it("formats USD with $ and falls back to a code prefix for unknown currencies", () => {
    const usd = buildTraceFrame(
      { cardCount: 0, busy: false, cards: [], walletBalance: 5, walletCurrency: "USD" },
      80,
      24,
    );
    if (usd.root.kind !== "box") return;
    const usdStatus = usd.root.children[2];
    if (usdStatus?.kind !== "box") return;
    const usdRight = usdStatus.children[2];
    if (usdRight?.kind !== "text") return;
    expect(usdRight.runs.map((r) => r.text).join("")).toContain("$5.00");

    const other = buildTraceFrame(
      { cardCount: 0, busy: false, cards: [], walletBalance: 5, walletCurrency: "AUD" },
      80,
      24,
    );
    if (other.root.kind !== "box") return;
    const otherStatus = other.root.children[2];
    if (otherStatus?.kind !== "box") return;
    const otherRight = otherStatus.children[2];
    if (otherRight?.kind !== "text") return;
    expect(otherRight.runs.map((r) => r.text).join("")).toContain("AUD 5.00");
  });

  it("hides the wallet segment when balance is missing even with a currency present", () => {
    const f = buildTraceFrame(
      { cardCount: 0, busy: false, cards: [], walletCurrency: "CNY" },
      80,
      24,
    );
    if (f.root.kind !== "box") return;
    const status = f.root.children[2];
    expect(status?.kind).toBe("text");
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

  function makeMatches(n: number): SceneSlashMatch[] {
    return Array.from({ length: n }, (_, i) => ({
      cmd: `/cmd${i}`,
      summary: `summary ${i}`,
    }));
  }

  function buildWithSlash(matches: SceneSlashMatch[], selected: number) {
    return buildTraceFrame(
      {
        cardCount: 0,
        busy: false,
        cards: [],
        composerText: "/",
        slashMatches: matches,
        slashSelectedIndex: selected,
      },
      80,
      24,
    );
  }

  it("omits slash rows when slashMatches is empty / undefined", () => {
    const f = buildEmpty();
    if (f.root.kind !== "box") return;
    expect(f.root.children).toHaveLength(4);
  });

  it("appends one slash row per match below the composer with a ▸ on the selected one", () => {
    const f = buildWithSlash(makeMatches(3), 1);
    if (f.root.kind !== "box") return;
    expect(f.root.children).toHaveLength(4 + 3);
    const rows = f.root.children.slice(4);
    const rendered = rows.map((r) => (r.kind === "text" ? r.runs.map((x) => x.text).join("") : ""));
    expect(rendered[0]?.startsWith("  /cmd0")).toBe(true);
    expect(rendered[1]?.startsWith("▸ /cmd1")).toBe(true);
    expect(rendered[2]?.startsWith("  /cmd2")).toBe(true);
  });

  it("includes argsHint after the cmd when given", () => {
    const f = buildWithSlash([{ cmd: "/model", summary: "switch model", argsHint: "<name>" }], 0);
    if (f.root.kind !== "box") return;
    const row = f.root.children[4];
    if (row?.kind !== "text") return;
    const flat = row.runs.map((r) => r.text).join("");
    expect(flat).toContain("/model");
    expect(flat).toContain("<name>");
    expect(flat).toContain("switch model");
  });

  it("windows long match lists and shows an overflow row with the hidden count", () => {
    const f = buildWithSlash(makeMatches(20), 0);
    if (f.root.kind !== "box") return;
    expect(f.root.children).toHaveLength(4 + 6 + 1);
    const overflow = f.root.children.at(-1);
    if (overflow?.kind !== "text") return;
    expect(overflow.runs.map((r) => r.text).join("")).toContain("…14 more");
  });

  it("keeps the selected match inside the window", () => {
    const w = slashWindow(makeMatches(20), 15);
    expect(w.startIndex).toBe(12);
    expect(w.matches.map((m) => m.cmd)).toEqual([
      "/cmd12",
      "/cmd13",
      "/cmd14",
      "/cmd15",
      "/cmd16",
      "/cmd17",
    ]);
  });

  it("anchors the window at the end when the selection is near the tail", () => {
    const w = slashWindow(makeMatches(20), 19);
    expect(w.startIndex).toBe(14);
    expect(w.matches.map((m) => m.cmd).at(-1)).toBe("/cmd19");
  });

  it("anchors the window at the start when the selection is at index 0", () => {
    const w = slashWindow(makeMatches(20), 0);
    expect(w.startIndex).toBe(0);
    expect(w.matches.map((m) => m.cmd)[0]).toBe("/cmd0");
  });

  it("clamps an out-of-range slashSelectedIndex", () => {
    const f = buildWithSlash(makeMatches(3), 99);
    if (f.root.kind !== "box") return;
    const rows = f.root.children.slice(4);
    const flat = rows.map((r) => (r.kind === "text" ? r.runs.map((x) => x.text).join("") : ""));
    expect(flat[2]?.startsWith("▸ /cmd2")).toBe(true);
  });
});

describe("buildTraceFrame approval modal", () => {
  function buildWithApproval(kind: string | undefined, prompt: string | undefined) {
    return buildTraceFrame(
      {
        cardCount: 0,
        busy: false,
        cards: [],
        composerText: "typing…",
        approvalKind: kind,
        approvalPrompt: prompt,
      },
      80,
      24,
    );
  }

  it("replaces the composer row with an approval row when approvalPrompt is set", () => {
    const f = buildWithApproval("shell", "rm -rf /tmp/x");
    if (f.root.kind !== "box") return;
    expect(f.root.children).toHaveLength(4);
    const row = f.root.children[3];
    if (row?.kind !== "text") return;
    const flat = row.runs.map((r) => r.text).join("");
    expect(flat).toContain("❓");
    expect(flat).toContain("[shell]");
    expect(flat).toContain("rm -rf /tmp/x");
    expect(flat).toContain("[y/n]");
    expect(flat).not.toContain("❯");
    expect(flat).not.toContain("typing…");
  });

  it("clips an overlong approval prompt to 60 chars with an ellipsis", () => {
    const long = "x".repeat(120);
    const f = buildWithApproval("shell", long);
    if (f.root.kind !== "box") return;
    const row = f.root.children[3];
    if (row?.kind !== "text") return;
    const promptRun = row.runs.find((r) => r.text.includes("x"));
    expect(promptRun?.text).toHaveLength(60);
    expect(promptRun?.text.endsWith("…")).toBe(true);
  });

  it("omits the kind tag when approvalKind is undefined", () => {
    const f = buildWithApproval(undefined, "go ahead?");
    if (f.root.kind !== "box") return;
    const row = f.root.children[3];
    if (row?.kind !== "text") return;
    const flat = row.runs.map((r) => r.text).join("");
    expect(flat).not.toContain("[]");
    expect(flat).toContain("go ahead?");
  });

  it("hides the slash overlay while an approval is active", () => {
    const f = buildTraceFrame(
      {
        cardCount: 0,
        busy: false,
        cards: [],
        composerText: "/",
        slashMatches: [{ cmd: "/help", summary: "show help" }],
        slashSelectedIndex: 0,
        approvalKind: "shell",
        approvalPrompt: "rm -rf /tmp/x",
      },
      80,
      24,
    );
    if (f.root.kind !== "box") return;
    expect(f.root.children).toHaveLength(4);
  });
});

describe("buildTraceFrame sessions picker", () => {
  function makeSessions(n: number): SceneSessionItem[] {
    return Array.from({ length: n }, (_, i) => ({
      title: `session-${i}`,
      meta: `main · ${i + 1} turns`,
    }));
  }

  function buildWithSessions(sessions: SceneSessionItem[], focus: number) {
    return buildTraceFrame(
      {
        cardCount: 0,
        busy: false,
        cards: [],
        composerText: "typing…",
        sessions,
        sessionsFocusedIndex: focus,
      },
      80,
      24,
    );
  }

  it("replaces composer with a header + one row per session + a hint footer", () => {
    const f = buildWithSessions(makeSessions(3), 0);
    if (f.root.kind !== "box") return;
    expect(f.root.children).toHaveLength(3 + 1 + 3 + 1);
    const header = f.root.children[3];
    if (header?.kind !== "text") return;
    const headerFlat = header.runs.map((r) => r.text).join("");
    expect(headerFlat).toContain("sessions");
    expect(headerFlat).toContain("(3 saved)");
    const row0 = f.root.children[4];
    if (row0?.kind !== "text") return;
    expect(row0.runs[0]?.text).toBe("▸ ");
    expect(row0.runs[1]?.text).toBe("session-0");
    const hint = f.root.children.at(-1);
    if (hint?.kind !== "text") return;
    const hintFlat = hint.runs.map((r) => r.text).join("");
    expect(hintFlat).toContain("navigate");
    expect(hintFlat).toContain("open");
  });

  it("windows a long session list at MAX_SESSION_ROWS (8) with an overflow row", () => {
    const f = buildWithSessions(makeSessions(20), 15);
    if (f.root.kind !== "box") return;
    expect(f.root.children).toHaveLength(3 + 1 + 8 + 1 + 1);
    const overflow = f.root.children.at(-2);
    if (overflow?.kind !== "text") return;
    expect(overflow.runs.map((r) => r.text).join("")).toContain("…12 more");
  });

  it("suppresses both the composer and the slash overlay while a sessions picker is active", () => {
    const f = buildTraceFrame(
      {
        cardCount: 0,
        busy: false,
        cards: [],
        composerText: "/",
        slashMatches: [{ cmd: "/help", summary: "show help" }],
        slashSelectedIndex: 0,
        sessions: makeSessions(2),
        sessionsFocusedIndex: 0,
      },
      80,
      24,
    );
    if (f.root.kind !== "box") return;
    const flat = f.root.children
      .map((c) => (c.kind === "text" ? c.runs.map((r) => r.text).join("") : ""))
      .join(" | ");
    expect(flat).not.toContain("/help");
    expect(flat).not.toContain("❯");
  });

  it("renders meta after the title when given", () => {
    const f = buildWithSessions([{ title: "feat-foo", meta: "feat · 12 turns" }], 0);
    if (f.root.kind !== "box") return;
    const row = f.root.children[4];
    if (row?.kind !== "text") return;
    const flat = row.runs.map((r) => r.text).join("");
    expect(flat).toContain("feat-foo");
    expect(flat).toContain("feat · 12 turns");
  });
});

describe("parseSessions", () => {
  it("returns [] for undefined / malformed input", () => {
    expect(parseSessions(undefined)).toEqual([]);
    expect(parseSessions("")).toEqual([]);
    expect(parseSessions("oops")).toEqual([]);
    expect(parseSessions('{"not":"array"}')).toEqual([]);
  });

  it("decodes a JSON array of session items and preserves optional meta", () => {
    const json = JSON.stringify([{ title: "a", meta: "main · 1 turns" }, { title: "b" }]);
    expect(parseSessions(json)).toEqual([{ title: "a", meta: "main · 1 turns" }, { title: "b" }]);
  });

  it("skips entries missing a title", () => {
    const json = JSON.stringify([{ title: "ok" }, { meta: "no-title" }, null, 42]);
    expect(parseSessions(json)).toEqual([{ title: "ok" }]);
  });
});

describe("buildSetupFrame", () => {
  function flatten(frame: ReturnType<typeof buildSetupFrame>): string[] {
    if (frame.root.kind !== "box") throw new Error("expected box");
    return frame.root.children.map((c) =>
      c.kind === "text" ? c.runs.map((r) => r.text).join("") : "",
    );
  }

  it("renders welcome + prompt + masked-input placeholder + exit hint when the buffer is empty", () => {
    const f = buildSetupFrame({ bufferLength: 0 }, 80, 24);
    const rows = flatten(f);
    expect(rows[0]).toContain("reasonix");
    expect(rows[0]).toContain("welcome");
    expect(rows[1]).toContain("API key");
    expect(rows[2]).toContain("platform.deepseek.com");
    expect(rows[3]).toContain("(start typing your key)");
    expect(rows[3]).not.toContain("▮");
    expect(rows.at(-1)).toContain("Ctrl+C");
  });

  it("masks the buffer with • dots and appends a ▮ cursor when the user has typed", () => {
    const f = buildSetupFrame({ bufferLength: 5 }, 80, 24);
    const rows = flatten(f);
    expect(rows[3]).toContain("•••••");
    expect(rows[3]).toContain("▮");
    expect(rows[3]).not.toContain("(start typing");
  });

  it("never leaks the raw buffer content — bufferLength is the only signal in/out", () => {
    const f = buildSetupFrame({ bufferLength: 12 }, 80, 24);
    const rows = flatten(f);
    expect(rows[3]?.match(/•/g)?.length).toBe(12);
  });

  it("inserts an error row above the exit hint when an error is set", () => {
    const f = buildSetupFrame({ bufferLength: 0, error: "key looks malformed" }, 80, 24);
    const rows = flatten(f);
    const errIdx = rows.findIndex((r) => r.includes("key looks malformed"));
    expect(errIdx).toBeGreaterThan(0);
    expect(rows[errIdx]?.startsWith("✗")).toBe(true);
    expect(rows.at(-1)).toContain("Ctrl+C");
  });

  it("omits the error row when error is undefined", () => {
    const f = buildSetupFrame({ bufferLength: 3 }, 80, 24);
    const rows = flatten(f);
    expect(rows.some((r) => r.startsWith("✗"))).toBe(false);
  });
});

describe("parseSlashMatches", () => {
  it("returns [] for undefined / empty / malformed input", () => {
    expect(parseSlashMatches(undefined)).toEqual([]);
    expect(parseSlashMatches("")).toEqual([]);
    expect(parseSlashMatches("not-json")).toEqual([]);
    expect(parseSlashMatches('{"not":"array"}')).toEqual([]);
  });

  it("decodes a JSON array of slash specs and preserves optional argsHint", () => {
    const json = JSON.stringify([
      { cmd: "/help", summary: "show help" },
      { cmd: "/model", summary: "switch model", argsHint: "<name>" },
    ]);
    expect(parseSlashMatches(json)).toEqual([
      { cmd: "/help", summary: "show help" },
      { cmd: "/model", summary: "switch model", argsHint: "<name>" },
    ]);
  });

  it("skips entries missing cmd or summary", () => {
    const json = JSON.stringify([
      { cmd: "/ok", summary: "ok" },
      { cmd: "/no-summary" },
      { summary: "no-cmd" },
      null,
      "string",
    ]);
    expect(parseSlashMatches(json)).toEqual([{ cmd: "/ok", summary: "ok" }]);
  });
});
