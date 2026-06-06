import { describe, expect, it } from "vitest";
import type {
  ReasoningCard,
  StreamingCard,
  ToolCard,
  UserCard,
} from "../src/cli/ui/state/cards.js";
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

function buildBigReasoningEvents(count: number, idPrefix = "r"): AgentEvent[] {
  const out: AgentEvent[] = [];
  for (let i = 0; i < count; i++) {
    out.push({ type: "reasoning.start", id: `${idPrefix}${i}` });
    out.push({ type: "reasoning.chunk", id: `${idPrefix}${i}`, text: `${BIG_OUTPUT}::${i}` });
    out.push({ type: "reasoning.end", id: `${idPrefix}${i}`, paragraphs: 1, tokens: 100 });
  }
  return out;
}

function buildBigStreamingEvents(count: number, idPrefix = "s"): AgentEvent[] {
  const out: AgentEvent[] = [];
  for (let i = 0; i < count; i++) {
    out.push({ type: "streaming.start", id: `${idPrefix}${i}` });
    out.push({ type: "streaming.chunk", id: `${idPrefix}${i}`, text: `${BIG_OUTPUT}::${i}` });
    out.push({ type: "streaming.end", id: `${idPrefix}${i}` });
  }
  return out;
}

describe("reducer card-content elision (issue #1031 memory mitigation)", () => {
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
    const cutoff = s.cards.length - RECENT_CARDS_WINDOW;
    for (let i = 0; i < cutoff; i++) {
      const c = s.cards[i]!;
      expect(c.kind).toBe("tool");
      const out = (c as ToolCard).output;
      expect(out.startsWith("[elided")).toBe(true);
      expect(out.length).toBeLessThan(300);
      expect(out).toMatch(/chars dropped to save memory/);
    }
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

  it("user-authored text is never elided — user input is precious and small", () => {
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

  it("stubs old reasoning text once the window is exceeded", () => {
    const total = RECENT_CARDS_WINDOW + 30;
    const s = run(buildBigReasoningEvents(total));
    expect(s.cards).toHaveLength(total);
    const cutoff = s.cards.length - RECENT_CARDS_WINDOW;
    for (let i = 0; i < cutoff; i++) {
      const c = s.cards[i] as ReasoningCard;
      expect(c.kind).toBe("reasoning");
      expect(c.text.startsWith("[elided")).toBe(true);
      expect(c.text).toMatch(/chars dropped to save memory/);
    }
    for (let i = cutoff; i < s.cards.length; i++) {
      const c = s.cards[i] as ReasoningCard;
      expect(c.text.startsWith("[elided")).toBe(false);
      expect(c.text.length).toBeGreaterThan(7000);
    }
  });

  it("stubs old streaming text once the window is exceeded", () => {
    const total = RECENT_CARDS_WINDOW + 30;
    const s = run(buildBigStreamingEvents(total));
    expect(s.cards).toHaveLength(total);
    const cutoff = s.cards.length - RECENT_CARDS_WINDOW;
    for (let i = 0; i < cutoff; i++) {
      const c = s.cards[i] as StreamingCard;
      expect(c.kind).toBe("streaming");
      expect(c.text.startsWith("[elided")).toBe(true);
    }
    for (let i = cutoff; i < s.cards.length; i++) {
      const c = s.cards[i] as StreamingCard;
      expect(c.text.startsWith("[elided")).toBe(false);
    }
  });

  it("never elides a reasoning card that is still streaming — chunks would append to the stub", () => {
    const events: AgentEvent[] = [];
    events.push({ type: "reasoning.start", id: "r-stuck" });
    events.push({ type: "reasoning.chunk", id: "r-stuck", text: BIG_OUTPUT });
    // r-stuck has no `reasoning.end` — still streaming.
    for (let i = 0; i < RECENT_CARDS_WINDOW + 10; i++) {
      events.push({ type: "user.submit", text: `msg ${i}` });
    }
    const s = run(events);
    const stuck = s.cards.find((c) => c.id === "r-stuck") as ReasoningCard | undefined;
    expect(stuck).toBeDefined();
    expect(stuck!.streaming).toBe(true);
    expect(stuck!.text.startsWith("[elided")).toBe(false);
    expect(stuck!.text.length).toBeGreaterThan(7000);
  });

  it("never elides a streaming card before streaming.end — chunks would append to the stub", () => {
    const events: AgentEvent[] = [];
    events.push({ type: "streaming.start", id: "s-stuck" });
    events.push({ type: "streaming.chunk", id: "s-stuck", text: BIG_OUTPUT });
    // s-stuck has no `streaming.end` — still in flight.
    for (let i = 0; i < RECENT_CARDS_WINDOW + 10; i++) {
      events.push({ type: "user.submit", text: `msg ${i}` });
    }
    const s = run(events);
    const stuck = s.cards.find((c) => c.id === "s-stuck") as StreamingCard | undefined;
    expect(stuck).toBeDefined();
    expect(stuck!.done).toBe(false);
    expect(stuck!.text.startsWith("[elided")).toBe(false);
  });
});
