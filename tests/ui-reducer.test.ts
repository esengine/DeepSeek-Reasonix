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
import {
  USD_TO_CNY,
  balanceColorCny,
  balanceColorForBalance,
  formatBalance,
  formatBalanceLabel,
  formatCost,
  formatWalletDisplay,
} from "../src/cli/ui/theme/tokens.js";

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
  it("formats USD balance with $ sign", () => {
    expect(formatBalance({ currency: "USD", total: 0.91 })).toBe("$0.91");
  });

  it("formats CNY balance with ¥ sign", () => {
    expect(formatBalance({ currency: "CNY", total: 6.55 })).toBe("¥6.55");
  });

  it("formats unknown currency with ISO code suffix", () => {
    expect(formatBalance({ currency: "EUR", total: 1.23 })).toBe("EUR 1.23");
  });
});

describe("formatBalanceLabel", () => {
  it("formats USD with 'w $' prefix (ChromeBar style)", () => {
    expect(formatBalanceLabel({ currency: "USD", total: 0.91 })).toBe("w $0.91");
  });

  it("formats CNY with 'w ¥' prefix (ChromeBar style)", () => {
    expect(formatBalanceLabel({ currency: "CNY", total: 6.55 })).toBe("w ¥6.55");
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

// Every test below imports from tokens.ts. They fail when exports are missing.

describe("balanceColorCny (StatusRow:12 - exported from tokens.ts)", () => {
  // Thresholds: < ¥5 -> err (red), ¥5-20 -> warn (yellow), >= ¥20 -> brand (blue)
  // Currently a private function in StatusRow.tsx.  The fix must export it from
  // tokens.ts so both StatusRow and tests can import it.

  it("returns err for < ¥5", () => {
    expect(balanceColorCny(0)).toBe("#ff8b81"); // TONE.err
    expect(balanceColorCny(3)).toBe("#ff8b81");
    expect(balanceColorCny(4.99)).toBe("#ff8b81");
  });

  it("returns warn for ¥5-20", () => {
    expect(balanceColorCny(5)).toBe("#f0b07d"); // TONE.warn
    expect(balanceColorCny(10)).toBe("#f0b07d");
    expect(balanceColorCny(19.99)).toBe("#f0b07d");
  });

  it("returns brand for >= ¥20", () => {
    expect(balanceColorCny(20)).toBe("#79c0ff"); // TONE.brand
    expect(balanceColorCny(100)).toBe("#79c0ff");
  });
});

describe("balanceColorForBalance (currency-aware - new in tokens.ts)", () => {
  // USD balances must be converted to CNY before threshold check, otherwise
  // $0.91 would show as red when it's actually ~¥6.55 (should be yellow).
  // This is the bug on StatusRow:60 - balanceColor receives the raw API number.

  it("USD $0.91 (~¥6.55) -> warn, NOT err", () => {
    expect(balanceColorForBalance(0.91, "USD")).toBe("#f0b07d");
  });

  it("USD $0.50 (~¥3.60) -> err", () => {
    expect(balanceColorForBalance(0.5, "USD")).toBe("#ff8b81");
  });

  it("USD $3.00 (~¥21.60) -> brand", () => {
    expect(balanceColorForBalance(3.0, "USD")).toBe("#79c0ff");
  });

  it("CNY ¥3 -> err", () => {
    expect(balanceColorForBalance(3, "CNY")).toBe("#ff8b81");
  });

  it("CNY ¥8 -> warn", () => {
    expect(balanceColorForBalance(8, "CNY")).toBe("#f0b07d");
  });

  it("CNY ¥25 -> brand", () => {
    expect(balanceColorForBalance(25, "CNY")).toBe("#79c0ff");
  });

  it("unknown currency: treats value as-is (no conversion)", () => {
    expect(balanceColorForBalance(3, "EUR")).toBe("#ff8b81");
    expect(balanceColorForBalance(10, "EUR")).toBe("#f0b07d");
  });
});

describe("formatWalletDisplay (StatusRow:61, UsageCard:74,95 - new in tokens.ts)", () => {
  // These three call sites currently hardcode `¥${balance.toFixed(2)}`.
  // After fix they call formatWalletDisplay(balance, balanceCurrency).

  it("USD balance -> $0.91", () => {
    expect(formatWalletDisplay(0.91, "USD")).toBe("$0.91");
  });

  it("CNY balance -> ¥6.55", () => {
    expect(formatWalletDisplay(6.55, "CNY")).toBe("¥6.55");
  });

  it("unknown currency: shows ISO code suffix", () => {
    expect(formatWalletDisplay(1.23, "EUR")).toBe("EUR 1.23");
  });

  it("undefined currency (legacy): bare number, no symbol", () => {
    expect(formatWalletDisplay(10.0, undefined)).toBe("10.00");
  });

  it("undefined balance -> null (hide wallet)", () => {
    expect(formatWalletDisplay(undefined, "USD")).toBeNull();
  });
});

describe("formatCost (turn/session — currency-aware)", () => {
  it("USD wallet: cost in $ with no conversion", () => {
    expect(formatCost(0.0308, "USD")).toBe("$0.0308");
  });

  it("USD wallet: session cost in $", () => {
    expect(formatCost(0.064, "USD", 3)).toBe("$0.064");
  });

  it("CNY wallet: cost in ¥ converted from USD", () => {
    expect(formatCost(0.0308, "CNY")).toBe("¥0.2218");
  });

  it("CNY wallet: session cost in ¥", () => {
    expect(formatCost(0.064, "CNY", 3)).toBe("¥0.461");
  });

  it("no wallet (undefined): cost in ¥ (backward compat)", () => {
    expect(formatCost(0.0308, undefined)).toBe("¥0.2218");
  });
});
