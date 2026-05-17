import { describe, expect, it } from "vitest";
import type { ToolCard, UserCard } from "../src/cli/ui/state/cards.js";
import type { AgentEvent } from "../src/cli/ui/state/events.js";
import { reduce } from "../src/cli/ui/state/reducer.js";
import { type AgentState, type SessionInfo, initialState } from "../src/cli/ui/state/state.js";

const session: SessionInfo = {
  id: "test-session",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

function run(events: AgentEvent[], from: AgentState = initialState(session)): AgentState {
  return events.reduce(reduce, from);
}

/** Larger than the elision MIN_ELIDE_OUTPUT_LENGTH (4096) so the helper considers it. */
const BIG_OUTPUT = "x".repeat(8000);
const RECENT_CARDS_WINDOW = 200;

function buildBigToolEvents(count: number, idPrefix = "t"): AgentEvent[] {
  const out: AgentEvent[] = [];
  for (let i = 0; i < count; i++) {
    out.push({ type: "tool.start", id: `${idPrefix}${i}`, name: "read_file", args: { i } });
    out.push({
      type: "tool.end",
      id: `${idPrefix}${i}`,
      output: `${BIG_OUTPUT}::${i}`,
      elapsedMs: 1,
    });
  }
  return out;
}

describe("reducer card-output elision (issue #1031 memory mitigation)", () => {
  it("leaves all tool outputs intact below the recent window", () => {
    const s = run(buildBigToolEvents(50));
    for (const c of s.cards) {
      if (c.kind === "tool") {
        expect((c as ToolCard).output.startsWith("[elided")).toBe(false);
        expect((c as ToolCard).output.length).toBeGreaterThan(7000);
      }
    }
  });

  it("stubs old tool outputs once the window is exceeded", () => {
    const total = RECENT_CARDS_WINDOW + 50;
    const s = run(buildBigToolEvents(total));
    expect(s.cards).toHaveLength(total);
    // Cards beyond the window from the end should be stubbed.
    const cutoff = s.cards.length - RECENT_CARDS_WINDOW;
    for (let i = 0; i < cutoff; i++) {
      const c = s.cards[i]!;
      expect(c.kind).toBe("tool");
      const out = (c as ToolCard).output;
      expect(out.startsWith("[elided")).toBe(true);
      expect(out.length).toBeLessThan(300);
      expect(out).toMatch(/chars dropped to save memory/);
    }
    // Recent cards stay full.
    for (let i = cutoff; i < s.cards.length; i++) {
      const c = s.cards[i] as ToolCard;
      expect(c.output.startsWith("[elided")).toBe(false);
      expect(c.output.length).toBeGreaterThan(7000);
    }
  });

  it("doesn't double-elide cards on subsequent appends", () => {
    const s1 = run(buildBigToolEvents(RECENT_CARDS_WINDOW + 10));
    const firstOldOutput = (s1.cards[0] as ToolCard).output;
    expect(firstOldOutput.startsWith("[elided")).toBe(true);
    const lenAfterFirst = firstOldOutput.length;
    const s2 = run(buildBigToolEvents(20, "u"), s1);
    expect((s2.cards[0] as ToolCard).output).toBe(firstOldOutput);
    expect((s2.cards[0] as ToolCard).output.length).toBe(lenAfterFirst);
  });

  it("leaves small tool outputs alone (no point eliding a 200-byte result)", () => {
    const small = "tiny result";
    const events: AgentEvent[] = [];
    for (let i = 0; i < RECENT_CARDS_WINDOW + 10; i++) {
      events.push({ type: "tool.start", id: `t${i}`, name: "ls", args: {} });
      events.push({ type: "tool.end", id: `t${i}`, output: small, elapsedMs: 1 });
    }
    const s = run(events);
    for (const c of s.cards) {
      if (c.kind === "tool") expect((c as ToolCard).output).toBe(small);
    }
  });

  it("only touches tool cards — user / other card kinds are untouched", () => {
    const events: AgentEvent[] = [];
    for (let i = 0; i < RECENT_CARDS_WINDOW + 5; i++) {
      events.push({ type: "user.submit", text: `${BIG_OUTPUT}::${i}` });
    }
    const s = run(events);
    for (const c of s.cards) {
      expect(c.kind).toBe("user");
      expect((c as UserCard).text.startsWith("[elided")).toBe(false);
    }
  });
});
