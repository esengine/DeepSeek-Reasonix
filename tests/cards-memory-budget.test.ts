/** End-to-end memory budget for the cards store on a multi-turn session (issue #1031). Runs realistic event streams through the live reducer, then walks state.cards and reports retained bytes by kind + field — so a regression that re-grows a heavy field shows up as a budget breach instead of a silent OOM in prod. */

import { describe, expect, it } from "vitest";
import type { Card } from "../src/cli/ui/state/cards.js";
import type { AgentEvent } from "../src/cli/ui/state/events.js";
import { reduce } from "../src/cli/ui/state/reducer.js";
import { type AgentState, type SessionInfo, initialState } from "../src/cli/ui/state/state.js";

const session: SessionInfo = {
  id: "mem-budget",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

/** Per-turn payload sizes calibrated to a thinking-model session (Opus-ish reasoning, mid-size tool reads). */
const SIZES = {
  userPrompt: 200,
  reasoningChars: 50_000,
  streamingChars: 15_000,
  toolOutputChars: 25_000,
  toolsPerTurn: 2,
};

interface SizeReport {
  cardCount: number;
  totalBytes: number;
  byKind: Map<string, { count: number; bytes: number }>;
  byField: Map<string, number>;
}

function bytesUtf8(s: string): number {
  return Buffer.byteLength(s, "utf8");
}

function measureCards(cards: ReadonlyArray<Card>): SizeReport {
  const byKind = new Map<string, { count: number; bytes: number }>();
  const byField = new Map<string, number>();
  let total = 0;

  const bump = (kind: string, field: string, b: number) => {
    const k = byKind.get(kind) ?? { count: 0, bytes: 0 };
    k.bytes += b;
    byKind.set(kind, k);
    byField.set(`${kind}.${field}`, (byField.get(`${kind}.${field}`) ?? 0) + b);
    total += b;
  };

  for (const c of cards) {
    const k = byKind.get(c.kind) ?? { count: 0, bytes: 0 };
    k.count++;
    byKind.set(c.kind, k);
    bump(c.kind, "_meta", 64);
    bump(c.kind, "id", bytesUtf8(c.id));

    switch (c.kind) {
      case "user":
        bump("user", "text", bytesUtf8(c.text));
        break;
      case "reasoning":
        bump("reasoning", "text", bytesUtf8(c.text));
        break;
      case "streaming":
        bump("streaming", "text", bytesUtf8(c.text));
        break;
      case "tool":
        bump("tool", "output", bytesUtf8(c.output));
        bump("tool", "name", bytesUtf8(c.name));
        bump("tool", "args", bytesUtf8(JSON.stringify(c.args ?? null)));
        break;
      case "diff":
        for (const h of c.hunks) {
          bump("diff", "header", bytesUtf8(h.header));
          for (const l of h.lines) bump("diff", "lines", bytesUtf8(l.text));
        }
        break;
      default:
        break;
    }
  }
  return { cardCount: cards.length, totalBytes: total, byKind, byField };
}

function buildTurnEvents(turnIdx: number): AgentEvent[] {
  const evs: AgentEvent[] = [];
  const tag = `t${turnIdx}`;
  evs.push({ type: "user.submit", text: "u".repeat(SIZES.userPrompt) });
  evs.push({ type: "reasoning.start", id: `${tag}-r` });
  evs.push({ type: "reasoning.chunk", id: `${tag}-r`, text: "r".repeat(SIZES.reasoningChars) });
  evs.push({ type: "reasoning.end", id: `${tag}-r`, paragraphs: 1, tokens: 1000 });
  evs.push({ type: "streaming.start", id: `${tag}-s` });
  evs.push({ type: "streaming.chunk", id: `${tag}-s`, text: "s".repeat(SIZES.streamingChars) });
  evs.push({ type: "streaming.end", id: `${tag}-s` });
  for (let i = 0; i < SIZES.toolsPerTurn; i++) {
    const tid = `${tag}-tool${i}`;
    evs.push({ type: "tool.start", id: tid, name: "read_file", args: { path: "a/b/c.ts" } });
    evs.push({
      type: "tool.end",
      id: tid,
      output: "o".repeat(SIZES.toolOutputChars),
      elapsedMs: 50,
    });
  }
  return evs;
}

function runSession(turns: number): AgentState {
  let state = initialState(session);
  for (let t = 0; t < turns; t++) {
    for (const ev of buildTurnEvents(t)) state = reduce(state, ev);
  }
  return state;
}

function rawInputBytes(turns: number): number {
  const perTurn =
    SIZES.userPrompt +
    SIZES.reasoningChars +
    SIZES.streamingChars +
    SIZES.toolOutputChars * SIZES.toolsPerTurn;
  return perTurn * turns;
}

function formatReport(label: string, r: SizeReport): string {
  const mb = (b: number) => (b / 1024 / 1024).toFixed(2);
  const lines: string[] = [];
  lines.push(`\n--- ${label} ---`);
  lines.push(`cards retained: ${r.cardCount}, total bytes: ${mb(r.totalBytes)} MB`);
  const kinds = [...r.byKind.entries()].sort((a, b) => b[1].bytes - a[1].bytes);
  for (const [kind, info] of kinds) {
    lines.push(
      `  ${kind.padEnd(10)} count=${String(info.count).padStart(5)}  ${mb(info.bytes).padStart(7)} MB`,
    );
  }
  lines.push("  top field consumers:");
  const fields = [...r.byField.entries()].sort((a, b) => b[1] - a[1]).slice(0, 8);
  for (const [name, b] of fields) {
    lines.push(`    ${name.padEnd(22)} ${mb(b).padStart(7)} MB`);
  }
  return lines.join("\n");
}

describe("cards memory budget end-to-end (issue #1031)", () => {
  it("100-turn session stays well under window — every card kept full", () => {
    const turns = 100;
    const r = measureCards(runSession(turns).cards);
    console.log(formatReport(`${turns}-turn session (below elision window)`, r));
    expect(r.cardCount).toBeLessThan(600);
    expect(r.totalBytes).toBeLessThan(rawInputBytes(turns) + 1024 * 1024);
  });

  it("1000-turn session: elision keeps retained bytes under 10% of raw input", () => {
    const turns = 1000;
    const r = measureCards(runSession(turns).cards);
    const raw = rawInputBytes(turns);
    console.log(formatReport(`${turns}-turn session (post-elision)`, r));
    console.log(
      `\nelided=${(r.totalBytes / 1024 / 1024).toFixed(2)} MB,  raw-input=${(raw / 1024 / 1024).toFixed(2)} MB,  saved=${(((raw - r.totalBytes) / raw) * 100).toFixed(1)}%\n`,
    );
    expect(r.totalBytes).toBeLessThan(raw * 0.1);
  });

  it("no single card kind retains more than the recent window's worth of full content", () => {
    const turns = 1000;
    const r = measureCards(runSession(turns).cards);
    const ceiling = (kind: string, perCardBytes: number, retainedFullCards = 200) =>
      perCardBytes * retainedFullCards + r.byKind.get(kind)!.count * 256;

    expect(r.byKind.get("tool")!.bytes).toBeLessThan(ceiling("tool", SIZES.toolOutputChars));
    expect(r.byKind.get("reasoning")!.bytes).toBeLessThan(
      ceiling("reasoning", SIZES.reasoningChars),
    );
    expect(r.byKind.get("streaming")!.bytes).toBeLessThan(
      ceiling("streaming", SIZES.streamingChars),
    );
  });

  it("growth is sublinear past the recent window — doubling turns barely doubles bytes", () => {
    const r1k = measureCards(runSession(1000).cards);
    const r2k = measureCards(runSession(2000).cards);
    const ratio = r2k.totalBytes / r1k.totalBytes;
    console.log(
      `1000-turn=${(r1k.totalBytes / 1024 / 1024).toFixed(2)} MB,  2000-turn=${(r2k.totalBytes / 1024 / 1024).toFixed(2)} MB,  ratio=${ratio.toFixed(2)}x`,
    );
    expect(ratio).toBeLessThan(1.5);
  });

  it("elides old tool arguments, not just old tool output", () => {
    let state = initialState(session);
    const turns = 400;
    const argPayload = "input payload\n".repeat(2000);
    const rawArgsBytes = bytesUtf8(JSON.stringify({ path: "big.txt", content: argPayload }));

    for (let i = 0; i < turns; i++) {
      const id = `tool-arg-${i}`;
      state = reduce(state, {
        type: "tool.start",
        id,
        name: "write_file",
        args: { path: "big.txt", content: argPayload },
      });
      state = reduce(state, {
        type: "tool.end",
        id,
        output: "ok",
        elapsedMs: 1,
      });
    }

    const r = measureCards(state.cards);
    const retainedArgBytes = r.byField.get("tool.args") ?? 0;
    expect(retainedArgBytes).toBeLessThan(rawArgsBytes * turns * 0.6);
  });

  it("actual V8 heap delta on a long session stays under 100 MB (smoke check)", () => {
    if (typeof globalThis.gc !== "function") {
      console.log("(skipped — run with NODE_OPTIONS=--expose-gc for real heap delta)");
      return;
    }
    globalThis.gc();
    const before = process.memoryUsage().heapUsed;
    const state = runSession(1000);
    globalThis.gc();
    const after = process.memoryUsage().heapUsed;
    const deltaMB = (after - before) / 1024 / 1024;
    console.log(
      `V8 heap delta over 1000 turns: ${deltaMB.toFixed(1)} MB, cards retained: ${state.cards.length}`,
    );
    expect(deltaMB).toBeLessThan(100);
  });
});
