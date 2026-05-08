/** Transcripts are receipts (cost/usage/prefix); sessions are memory (ChatMessages). Don't conflate. */

import { type WriteStream, createWriteStream, readFileSync } from "node:fs";
import type { LoopEvent } from "../loop.js";
import type { RawUsage } from "../types.js";

export interface TranscriptRecord {
  /** ISO-8601 timestamp at emit time. */
  ts: string;
  /** 1-based turn number within the session. */
  turn: number;
  /** LoopEvent role — "assistant_delta" | "assistant_final" | "tool" | "done" | ... */
  role: string;
  /** For assistant events, the final (or delta) text; for tool events, the tool result. */
  content: string;
  /** Tool name (role === "tool"). */
  tool?: string;
  /** JSON-string args the model sent for a tool call (role === "tool"). Persisted so diff can explain *why* two runs made different calls. */
  args?: string;
  /** DeepSeek token-usage snapshot (role === "assistant_final"). */
  usage?: RawUsage;
  /** USD cost of this turn (role === "assistant_final"). */
  cost?: number;
  /** Model id that produced this turn. */
  model?: string;
  /** Lets diff attribute cache-hit delta to log stability vs prompt change. */
  prefixHash?: string;
  /** Optional error message (role === "error"). */
  error?: string;
}

export interface TranscriptMeta {
  version: 1;
  source: string; // e.g. "reasonix chat", "bench/baseline", "bench/reasonix"
  model?: string;
  task?: string;
  mode?: string;
  repeat?: number;
  startedAt: string;
}

interface MetaLine {
  role: "_meta";
  meta: TranscriptMeta;
}

export interface ReadTranscriptResult {
  meta: TranscriptMeta | null;
  records: TranscriptRecord[];
}

export function recordFromLoopEvent(
  ev: LoopEvent,
  extra: { model: string; prefixHash: string },
): TranscriptRecord {
  const rec: TranscriptRecord = {
    ts: new Date().toISOString(),
    turn: ev.turn,
    role: ev.role,
    content: ev.content,
  };
  if (ev.toolName !== undefined) rec.tool = ev.toolName;
  if (ev.toolArgs !== undefined) rec.args = ev.toolArgs;
  if (ev.error !== undefined) rec.error = ev.error;
  if (ev.stats) {
    rec.usage = {
      prompt_tokens: ev.stats.usage.promptTokens,
      completion_tokens: ev.stats.usage.completionTokens,
      total_tokens: ev.stats.usage.totalTokens,
      prompt_cache_hit_tokens: ev.stats.usage.promptCacheHitTokens,
      prompt_cache_miss_tokens: ev.stats.usage.promptCacheMissTokens,
    };
    rec.cost = ev.stats.cost;
    rec.model = ev.stats.model;
    rec.prefixHash = extra.prefixHash;
  } else if (ev.role === "assistant_final") {
    // assistant_final without stats (shouldn't happen in the live loop but
    // might in test fixtures) — still persist model + prefix for continuity.
    rec.model = extra.model;
    rec.prefixHash = extra.prefixHash;
  }
  return rec;
}

/**
 * Append a record to an open write stream. Caller owns the stream lifecycle.
 */
export function writeRecord(stream: WriteStream, record: TranscriptRecord): void {
  stream.write(`${JSON.stringify(record)}\n`);
}

/**
 * Write a _meta line to an open write stream. Call exactly once, at the top.
 */
export function writeMeta(stream: WriteStream, meta: TranscriptMeta): void {
  const line: MetaLine = { role: "_meta", meta };
  stream.write(`${JSON.stringify(line)}\n`);
}

/**
 * Convenience: open a stream, write meta, return stream.
 */
export function openTranscriptFile(path: string, meta: TranscriptMeta): WriteStream {
  const stream = createWriteStream(path, { flags: "a" });
  writeMeta(stream, meta);
  return stream;
}

/** Tolerant: empty / malformed lines skipped, missing optionals OK — live chats may be mid-write. */
export function readTranscript(path: string): ReadTranscriptResult {
  const raw = readFileSync(path, "utf8");
  return parseTranscript(raw);
}

export function parseTranscript(raw: string): ReadTranscriptResult {
  const out: ReadTranscriptResult = { meta: null, records: [] };
  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    let obj: unknown;
    try {
      obj = JSON.parse(trimmed);
    } catch {
      continue;
    }
    if (!obj || typeof obj !== "object") continue;
    const rec = obj as Record<string, unknown>;
    if (rec.role === "_meta" && rec.meta && typeof rec.meta === "object") {
      out.meta = rec.meta as TranscriptMeta;
      continue;
    }
    if (
      typeof rec.ts === "string" &&
      typeof rec.turn === "number" &&
      typeof rec.role === "string" &&
      typeof rec.content === "string"
    ) {
      out.records.push(rec as unknown as TranscriptRecord);
    }
  }
  return out;
}
