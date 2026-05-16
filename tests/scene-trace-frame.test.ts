import { describe, expect, it } from "vitest";
import { buildTraceFrame, summarizeCard } from "../src/cli/ui/hooks/useSceneTrace.js";
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

describe("buildTraceFrame", () => {
  it("returns a column box with four children: title, card, status, composer", () => {
    const f = buildTraceFrame({ cardCount: 0, busy: false }, 80, 24);
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
    const f = buildTraceFrame({ cardCount: 0, busy: false, model: "deepseek-chat" }, 80, 24);
    expect(f.root.kind).toBe("box");
    if (f.root.kind !== "box") return;
    const title = f.root.children[0];
    expect(title?.kind).toBe("text");
    if (title?.kind !== "text") return;
    const flat = title.runs.map((r) => r.text).join("");
    expect(flat).toContain("reasonix");
    expect(flat).toContain("deepseek-chat");
  });

  it("shows a placeholder card row when no cards exist", () => {
    const f = buildTraceFrame({ cardCount: 0, busy: false }, 80, 24);
    if (f.root.kind !== "box") return;
    const card = f.root.children[1];
    if (card?.kind !== "text") return;
    const flat = card.runs.map((r) => r.text).join("");
    expect(flat).toContain("no cards yet");
  });

  it("paints the user-card icon cyan and the text bold", () => {
    const f = buildTraceFrame(
      {
        cardCount: 1,
        busy: false,
        lastCardKind: "user",
        lastCardSummary: "hello",
      },
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
    const idle = buildTraceFrame({ cardCount: 3, busy: false }, 80, 24);
    const busy = buildTraceFrame({ cardCount: 3, busy: true }, 80, 24);
    if (idle.root.kind !== "box" || busy.root.kind !== "box") return;
    const idleStatus = idle.root.children[2];
    const busyStatus = busy.root.children[2];
    if (idleStatus?.kind !== "text" || busyStatus?.kind !== "text") return;
    expect(idleStatus.runs[2]?.style?.color).toBe("green");
    expect(busyStatus.runs[2]?.style?.color).toBe("yellow");
  });

  it("appends activity to the status row when given", () => {
    const f = buildTraceFrame({ cardCount: 3, busy: true, activity: "awaiting tools" }, 80, 24);
    if (f.root.kind !== "box") return;
    const status = f.root.children[2];
    if (status?.kind !== "text") return;
    const flat = status.runs.map((r) => r.text).join("");
    expect(flat).toContain("awaiting tools");
  });

  it("composer row is a dim placeholder when composerText is empty / undefined", () => {
    for (const f of [
      buildTraceFrame({ cardCount: 0, busy: false }, 80, 24),
      buildTraceFrame({ cardCount: 0, busy: false, composerText: "" }, 80, 24),
    ]) {
      if (f.root.kind !== "box") return;
      const composer = f.root.children[3];
      if (composer?.kind !== "text") return;
      expect(composer.runs[0]?.text).toBe("❯ ");
      expect(composer.runs[0]?.style?.color).toBe("cyan");
      expect(composer.runs[1]?.style?.dim).toBe(true);
    }
  });

  it("composer row shows typed text plus a cursor block when composerText is non-empty", () => {
    const f = buildTraceFrame({ cardCount: 1, busy: false, composerText: "hello" }, 80, 24);
    if (f.root.kind !== "box") return;
    const composer = f.root.children[3];
    if (composer?.kind !== "text") return;
    expect(composer.runs[0]?.text).toBe("❯ ");
    expect(composer.runs[1]?.text).toBe("hello");
    expect(composer.runs[2]?.text).toBe("▮");
    expect(composer.runs[2]?.style?.color).toBe("cyan");
  });
});
