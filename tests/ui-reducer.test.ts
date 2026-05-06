import { describe, expect, it } from "vitest";
import type {
  ReasoningCard,
  StreamingCard,
  ToolCard,
  UsageCard,
  UserCard,
} from "../src/cli/ui/state/cards.js";
import type { AgentEvent } from "../src/cli/ui/state/events.js";
import { parseEvent } from "../src/cli/ui/state/events.js";
import { reduce } from "../src/cli/ui/state/reducer.js";
import { type AgentState, type SessionInfo, initialState } from "../src/cli/ui/state/state.js";
import { USD_TO_CNY, balanceColor, formatBalance, formatCost } from "../src/cli/ui/theme/tokens.js";

const session: SessionInfo = {
  id: "test-session",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

function run(events: AgentEvent[], from: AgentState = initialState(session)): AgentState {
  return events.reduce(reduce, from);
}

describe("ui reducer", () => {
  it("appends a user card on user.submit", () => {
    const s = run([{ type: "user.submit", text: "hello world" }]);
    expect(s.cards).toHaveLength(1);
    const card = s.cards[0] as UserCard;
    expect(card.kind).toBe("user");
    expect(card.text).toBe("hello world");
  });

  it("streams reasoning chunks into a single card", () => {
    const s = run([
      { type: "reasoning.start", id: "r1" },
      { type: "reasoning.chunk", id: "r1", text: "Two paths: " },
      { type: "reasoning.chunk", id: "r1", text: "A or B." },
      { type: "reasoning.end", id: "r1", paragraphs: 1, tokens: 12 },
    ]);
    expect(s.cards).toHaveLength(1);
    const card = s.cards[0] as ReasoningCard;
    expect(card.text).toBe("Two paths: A or B.");
    expect(card.streaming).toBe(false);
    expect(card.paragraphs).toBe(1);
    expect(card.tokens).toBe(12);
  });

  it("snapshots the producing model on reasoning.start so mid-turn escalation doesn't relabel it", () => {
    const s = run([{ type: "reasoning.start", id: "r1" }]);
    const card = s.cards[0] as ReasoningCard;
    expect(card.model).toBe("deepseek-chat");
  });

  it("streams response chunks into a single streaming card", () => {
    const s = run([
      { type: "streaming.start", id: "s1" },
      { type: "streaming.chunk", id: "s1", text: "The change " },
      { type: "streaming.chunk", id: "s1", text: "maps to..." },
    ]);
    expect(s.cards).toHaveLength(1);
    const card = s.cards[0] as StreamingCard;
    expect(card.text).toBe("The change maps to...");
    expect(card.done).toBe(false);
  });

  it("marks streaming card done on streaming.end", () => {
    const s = run([
      { type: "streaming.start", id: "s1" },
      { type: "streaming.chunk", id: "s1", text: "ok" },
      { type: "streaming.end", id: "s1" },
    ]);
    expect((s.cards[0] as StreamingCard).done).toBe(true);
  });

  it("ignores chunks for unknown ids", () => {
    const s = run([{ type: "streaming.chunk", id: "missing", text: "lost" }]);
    expect(s.cards).toHaveLength(0);
  });

  it("flags tool card as rejected when tool.end output carries plan-mode marker", () => {
    const planBounce = JSON.stringify({
      error: "write_file: unavailable in plan mode — ...",
      rejectedReason: "plan-mode",
    });
    const s = run([
      { type: "tool.start", id: "t1", name: "write_file", args: { path: "x.ts", content: "y" } },
      { type: "tool.end", id: "t1", output: planBounce, elapsedMs: 2 },
    ]);
    const card = s.cards[0] as ToolCard;
    expect(card.rejected).toBe(true);
    expect(card.done).toBe(true);
  });

  it("does not flag rejection on a regular error output", () => {
    const s = run([
      { type: "tool.start", id: "t1", name: "edit_file", args: { path: "x" } },
      {
        type: "tool.end",
        id: "t1",
        output: JSON.stringify({ error: "edit_file: search not found" }),
        elapsedMs: 5,
      },
    ]);
    const card = s.cards[0] as ToolCard;
    expect(card.rejected).toBeUndefined();
  });

  it("changes mode and accumulates session cost", () => {
    const s = run([
      { type: "mode.change", mode: "ask" },
      {
        type: "turn.end",
        usage: { prompt: 1000, reason: 100, output: 50, cacheHit: 0.9, cost: 0.0014 },
      },
      {
        type: "turn.end",
        usage: { prompt: 1000, reason: 100, output: 50, cacheHit: 0.92, cost: 0.0016 },
      },
    ]);
    expect(s.status.mode).toBe("ask");
    expect(s.status.cost).toBeCloseTo(0.0016);
    expect(s.status.sessionCost).toBeCloseTo(0.003);
    expect(s.status.cacheHit).toBeCloseTo(0.92);
  });

  it("turn.end + session.update sets all display fields", () => {
    // Full flow: a turn completes (updates cost/sessionCost), then the
    // App dispatches balance + balanceCurrency via session.update.
    const s = run([
      {
        type: "turn.end",
        usage: { prompt: 1000, reason: 0, output: 200, cacheHit: 0.8, cost: 0.00015 },
      },
      {
        type: "session.update",
        patch: { balance: 0.71, balanceCurrency: "USD" },
      },
    ]);
    expect(s.status.cost).toBeCloseTo(0.00015);
    expect(s.status.sessionCost).toBeCloseTo(0.00015);
    expect(s.status.balance).toBe(0.71);
    expect(s.status.balanceCurrency).toBe("USD");
  });

  it("multiple turn.end events accumulate sessionCost with balanceCurrency from session.update", () => {
    const s = run([
      {
        type: "turn.end",
        usage: { prompt: 500, reason: 0, output: 100, cacheHit: 0.9, cost: 0.0001 },
      },
      {
        type: "turn.end",
        usage: { prompt: 1000, reason: 0, output: 300, cacheHit: 0.7, cost: 0.0003 },
      },
      { type: "session.update", patch: { balance: 5.0, balanceCurrency: "CNY" } },
      {
        type: "turn.end",
        usage: { prompt: 200, reason: 0, output: 50, cacheHit: 0.95, cost: 0.00005 },
      },
    ]);
    expect(s.status.cost).toBeCloseTo(0.00005); // last turn
    expect(s.status.sessionCost).toBeCloseTo(0.00045); // total: 0.0001+0.0003+0.00005
    expect(s.status.balance).toBe(5.0);
    expect(s.status.balanceCurrency).toBe("CNY");
  });

  it("focus.move walks cards forward and back, clamped at edges", () => {
    let s = run([
      { type: "user.submit", text: "a" },
      { type: "user.submit", text: "b" },
      { type: "user.submit", text: "c" },
    ]);
    s = reduce(s, { type: "focus.move", direction: "first" });
    expect(s.focusedCardId).toBe(s.cards[0]?.id);
    s = reduce(s, { type: "focus.move", direction: "next" });
    expect(s.focusedCardId).toBe(s.cards[1]?.id);
    s = reduce(s, { type: "focus.move", direction: "next" });
    expect(s.focusedCardId).toBe(s.cards[2]?.id);
    s = reduce(s, { type: "focus.move", direction: "next" });
    expect(s.focusedCardId).toBe(s.cards[2]?.id);
    s = reduce(s, { type: "focus.move", direction: "prev" });
    expect(s.focusedCardId).toBe(s.cards[1]?.id);
  });

  it("composer input clears the abort hint", () => {
    let s = run([{ type: "turn.abort" }]);
    expect(s.composer.abortedHint).toBe(true);
    s = reduce(s, { type: "composer.input", value: "n" });
    expect(s.composer.abortedHint).toBe(false);
  });
});

describe("event schema", () => {
  it("parses well-formed events", () => {
    const ev = parseEvent({ type: "user.submit", text: "hi" });
    expect(ev?.type).toBe("user.submit");
  });

  it("rejects malformed events", () => {
    expect(parseEvent({ type: "user.submit" })).toBeNull();
    expect(parseEvent({ type: "unknown" })).toBeNull();
    expect(parseEvent({ type: "streaming.chunk", id: "", text: "x" })).toBeNull();
  });

  it("validates discriminated union variants", () => {
    expect(parseEvent({ type: "mode.change", mode: "auto" })?.type).toBe("mode.change");
    expect(parseEvent({ type: "mode.change", mode: "invalid" })).toBeNull();
  });

  it("accepts balanceCurrency in session.update events", () => {
    const ev = parseEvent({
      type: "session.update",
      patch: { balance: 0.91, balanceCurrency: "USD" },
    } as any);
    expect(ev).not.toBeNull();
    expect((ev as any)?.patch?.balanceCurrency).toBe("USD");
  });

  it("accepts balanceCurrency in usage.show events", () => {
    const ev = parseEvent({
      type: "usage.show",
      id: "u1",
      turn: 1,
      tokens: { prompt: 100, reason: 50, output: 20, promptCap: 1000 },
      cacheHit: 0.5,
      cost: 0.001,
      sessionCost: 0.01,
      balance: 0.91,
      balanceCurrency: "USD",
    } as any);
    expect(ev).not.toBeNull();
    expect((ev as any)?.balanceCurrency).toBe("USD");
  });
});

describe("formatBalance", () => {
  it("USD → $0.91", () => {
    expect(formatBalance(0.91, "USD")).toBe("$0.91");
  });

  it("CNY → ¥6.55", () => {
    expect(formatBalance(6.55, "CNY")).toBe("¥6.55");
  });

  it("undefined currency defaults to CNY (matches pre-fix unconditional ¥)", () => {
    expect(formatBalance(0.91)).toBe("¥0.91");
  });

  it("unknown currency falls back to ISO-code prefix", () => {
    expect(formatBalance(1.23, "EUR")).toBe("EUR 1.23");
  });

  it("label option produces ChromeBar 'w $0.91' style", () => {
    expect(formatBalance(0.91, "USD", { label: true })).toBe("w $0.91");
    expect(formatBalance(6.55, "CNY", { label: true })).toBe("w ¥6.55");
  });

  it("fractionDigits option overrides the 2-digit default", () => {
    expect(formatBalance(0.0308, "USD", { fractionDigits: 4 })).toBe("$0.0308");
  });
});

describe("balance currency in reducer", () => {
  it("session.update propagates balanceCurrency to status", () => {
    const s = run([
      { type: "session.update", patch: { balance: 0.91, balanceCurrency: "USD" } } as any,
    ]);
    expect((s.status as any).balanceCurrency).toBe("USD");
    expect(s.status.balance).toBe(0.91);
  });

  it("usage.show card stores balanceCurrency on the card", () => {
    const s = run([
      {
        type: "usage.show",
        id: "u1",
        turn: 3,
        tokens: { prompt: 500, reason: 200, output: 100, promptCap: 1024 },
        cacheHit: 0.8,
        cost: 0.002,
        sessionCost: 0.05,
        balance: 0.91,
        balanceCurrency: "USD",
      } as any,
    ]);
    const card = s.cards[0] as UsageCard;
    expect(card.kind).toBe("usage");
    expect((card as any).balanceCurrency).toBe("USD");
    expect(card.balance).toBe(0.91);
  });

  it("balance stays undefined when not provided", () => {
    const s = run([{ type: "session.update", patch: {} } as any]);
    expect(s.status.balance).toBeUndefined();
    expect((s.status as any).balanceCurrency).toBeUndefined();
  });
});

describe("balanceColor", () => {
  // CNY thresholds: < ¥5 → err (red), ¥5-20 → warn (yellow), >= ¥20 → brand (blue).
  // USD balances are multiplied by USD_TO_CNY before the threshold check.

  it("CNY → threshold checked directly", () => {
    expect(balanceColor(3, "CNY")).toBe("#ff8b81"); // err
    expect(balanceColor(8, "CNY")).toBe("#f0b07d"); // warn
    expect(balanceColor(25, "CNY")).toBe("#79c0ff"); // brand
  });

  it("USD → converted to CNY before threshold check ($0.91 ≈ ¥6.55 → warn)", () => {
    expect(balanceColor(0.5, "USD")).toBe("#ff8b81"); // ≈ ¥3.60 → err
    expect(balanceColor(0.91, "USD")).toBe("#f0b07d"); // ≈ ¥6.55 → warn
    expect(balanceColor(3.0, "USD")).toBe("#79c0ff"); // ≈ ¥21.60 → brand
  });

  it("undefined currency defaults to CNY (matches pre-fix behavior)", () => {
    expect(balanceColor(8)).toBe("#f0b07d");
  });
});

describe("formatCost (turn/session — currency-aware)", () => {
  it("USD wallet: cost in $, no conversion", () => {
    expect(formatCost(0.0308, "USD")).toBe("$0.0308");
    expect(formatCost(0.064, "USD", 3)).toBe("$0.064");
  });

  it("CNY wallet: USD cost multiplied to ¥", () => {
    expect(formatCost(0.0308, "CNY")).toBe("¥0.2218");
    expect(formatCost(0.064, "CNY", 3)).toBe("¥0.461");
  });

  it("undefined currency defaults to CNY (backward compat)", () => {
    expect(formatCost(0.0308)).toBe("¥0.2218");
  });
});
