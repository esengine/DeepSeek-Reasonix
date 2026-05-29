import { createHash } from "node:crypto";
import type { Usage } from "../client.js";
import type { ChatMessage, ToolSpec } from "../types.js";
import { cacheSavingsUsd } from "./stats.js";

export const CACHE_DIAGNOSTICS_MAX_ENTRIES = 50;

export type CacheMissReason =
  | "no-miss"
  | "cold-start"
  | "system-prompt-changed"
  | "tool-list-changed"
  | "tool-schema-or-order-changed"
  | "mcp-tool-hot-add"
  | "memory-or-skill-changed"
  | "unknown";

export interface PrefixDiagnosticHashes {
  prefixHash: string;
  systemHash: string;
  toolSpecsHash: string;
  fewShotsHash: string;
  toolCount: number;
  toolNames: string[];
}

export interface CacheDiagnosticEntry extends PrefixDiagnosticHashes {
  ts: number;
  turn: number;
  model: string;
  inputTokens: number;
  cachedTokens: number;
  cacheMissTokens: number;
  cacheHitRate: number;
  estimatedCostUsd: number;
  savedCostUsd: number;
  missReason: CacheMissReason;
  missReasonDetail: string;
  /** DeepSeek reports token counts only; miss reason is inferred locally from stable prefix evidence. */
  inferred: true;
}

export interface CacheDiagnosticInput {
  turn: number;
  model: string;
  usage: Usage;
  estimatedCostUsd: number;
  prefix: PrefixDiagnosticHashes;
  previous?: CacheDiagnosticEntry | null;
  now?: number;
}

export function stableHash(value: unknown): string {
  return createHash("sha256").update(JSON.stringify(value)).digest("hex").slice(0, 16);
}

export function prefixDiagnosticHashes(opts: {
  system: string;
  toolSpecs: readonly ToolSpec[];
  fewShots: readonly ChatMessage[];
}): PrefixDiagnosticHashes {
  const toolNames = opts.toolSpecs.map((spec) => spec.function?.name ?? "").filter(Boolean);
  return {
    prefixHash: stableHash({
      system: opts.system,
      tools: opts.toolSpecs,
      shots: opts.fewShots,
    }),
    systemHash: stableHash(opts.system),
    toolSpecsHash: stableHash(opts.toolSpecs),
    fewShotsHash: stableHash(opts.fewShots),
    toolCount: opts.toolSpecs.length,
    toolNames,
  };
}

export function buildCacheDiagnostic(input: CacheDiagnosticInput): CacheDiagnosticEntry {
  const usage = input.usage;
  const { reason, detail } = inferCacheMissReason(input.previous ?? null, input.prefix, usage);
  return {
    ...input.prefix,
    ts: input.now ?? Date.now(),
    turn: input.turn,
    model: input.model,
    inputTokens: usage.promptTokens,
    cachedTokens: usage.promptCacheHitTokens,
    cacheMissTokens: usage.promptCacheMissTokens,
    cacheHitRate: usage.cacheHitRatio,
    estimatedCostUsd: input.estimatedCostUsd,
    savedCostUsd: cacheSavingsUsd(input.model, usage.promptCacheHitTokens),
    missReason: reason,
    missReasonDetail: detail,
    inferred: true,
  };
}

export function appendCacheDiagnostic(
  existing: readonly CacheDiagnosticEntry[] | undefined,
  entry: CacheDiagnosticEntry,
  limit = CACHE_DIAGNOSTICS_MAX_ENTRIES,
): CacheDiagnosticEntry[] {
  const safeExisting = Array.isArray(existing) ? existing.filter(isCacheDiagnosticEntry) : [];
  const next = [...safeExisting, entry];
  return next.slice(Math.max(0, next.length - limit));
}

export function latestCacheDiagnostic(
  entries: readonly CacheDiagnosticEntry[] | undefined,
): CacheDiagnosticEntry | null {
  if (!Array.isArray(entries)) return null;
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i];
    if (isCacheDiagnosticEntry(entry)) return entry;
  }
  return null;
}

export function inferCacheMissReason(
  previous: CacheDiagnosticEntry | null,
  current: PrefixDiagnosticHashes,
  usage: Pick<Usage, "promptCacheMissTokens">,
): { reason: CacheMissReason; detail: string } {
  if (usage.promptCacheMissTokens <= 0) {
    return { reason: "no-miss", detail: "No prompt-side cache miss tokens were reported." };
  }
  if (!previous) {
    return {
      reason: "cold-start",
      detail: "No previous cache evidence exists for this session.",
    };
  }
  if (previous.systemHash !== current.systemHash) {
    return {
      reason: "system-prompt-changed",
      detail: `systemHash ${short(previous.systemHash)} -> ${short(current.systemHash)}`,
    };
  }
  if (previous.fewShotsHash !== current.fewShotsHash) {
    return {
      reason: "memory-or-skill-changed",
      detail: `fewShotsHash ${short(previous.fewShotsHash)} -> ${short(current.fewShotsHash)}`,
    };
  }
  if (previous.toolSpecsHash !== current.toolSpecsHash) {
    const oldNames = previous.toolNames;
    const newNames = current.toolNames;
    const added = newNames.filter((name) => !oldNames.includes(name));
    const removed = oldNames.filter((name) => !newNames.includes(name));
    if (added.length > 0 && removed.length === 0 && looksLikeMcpToolNames(added)) {
      return {
        reason: "mcp-tool-hot-add",
        detail: `MCP-like tool(s) added: ${added.join(", ")}`,
      };
    }
    if (added.length > 0 || removed.length > 0 || previous.toolCount !== current.toolCount) {
      const parts = [];
      if (added.length > 0) parts.push(`added ${added.join(", ")}`);
      if (removed.length > 0) parts.push(`removed ${removed.join(", ")}`);
      if (parts.length === 0)
        parts.push(`tool count ${previous.toolCount} -> ${current.toolCount}`);
      return {
        reason: "tool-list-changed",
        detail: parts.join("; "),
      };
    }
    return {
      reason: "tool-schema-or-order-changed",
      detail: `toolSpecsHash ${short(previous.toolSpecsHash)} -> ${short(current.toolSpecsHash)}`,
    };
  }
  if (previous.prefixHash !== current.prefixHash) {
    return {
      reason: "unknown",
      detail: `prefixHash changed (${short(previous.prefixHash)} -> ${short(current.prefixHash)}) but sub-hashes matched.`,
    };
  }
  return {
    reason: "unknown",
    detail:
      "Prefix hashes matched. DeepSeek does not return cache-miss reasons, so this miss is likely due to provider-side cache state, TTL, or prompt bytes outside the immutable prefix.",
  };
}

export function renderCacheMissReport(
  entries: readonly CacheDiagnosticEntry[] | undefined,
  opts: { limit?: number } = {},
): string {
  const valid = Array.isArray(entries) ? entries.filter(isCacheDiagnosticEntry) : [];
  if (valid.length === 0) {
    return [
      "cache miss report",
      "",
      "No cache diagnostics recorded for this session yet.",
      "Run one model turn first. DeepSeek reports hit/miss token counts; Reasonix infers miss reasons locally from prefix hashes.",
    ].join("\n");
  }
  const limit = opts.limit ?? 8;
  const recent = valid.slice(Math.max(0, valid.length - limit));
  const totalCached = valid.reduce((sum, e) => sum + e.cachedTokens, 0);
  const totalMiss = valid.reduce((sum, e) => sum + e.cacheMissTokens, 0);
  const totalInput = totalCached + totalMiss;
  const hitRate = totalInput > 0 ? totalCached / totalInput : 0;
  const saved = valid.reduce((sum, e) => sum + e.savedCostUsd, 0);
  const lines = [
    "cache miss report",
    `turns: ${valid.length} · input: ${totalInput.toLocaleString()} · cached: ${totalCached.toLocaleString()} · hit rate: ${pct(hitRate)} · saved: ${usd(saved)}`,
    "note: DeepSeek does not return a cache-miss reason. Reasonix infers the reason locally from byte-stable prefix evidence.",
    "",
  ];
  for (const entry of recent) {
    lines.push(
      `#${entry.turn} ${entry.model} · input ${entry.inputTokens.toLocaleString()} · cached ${entry.cachedTokens.toLocaleString()} · miss ${entry.cacheMissTokens.toLocaleString()} · hit ${pct(entry.cacheHitRate)} · cost ${usd(entry.estimatedCostUsd)} · saved ${usd(entry.savedCostUsd)}`,
      `  reason: ${entry.missReason} — ${entry.missReasonDetail}`,
      `  prefix: ${short(entry.prefixHash)} · system ${short(entry.systemHash)} · tools ${short(entry.toolSpecsHash)} (${entry.toolCount}) · few-shot ${short(entry.fewShotsHash)}`,
    );
  }
  return lines.join("\n");
}

export function isCacheDiagnosticEntry(value: unknown): value is CacheDiagnosticEntry {
  if (!value || typeof value !== "object") return false;
  const entry = value as Partial<CacheDiagnosticEntry>;
  return (
    typeof entry.ts === "number" &&
    typeof entry.turn === "number" &&
    typeof entry.model === "string" &&
    typeof entry.prefixHash === "string" &&
    typeof entry.systemHash === "string" &&
    typeof entry.toolSpecsHash === "string" &&
    typeof entry.fewShotsHash === "string" &&
    typeof entry.inputTokens === "number" &&
    typeof entry.cachedTokens === "number" &&
    typeof entry.cacheMissTokens === "number" &&
    typeof entry.cacheHitRate === "number" &&
    typeof entry.estimatedCostUsd === "number" &&
    typeof entry.savedCostUsd === "number" &&
    typeof entry.missReason === "string" &&
    typeof entry.missReasonDetail === "string"
  );
}

function looksLikeMcpToolNames(names: readonly string[]): boolean {
  return names.some((name) => name.includes("_") || name.includes("__"));
}

function short(hash: string): string {
  return hash.slice(0, 8);
}

function pct(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

function usd(value: number): string {
  return `$${value < 0.01 ? value.toFixed(6) : value.toFixed(4)}`;
}
