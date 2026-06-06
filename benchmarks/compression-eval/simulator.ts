import { readFileSync } from "node:fs";
import {
  countTokensBounded,
  estimateConversationTokens,
  estimateRequestTokens,
} from "../../src/tokenizer.js";
import type { ChatMessage } from "../../src/types.js";

export interface ThresholdConfig {
  ctxMax: number;
  foldThreshold: number;
  tailFraction: number;
  aggressiveThreshold: number;
  aggressiveTailFraction: number;
  minSavingsFraction: number;
  summaryRatio: number;
}

export interface FoldEvent {
  turn: number;
  ratioBeforeFold: number;
  promptTokensBefore: number;
  headMessages: number;
  tailMessages: number;
  headTokens: number;
  summaryTokens: number;
  aggressive: boolean;
}

export interface TurnRecord {
  turn: number;
  promptTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  ratio: number;
  folded: boolean;
}

export interface RunResult {
  config: ThresholdConfig;
  sessionPath: string;
  totalTurns: number;
  totalInputTokens: number;
  totalCacheHitTokens: number;
  totalCacheMissTokens: number;
  cacheHitRatio: number;
  foldCount: number;
  folds: FoldEvent[];
  perTurn: TurnRecord[];
}

export function loadSession(path: string): ChatMessage[] {
  const lines = readFileSync(path, "utf8").split("\n");
  const out: ChatMessage[] = [];
  for (const ln of lines) {
    const t = ln.trim();
    if (!t) continue;
    try {
      out.push(JSON.parse(t) as ChatMessage);
    } catch {
      continue;
    }
  }
  return out;
}

function messageTokens(m: ChatMessage): number {
  return estimateConversationTokens([m], true);
}

function messageKey(m: ChatMessage): string {
  return JSON.stringify({
    r: m.role,
    c: m.content ?? "",
    tc: m.tool_calls ?? null,
    tcid: m.tool_call_id ?? null,
  });
}

function commonPrefixTokens(curr: ChatMessage[], prev: ChatMessage[]): number {
  const n = Math.min(curr.length, prev.length);
  let tokens = 0;
  for (let i = 0; i < n; i++) {
    if (messageKey(curr[i]!) !== messageKey(prev[i]!)) break;
    tokens += messageTokens(curr[i]!);
  }
  return tokens;
}

function findFoldBoundary(log: ChatMessage[], tailBudget: number): number {
  let cum = 0;
  let boundary = log.length;
  for (let i = log.length - 1; i >= 0; i--) {
    const t = messageTokens(log[i]!);
    if (cum + t > tailBudget) break;
    cum += t;
    if (log[i]!.role === "user") boundary = i;
  }
  return boundary;
}

function synthesizeSummary(head: ChatMessage[], targetTokens: number): ChatMessage {
  const userBits: string[] = [];
  for (const m of head) {
    if (m.role === "user" && typeof m.content === "string") userBits.push(m.content);
  }
  const seed = userBits.join("\n") || "earlier turns folded";
  let text = `[CONVERSATION HISTORY SUMMARY — fold of ${head.length} messages]\n\n${seed}`;
  const currentTokens = countTokensBounded(text);
  if (currentTokens > targetTokens) {
    const ratio = targetTokens / currentTokens;
    text = text.slice(0, Math.max(80, Math.floor(text.length * ratio)));
  }
  return { role: "assistant", content: text, reasoning_content: "" };
}

function turnBoundaries(messages: ChatMessage[]): number[] {
  const ends: number[] = [];
  for (let i = 0; i < messages.length; i++) {
    if (messages[i]!.role === "assistant") ends.push(i);
  }
  return ends;
}

export function simulate(messages: ChatMessage[], config: ThresholdConfig, sessionPath: string): RunResult {
  const ends = turnBoundaries(messages);
  const log: ChatMessage[] = [];
  let prevSnapshot: ChatMessage[] = [];
  const perTurn: TurnRecord[] = [];
  const folds: FoldEvent[] = [];
  let totalInput = 0;
  let totalHit = 0;
  let totalMiss = 0;
  let cursor = 0;

  for (let turn = 0; turn < ends.length; turn++) {
    const end = ends[turn]!;
    for (let i = cursor; i < end; i++) log.push(messages[i]!);
    cursor = end;

    const promptTokens = estimateRequestTokens(log, null, true);
    const ratio = promptTokens / config.ctxMax;
    const hit = commonPrefixTokens(log, prevSnapshot);
    const miss = Math.max(0, promptTokens - hit);
    let folded = false;

    if (ratio > config.foldThreshold) {
      const aggressive = ratio > config.aggressiveThreshold;
      const tailFrac = aggressive ? config.aggressiveTailFraction : config.tailFraction;
      const tailBudget = Math.floor(config.ctxMax * tailFrac);
      const boundary = findFoldBoundary(log, tailBudget);
      if (boundary > 0) {
        const head = log.slice(0, boundary);
        const tail = log.slice(boundary);
        const headTokens = head.reduce((a, m) => a + messageTokens(m), 0);
        const totalTokens = log.reduce((a, m) => a + messageTokens(m), 0);
        if (headTokens >= totalTokens * config.minSavingsFraction) {
          const summaryTokens = Math.max(64, Math.floor(headTokens * config.summaryRatio));
          const summary = synthesizeSummary(head, summaryTokens);
          const next = [summary, ...tail];
          log.length = 0;
          for (const m of next) log.push(m);
          folds.push({
            turn,
            ratioBeforeFold: ratio,
            promptTokensBefore: promptTokens,
            headMessages: head.length,
            tailMessages: tail.length,
            headTokens,
            summaryTokens,
            aggressive,
          });
          folded = true;
        }
      }
    }

    totalInput += promptTokens;
    totalHit += hit;
    totalMiss += miss;
    perTurn.push({ turn, promptTokens, cacheHitTokens: hit, cacheMissTokens: miss, ratio, folded });
    prevSnapshot = log.slice();
  }

  return {
    config,
    sessionPath,
    totalTurns: ends.length,
    totalInputTokens: totalInput,
    totalCacheHitTokens: totalHit,
    totalCacheMissTokens: totalMiss,
    cacheHitRatio: totalInput > 0 ? totalHit / totalInput : 0,
    foldCount: folds.length,
    folds,
    perTurn,
  };
}
