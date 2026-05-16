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
  toSceneCard,
} from "../src/cli/ui/hooks/useSceneTrace.js";
import type { SceneNode } from "../src/cli/ui/scene/types.js";
import type { Card } from "../src/cli/ui/state/cards.js";

function rootChildren(f: ReturnType<typeof buildTraceFrame>): SceneNode[] {
  if (f.root.kind !== "box") throw new Error("expected root box");
  return f.root.children;
}

function scrollOf(f: ReturnType<typeof buildTraceFrame>): SceneNode[] {
  const scroll = rootChildren(f)[0];
  if (scroll?.kind !== "box") throw new Error("expected scroll box");
  return scroll.children;
}

function dockOf(f: ReturnType<typeof buildTraceFrame>): SceneNode[] {
  const dock = rootChildren(f)[1];
  if (dock?.kind !== "box") throw new Error("expected dock box");
  return dock.children;
}

function flat(node: SceneNode | undefined): string {
  if (!node || node.kind !== "text") return "";
  return node.runs.map((r) => r.text).join("");
}

function flatRows(nodes: SceneNode[]): string {
  return nodes
    .map((c) => {
      if (c.kind === "text") return flat(c);
      if (c.kind === "box") return c.children.map((cc) => flat(cc)).join("");
      return "";
    })
    .join("\n");
}

function findText(nodes: SceneNode[], predicate: (s: string) => boolean): SceneNode | undefined {
  return nodes.find((c) => c.kind === "text" && predicate(flat(c)));
}

function composerRowOf(f: ReturnType<typeof buildTraceFrame>): SceneNode {
  const dock = dockOf(f);
  const row = dock.find((c) => {
    if (c.kind !== "box") return false;
    const first = c.children[0];
    if (first?.kind !== "text") return false;
    return first.runs.some((r) => r.text === "❯ ");
  });
  if (!row) throw new Error("composer row not found");
  return row;
}

function statusRowOf(f: ReturnType<typeof buildTraceFrame>): SceneNode {
  const dock = dockOf(f);
  const last = dock.at(-1);
  if (!last) throw new Error("status row not found");
  return last;
}

function metaRowOf(f: ReturnType<typeof buildTraceFrame>): SceneNode {
  const dock = dockOf(f);
  const idx = dock.length - 2;
  const row = dock[idx];
  if (!row) throw new Error("meta row not found");
  return row;
}

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
  return buildTraceFrame({ cardCount: 0, busy: false, cards: [], ...extra }, 142, 38);
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
      { kind: "tool" },
      { summary: "no kind" },
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

  it("returns the last (rows - 4) cards by default", () => {
    const cards = makeCards(30);
    const fit = cardsForHeight(cards, 24);
    expect(fit).toHaveLength(20);
    expect(fit[0]?.summary).toBe("10");
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

describe("buildTraceFrame — v1 single-column layout", () => {
  it("has scroll + dock at the root, with bg fill on the outer column", () => {
    const f = buildEmpty();
    if (f.root.kind !== "box") return;
    expect(f.root.layout?.direction).toBe("column");
    expect(f.root.layout?.background).toBeDefined();
    expect(f.root.children).toHaveLength(2);
    const [scroll, dock] = f.root.children;
    if (scroll?.kind !== "box") throw new Error("expected scroll box");
    expect(scroll.layout?.height).toBe("fill");
    if (dock?.kind !== "box") throw new Error("expected dock box");
    expect(dock.layout?.direction).toBe("column");
  });

  it("renders a boot block in the scroll area when there are no cards", () => {
    const f = buildEmpty({ model: "deepseek-chat" });
    const flatScroll = flatRows(scrollOf(f));
    expect(flatScroll).toContain("██████╗");
    expect(flatScroll).toContain("DeepSeek code agent");
    expect(flatScroll).toContain("model");
    expect(flatScroll).toContain("deepseek-chat");
    expect(flatScroll).toContain("tools");
  });

  it("renders one card row per card in the scroll area", () => {
    const cards: SceneTraceCard[] = [
      { kind: "user", summary: "hi" },
      { kind: "streaming", summary: "hello back" },
      { kind: "user", summary: "follow up" },
    ];
    const f = buildTraceFrame({ cardCount: 3, busy: false, cards }, 142, 38);
    const scroll = scrollOf(f);
    expect(scroll).toHaveLength(3);
    const flatScroll = flatRows(scroll);
    expect(flatScroll).toContain("YOU");
    expect(flatScroll).toContain("reasonix");
    expect(flatScroll).toContain("hi");
    expect(flatScroll).toContain("hello back");
  });

  it("paints YOU cards with the design accent (ds hex) and bold", () => {
    const f = buildTraceFrame(
      { cardCount: 1, busy: false, cards: [{ kind: "user", summary: "hello" }] },
      142,
      38,
    );
    const row = scrollOf(f)[0];
    if (row?.kind !== "text") return;
    const glyph = row.runs[0];
    expect(glyph?.style?.bold).toBe(true);
    const color = glyph?.style?.color;
    expect(typeof color === "object" && color && "hex" in color).toBe(true);
  });

  it("dock has composer + meta + status when no overlay is active", () => {
    const f = buildEmpty();
    const dock = dockOf(f);
    expect(dock).toHaveLength(3);
  });

  it("composer row uses ❯ prefix and a bg-2 background", () => {
    const f = buildEmpty();
    const row = composerRowOf(f);
    if (row.kind !== "box") return;
    expect(row.layout?.background).toBeDefined();
    expect(row.layout?.height).toBe(1);
    const inner = row.children[0];
    if (inner?.kind !== "text") return;
    expect(inner.runs.some((r) => r.text === "❯ ")).toBe(true);
  });

  it("composer shows the typed text with a ▮ cursor at the offset", () => {
    const f = buildTraceFrame(
      { cardCount: 0, busy: false, cards: [], composerText: "hello", composerCursor: 2 },
      142,
      38,
    );
    const row = composerRowOf(f);
    if (row.kind !== "box") return;
    const inner = row.children[0];
    if (inner?.kind !== "text") return;
    const flatRow = inner.runs.map((r) => r.text);
    expect(flatRow).toContain("he");
    expect(flatRow).toContain("llo");
    expect(flatRow).toContain("▮");
  });

  it("meta row carries the design's kbd shortcut hints", () => {
    const f = buildEmpty();
    const row = metaRowOf(f);
    if (row.kind !== "box") return;
    const allText = row.children
      .map((c) => (c.kind === "text" ? c.runs.map((r) => r.text).join("") : ""))
      .join(" ");
    expect(allText).toContain("send");
    expect(allText).toContain("newline");
    expect(allText).toContain("cmd");
    expect(allText).toContain("file");
    expect(allText).toContain("shell");
    expect(allText).toContain("esc");
    expect(allText).toContain("cancel");
    expect(allText).toContain("history");
  });

  it("status bar renders ● reasonix on the left and wallet/cwd on the right", () => {
    const f = buildTraceFrame(
      {
        cardCount: 0,
        busy: false,
        cards: [],
        model: "deepseek-chat",
        walletBalance: 184.2,
        walletCurrency: "CNY",
        cwd: "/workspace/reasonix-core",
      },
      142,
      38,
    );
    const row = statusRowOf(f);
    if (row.kind !== "box") return;
    expect(row.layout?.background).toBeDefined();
    const allText = row.children
      .map((c) => (c.kind === "text" ? c.runs.map((r) => r.text).join("") : ""))
      .join(" | ");
    expect(allText).toContain("reasonix");
    expect(allText).toContain("deepseek-chat");
    expect(allText).toContain("¥184.20");
    expect(allText).toContain("reasonix-core");
  });

  it("status bar paints busy/idle in warn/ok hex colors and surfaces editMode", () => {
    const busy = buildTraceFrame(
      { cardCount: 0, busy: true, cards: [], editMode: "yolo" },
      142,
      38,
    );
    const row = statusRowOf(busy);
    if (row.kind !== "box") return;
    const text = row.children
      .map((c) => (c.kind === "text" ? c.runs.map((r) => r.text).join("") : ""))
      .join(" | ");
    expect(text).toContain("busy");
    expect(text).toContain("yolo");
  });

  it("truncates a long cwd to keep the status row from blowing out", () => {
    const long = `/some/very/long/path/${"x".repeat(60)}/end`;
    const f = buildTraceFrame({ cardCount: 0, busy: false, cards: [], cwd: long }, 142, 38);
    const row = statusRowOf(f);
    if (row.kind !== "box") return;
    const segments = row.children
      .map((c) => (c.kind === "text" ? c.runs.map((r) => r.text).join("") : ""))
      .filter((s) => s.includes("/end"));
    expect(segments.length).toBeGreaterThan(0);
    const cwdSegment = segments[0] ?? "";
    expect(cwdSegment.startsWith("cwd")).toBe(true);
    expect(cwdSegment).toContain("…");
    expect(cwdSegment.length).toBeLessThan(long.length);
  });

  it("default-empty cwd: cwd segment is omitted", () => {
    const f = buildEmpty();
    const row = statusRowOf(f);
    if (row.kind !== "box") return;
    const flatRow = row.children
      .map((c) => (c.kind === "text" ? c.runs.map((r) => r.text).join("") : ""))
      .join(" | ");
    expect(flatRow).not.toContain("cwd ");
  });
});

describe("toSceneCard — tool card enrichment", () => {
  it("returns a plain { kind, summary } for non-tool cards", () => {
    expect(toSceneCard({ id: "u1", ts: 0, kind: "user", text: "hi" })).toEqual({
      kind: "user",
      summary: "hi",
    });
  });

  it("includes args + status + elapsed + id for a completed tool card", () => {
    const card = toSceneCard({
      id: "tool-abc123def4",
      ts: 0,
      kind: "tool",
      name: "Read",
      args: { path: "src/parser.ts" },
      output: "",
      done: true,
      exitCode: 0,
      elapsedMs: 120,
    });
    expect(card.kind).toBe("tool");
    expect(card.summary).toBe("Read");
    expect(card.args).toBe("src/parser.ts");
    expect(card.status).toBe("ok");
    expect(card.elapsed).toBe("120ms");
    expect(card.id).toBe("#def4");
  });

  it("marks running tool cards with status=running and omits elapsed", () => {
    const card = toSceneCard({
      id: "t1",
      ts: 0,
      kind: "tool",
      name: "Bash",
      args: { command: "pnpm test" },
      output: "",
      done: false,
      elapsedMs: 0,
    });
    expect(card.status).toBe("running");
    expect(card.args).toBe("pnpm test");
    expect(card.elapsed).toBeUndefined();
  });

  it("marks rejected / non-zero exit tool cards as status=err", () => {
    const failed = toSceneCard({
      id: "t1",
      ts: 0,
      kind: "tool",
      name: "Bash",
      args: { command: "false" },
      output: "",
      done: true,
      exitCode: 1,
      elapsedMs: 10,
    });
    expect(failed.status).toBe("err");

    const rejected = toSceneCard({
      id: "t1",
      ts: 0,
      kind: "tool",
      name: "Bash",
      args: {},
      output: "",
      done: true,
      elapsedMs: 5,
      rejected: true,
    });
    expect(rejected.status).toBe("err");
  });

  it("formats elapsed >= 1000ms in seconds with 2 decimals", () => {
    const c = toSceneCard({
      id: "t",
      ts: 0,
      kind: "tool",
      name: "x",
      args: {},
      output: "",
      done: true,
      elapsedMs: 2123,
    });
    expect(c.elapsed).toBe("2.12s");
  });

  it("extracts the primary arg by common key preference (path / file / pattern)", () => {
    const withPath = toSceneCard({
      id: "t",
      ts: 0,
      kind: "tool",
      name: "x",
      args: { foo: "bar", path: "src/x.ts" },
      output: "",
      done: true,
      elapsedMs: 0,
    });
    expect(withPath.args).toBe("src/x.ts");
    const withPattern = toSceneCard({
      id: "t",
      ts: 0,
      kind: "tool",
      name: "x",
      args: { pattern: "foo", in: "src/" },
      output: "",
      done: true,
      elapsedMs: 0,
    });
    expect(withPattern.args).toBe("foo");
  });
});

describe("buildTraceFrame — tool cards render in rich format", () => {
  it("renders a tool card as ▸ name (args) ✓ elapsed #id", () => {
    const cards: SceneTraceCard[] = [
      {
        kind: "tool",
        summary: "Read",
        args: "src/parser.ts",
        status: "ok",
        elapsed: "120ms",
        id: "#a4f1",
      },
    ];
    const f = buildTraceFrame({ cardCount: 1, busy: false, cards }, 142, 38);
    const row = scrollOf(f)[0];
    if (row?.kind !== "text") throw new Error("expected text row");
    const flatRow = row.runs.map((r) => r.text).join("");
    expect(flatRow.trimStart().startsWith("▸ Read")).toBe(true);
    expect(flatRow).toContain("(src/parser.ts)");
    expect(flatRow).toContain("✓");
    expect(flatRow).toContain("120ms");
    expect(flatRow).toContain("#a4f1");
  });

  it("renders a failed tool card with ✗", () => {
    const cards: SceneTraceCard[] = [
      { kind: "tool", summary: "Bash", args: "false", status: "err", elapsed: "10ms" },
    ];
    const f = buildTraceFrame({ cardCount: 1, busy: false, cards }, 142, 38);
    const row = scrollOf(f)[0];
    if (row?.kind !== "text") throw new Error("expected text row");
    expect(row.runs.map((r) => r.text).join("")).toContain("✗");
  });

  it("renders a running tool card with … instead of a check mark", () => {
    const cards: SceneTraceCard[] = [
      { kind: "tool", summary: "Bash", args: "pnpm test", status: "running" },
    ];
    const f = buildTraceFrame({ cardCount: 1, busy: false, cards }, 142, 38);
    const row = scrollOf(f)[0];
    if (row?.kind !== "text") throw new Error("expected text row");
    expect(row.runs.map((r) => r.text).join("")).toContain("…");
  });
});

describe("buildTraceFrame — composer overlays", () => {
  it("approval prompt replaces the composer row with a y/n stub", () => {
    const f = buildTraceFrame(
      {
        cardCount: 0,
        busy: false,
        cards: [],
        composerText: "typing…",
        approvalKind: "shell",
        approvalPrompt: "rm -rf /tmp/x",
      },
      142,
      38,
    );
    const dock = dockOf(f);
    const composerLike = dock.find((c) => {
      if (c.kind !== "box") return false;
      const inner = c.children[0];
      return inner?.kind === "text" && inner.runs.some((r) => r.text === " ❓ ");
    });
    expect(composerLike).toBeDefined();
    const allText = dock
      .map((c) => {
        if (c.kind === "box") {
          return c.children
            .map((cc) => (cc.kind === "text" ? cc.runs.map((r) => r.text).join("") : ""))
            .join("");
        }
        return c.kind === "text" ? c.runs.map((r) => r.text).join("") : "";
      })
      .join(" | ");
    expect(allText).toContain("[shell]");
    expect(allText).toContain("rm -rf /tmp/x");
    expect(allText).toContain("[y/n]");
    expect(allText).not.toContain("typing…");
  });

  it("slash overlay renders inline above the composer row", () => {
    const matches: SceneSlashMatch[] = [
      { cmd: "/help", summary: "show help" },
      { cmd: "/model", summary: "switch model", argsHint: "<name>" },
    ];
    const f = buildTraceFrame(
      { cardCount: 0, busy: false, cards: [], slashMatches: matches, slashSelectedIndex: 1 },
      142,
      38,
    );
    const dock = dockOf(f);
    const flatDock = flatRows(dock);
    expect(flatDock).toContain("/help");
    expect(flatDock).toContain("/model");
    expect(flatDock).toContain("<name>");
    expect(flatDock).toContain("switch model");
    const selectedRow = dock.find((c) => c.kind === "text" && flat(c).includes("▸"));
    expect(selectedRow).toBeDefined();
  });

  it("sessions picker takes over the dock with a header + rows + hint", () => {
    const sessions: SceneSessionItem[] = [
      { title: "feat-foo", meta: "main · 12 turns" },
      { title: "spike-bar", meta: "release/4.5 · 3 turns" },
    ];
    const f = buildTraceFrame(
      { cardCount: 0, busy: false, cards: [], sessions, sessionsFocusedIndex: 0 },
      142,
      38,
    );
    const dock = dockOf(f);
    const flatDock = flatRows(dock);
    expect(flatDock).toContain("sessions");
    expect(flatDock).toContain("2 saved");
    expect(flatDock).toContain("feat-foo");
    expect(flatDock).toContain("spike-bar");
    expect(flatDock).toContain("navigate");
    expect(flatDock).toContain("open");
  });

  it("clamps an out-of-range slashSelectedIndex onto the last match", () => {
    const matches: SceneSlashMatch[] = [
      { cmd: "/a", summary: "" },
      { cmd: "/b", summary: "" },
      { cmd: "/c", summary: "" },
    ];
    const f = buildTraceFrame(
      { cardCount: 0, busy: false, cards: [], slashMatches: matches, slashSelectedIndex: 99 },
      142,
      38,
    );
    const dock = dockOf(f);
    const selected = dock.find((c) => c.kind === "text" && flat(c).includes("▸"));
    expect(selected).toBeDefined();
    if (selected?.kind === "text") {
      expect(selected.runs.map((r) => r.text).join("")).toContain("/c");
    }
  });
});

describe("slashWindow", () => {
  function makeMatches(n: number): SceneSlashMatch[] {
    return Array.from({ length: n }, (_, i) => ({ cmd: `/cmd${i}`, summary: `s${i}` }));
  }

  it("keeps the selected match centered inside the window", () => {
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

  it("anchors at the end when the selection is near the tail", () => {
    const w = slashWindow(makeMatches(20), 19);
    expect(w.startIndex).toBe(14);
    expect(w.matches.map((m) => m.cmd).at(-1)).toBe("/cmd19");
  });

  it("anchors at the start when the selection is at index 0", () => {
    const w = slashWindow(makeMatches(20), 0);
    expect(w.startIndex).toBe(0);
  });

  it("returns the original list when there are fewer items than the window", () => {
    const w = slashWindow(makeMatches(3), 0);
    expect(w.startIndex).toBe(0);
    expect(w.matches).toHaveLength(3);
  });
});

describe("parseSlashMatches / parseSessions edge cases", () => {
  it("parseSlashMatches drops malformed entries", () => {
    const json = JSON.stringify([
      { cmd: "/ok", summary: "ok" },
      { cmd: "/no-summary" },
      { summary: "no-cmd" },
      null,
      "x",
      { cmd: "/with-args", summary: "x", argsHint: "<a>" },
    ]);
    expect(parseSlashMatches(json)).toEqual([
      { cmd: "/ok", summary: "ok" },
      { cmd: "/with-args", summary: "x", argsHint: "<a>" },
    ]);
  });

  it("parseSessions accepts entries missing the optional meta", () => {
    const json = JSON.stringify([{ title: "a" }, { title: "b", meta: "main" }]);
    expect(parseSessions(json)).toEqual([{ title: "a" }, { title: "b", meta: "main" }]);
  });

  it("parseSessions returns [] on garbage input", () => {
    expect(parseSessions(undefined)).toEqual([]);
    expect(parseSessions("oops")).toEqual([]);
    expect(parseSessions('{"not":"array"}')).toEqual([]);
  });
});

describe("buildSetupFrame", () => {
  it("renders welcome + masked input + exit hint when buffer is empty", () => {
    const f = buildSetupFrame({ bufferLength: 0 }, 80, 24);
    if (f.root.kind !== "box") throw new Error("expected box");
    const flatRoot = flatRows(f.root.children);
    expect(flatRoot).toContain("REASONIX");
    expect(flatRoot).toContain("API key");
    expect(flatRoot).toContain("(start typing your key)");
    expect(flatRoot).toContain("Ctrl+C");
  });

  it("renders one • per typed char and a ▮ cursor", () => {
    const f = buildSetupFrame({ bufferLength: 5 }, 80, 24);
    if (f.root.kind !== "box") throw new Error("expected box");
    const flatRoot = flatRows(f.root.children);
    expect(flatRoot).toContain("•••••");
    expect(flatRoot).toContain("▮");
  });

  it("renders the error row in err color when set", () => {
    const f = buildSetupFrame({ bufferLength: 0, error: "key malformed" }, 80, 24);
    if (f.root.kind !== "box") throw new Error("expected box");
    const errRow = findText(f.root.children, (s) => s.includes("✗"));
    expect(errRow).toBeDefined();
    if (errRow?.kind === "text") {
      expect(flat(errRow)).toContain("key malformed");
    }
  });
});
