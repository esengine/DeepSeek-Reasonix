import { countTokens, countTokensBounded } from "../tokenizer.js";
import { ToolRegistry } from "../tools.js";
import type { JSONSchema } from "../types.js";
import type { McpClient } from "./client.js";
import { LatencyTracker, type SlowEvent } from "./latency.js";
import type { CallToolResult, McpContentBlock, McpTool } from "./types.js";

export interface BridgeOptions {
  /** Prefix for tool names — disambiguates collisions when bridging multiple servers. */
  namePrefix?: string;
  /** Registry to populate. Creates a fresh one if omitted. */
  registry?: ToolRegistry;
  /** Auto-flatten deep schemas (Pillar 3). Defaults to the registry's own default (true). */
  autoFlatten?: boolean;
  /** Cap on tool result chars; head+tail truncation. Floor against context-poisoning oversized reads. */
  maxResultChars?: number;
  /** Absent → no `_meta.progressToken` sent and server won't emit progress. */
  onProgress?: (info: {
    toolName: string;
    progress: number;
    total?: number;
    message?: string;
  }) => void;
  /** Server name used to tag latency samples + slow events. Falls through to namePrefix without trailing `_`. */
  serverName?: string;
  /** p95 cutoff in ms before a slow event fires — defaults to 4000. */
  slowThresholdMs?: number;
  /** Fired exactly when the per-server p95 transitions over `slowThresholdMs`. */
  onSlow?: (ev: SlowEvent) => void;
  /** Indirection so reconnect can swap the underlying client without re-registering tools. */
  host?: McpClientHost;
  /** Awaited before each `callTool` — resolves on `connected`, rejects on `failed`, caps via `readyTimeoutMs`. */
  ready?: Promise<void>;
  /** How long to wait on `ready` before failing the dispatch. Default 30_000ms. */
  readyTimeoutMs?: number;
}

/** Mutable holder so `/mcp reconnect` can swap the underlying client without re-bridging tools. */
export interface McpClientHost {
  client: McpClient;
}

export const DEFAULT_MAX_RESULT_CHARS = 32_000;

/** ~6% of DeepSeek V3 context. Char cap alone fails on CJK (~1 char/token). */
export const DEFAULT_MAX_RESULT_TOKENS = 8_000;

/** Default per-call wait before failing if the server is still handshaking. */
export const DEFAULT_READY_TIMEOUT_MS = 30_000;

export interface BridgeResult {
  registry: ToolRegistry;
  /** Names actually registered (may differ from MCP names when a prefix is applied). */
  registeredNames: string[];
  /** Names the server listed but the bridge skipped (e.g. invalid schemas). */
  skipped: Array<{ name: string; reason: string }>;
}

/** Resolved bridge environment that `registerSingleMcpTool` needs. Stored on summaries so reconnect can append new tools later. */
export interface BridgeEnv {
  registry: ToolRegistry;
  host: McpClientHost;
  prefix: string;
  maxResultChars: number;
  tracker: LatencyTracker | null;
  onProgress?: BridgeOptions["onProgress"];
  /** Optional readiness gate awaited before each `callTool` dispatch. */
  ready?: Promise<void>;
  /** Timeout for waiting on `ready` — milliseconds. Defaults to DEFAULT_READY_TIMEOUT_MS. */
  readyTimeoutMs?: number;
  /** Server name surfaced in timeout errors. Defaults to the prefix or "anon". */
  serverName?: string;
}

/** Register one MCP tool's bridged closure into the registry. Returns the registered name (or "" if skipped). */
export function registerSingleMcpTool(mcpTool: McpTool, env: BridgeEnv): string {
  const stableTool = canonicalizeMcpToolForCache(mcpTool);
  if (!stableTool.name) return "";
  const registeredName = `${env.prefix}${stableTool.name}`;
  env.registry.register({
    name: registeredName,
    description: stableTool.description ?? "",
    parameters: stableTool.inputSchema as JSONSchema,
    fn: async (args: Record<string, unknown>, ctx) => {
      if (env.ready) {
        await waitForReady(
          env.ready,
          env.readyTimeoutMs ?? DEFAULT_READY_TIMEOUT_MS,
          env.serverName ?? (env.prefix.replace(/_$/, "") || "anon"),
          ctx?.signal,
        );
      }
      const t0 = env.tracker ? Date.now() : 0;
      // Resolve client at call time via the host indirection so `/mcp reconnect`
      // can swap a fresh client in without re-bridging tools.
      const live = env.host.client;
      const toolResult = await live.callTool(stableTool.name, args, {
        onProgress: env.onProgress
          ? (info) => env.onProgress!({ toolName: registeredName, ...info })
          : undefined,
        signal: ctx?.signal,
      });
      if (env.tracker) env.tracker.record(Date.now() - t0);
      return flattenMcpResult(toolResult, { maxChars: env.maxResultChars });
    },
  });
  return registeredName;
}

async function waitForReady(
  ready: Promise<void>,
  timeoutMs: number,
  serverName: string,
  signal: AbortSignal | undefined,
): Promise<void> {
  let settled = false;
  let timer: NodeJS.Timeout | undefined;
  let onAbort: (() => void) | undefined;
  try {
    await new Promise<void>((resolve, reject) => {
      ready.then(
        () => {
          if (settled) return;
          settled = true;
          resolve();
        },
        (err) => {
          if (settled) return;
          settled = true;
          reject(err instanceof Error ? err : new Error(String(err)));
        },
      );
      if (timeoutMs > 0) {
        timer = setTimeout(() => {
          if (settled) return;
          settled = true;
          reject(
            new Error(
              `MCP server "${serverName}" still handshaking after ${timeoutMs}ms — try /mcp reconnect or check the server logs.`,
            ),
          );
        }, timeoutMs);
      }
      if (signal) {
        if (signal.aborted) {
          if (settled) return;
          settled = true;
          reject(new Error("aborted"));
          return;
        }
        onAbort = () => {
          if (settled) return;
          settled = true;
          reject(new Error("aborted"));
        };
        signal.addEventListener("abort", onAbort, { once: true });
      }
    });
  } finally {
    if (timer) clearTimeout(timer);
    if (signal && onAbort) signal.removeEventListener("abort", onAbort);
  }
}

export async function bridgeMcpTools(
  client: McpClient,
  opts: BridgeOptions = {},
): Promise<BridgeResult & { env: BridgeEnv }> {
  const registry = opts.registry ?? new ToolRegistry({ autoFlatten: opts.autoFlatten });
  const prefix = opts.namePrefix ?? "";
  const maxResultChars = opts.maxResultChars ?? DEFAULT_MAX_RESULT_CHARS;
  const result: BridgeResult = { registry, registeredNames: [], skipped: [] };

  const serverName = opts.serverName ?? prefix.replace(/_$/, "") ?? "anon";
  const tracker = opts.onSlow
    ? new LatencyTracker(serverName, { thresholdMs: opts.slowThresholdMs, onSlow: opts.onSlow })
    : null;
  // Synthesize a host on the fly when the caller didn't provide one. Older
  // callers (tests, single-shot non-reconnectable bridges) get the live
  // `client` reference frozen in; reconnect-aware callers pass their own
  // mutable host.
  const host: McpClientHost = opts.host ?? { client };
  const env: BridgeEnv = {
    registry,
    host,
    prefix,
    maxResultChars,
    tracker,
    onProgress: opts.onProgress,
    ready: opts.ready,
    readyTimeoutMs: opts.readyTimeoutMs ?? DEFAULT_READY_TIMEOUT_MS,
    serverName,
  };
  const listed = await client.listTools();
  const stableTools = listed.tools
    .map((tool) => canonicalizeMcpToolForCache(tool))
    .sort((a, b) => `${prefix}${a.name}`.localeCompare(`${prefix}${b.name}`));
  for (const mcpTool of stableTools) {
    if (!mcpTool.name) {
      result.skipped.push({ name: "?", reason: "empty tool name" });
      continue;
    }
    const registeredName = registerSingleMcpTool(mcpTool, env);
    if (registeredName) result.registeredNames.push(registeredName);
  }
  return { ...result, env };
}

export function canonicalizeMcpToolForCache(tool: McpTool): McpTool {
  return {
    ...tool,
    inputSchema: canonicalizeSchemaForCache(tool.inputSchema) as McpTool["inputSchema"],
  };
}

const SET_LIKE_SCHEMA_ARRAY_KEYS = new Set(["required", "dependentRequired"]);

export function canonicalizeSchemaForCache(value: unknown, parentKey?: string): unknown {
  if (Array.isArray(value)) {
    const mapped = value.map((item) => canonicalizeSchemaForCache(item));
    if (parentKey && SET_LIKE_SCHEMA_ARRAY_KEYS.has(parentKey) && mapped.every(isScalar)) {
      return [...mapped].sort((a, b) => String(a).localeCompare(String(b)));
    }
    return mapped;
  }
  if (!value || typeof value !== "object") return value;
  if (parentKey === "dependentRequired") {
    const out: Record<string, unknown> = {};
    for (const key of Object.keys(value as Record<string, unknown>).sort()) {
      const arr = (value as Record<string, unknown>)[key];
      out[key] =
        Array.isArray(arr) && arr.every(isScalar)
          ? [...(arr as unknown[])].sort((a, b) => String(a).localeCompare(String(b)))
          : canonicalizeSchemaForCache(arr, key);
    }
    return out;
  }
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(value as Record<string, unknown>).sort()) {
    out[key] = canonicalizeSchemaForCache((value as Record<string, unknown>)[key], key);
  }
  return out;
}

function isScalar(value: unknown): boolean {
  return value === null || ["string", "number", "boolean"].includes(typeof value);
}

export interface FlattenOptions {
  /** Cap the flattened string at this many characters. Default: no cap. */
  maxChars?: number;
}

export function flattenMcpResult(result: CallToolResult, opts: FlattenOptions = {}): string {
  validateResultShape(result);
  const parts = result.content.map(blockToString);
  const joined = parts.join("\n").trim();
  const prefixed = result.isError ? `ERROR: ${joined || "(no error message from server)"}` : joined;
  return opts.maxChars ? truncateForModel(prefixed, opts.maxChars) : prefixed;
}

/** Runtime schema check — MCP server responses cross a network boundary and the TypeScript types are compile-time only. */
function validateResultShape(result: CallToolResult): void {
  if (typeof result !== "object" || !result)
    throw new Error(`MCP server returned non-object result: ${typeof result}`);
  const { content, isError: _isError } = result as { content: unknown; isError?: unknown };
  if (!Array.isArray(content))
    throw new Error(`MCP server returned result with non-array content: ${typeof content}`);
  for (let i = 0; i < content.length; i++) {
    const block = content[i] as Record<string, unknown> | null | undefined;
    if (typeof block !== "object" || !block)
      throw new Error(`MCP server returned result.content[${i}] is not an object`);
    if (block.type !== "text" && block.type !== "image")
      throw new Error(
        `MCP server returned result.content[${i}] with unknown type ${JSON.stringify(block.type)}`,
      );
    if (block.type === "text" && typeof block.text !== "string")
      throw new Error(
        `MCP server returned result.content[${i}] with non-string text (${typeof block.text})`,
      );
    if (block.type === "image") {
      if (typeof block.data !== "string")
        throw new Error(
          `MCP server returned result.content[${i}] with non-string data (${typeof block.data})`,
        );
      if (typeof block.mimeType !== "string")
        throw new Error(
          `MCP server returned result.content[${i}] with non-string mimeType (${typeof block.mimeType})`,
        );
    }
  }
}

/** Head + 1KB tail so error messages at end of stack traces aren't lost. */
export function truncateForModel(s: string, maxChars: number, extraNote?: string): string {
  if (s.length <= maxChars) return s;
  const tailBudget = Math.min(1024, Math.floor(maxChars * 0.1));
  const headBudget = Math.max(0, maxChars - tailBudget);
  const head = sliceAlignedToCodepoint(s, headBudget);
  const tail = sliceSuffixAlignedToCodepoint(s, tailBudget);
  const dropped = s.length - head.length - tail.length;
  const note = extraNote ? ` — ${extraNote}` : "";
  return `${head}\n\n[…truncated ${dropped} chars — raise BridgeOptions.maxResultChars, or call the tool with a narrower scope (filter, head, pagination)${note}…]\n\n${tail}`;
}

/** Slicing inside a UTF-16 surrogate pair (emoji, supplementary CJK) leaves a lone surrogate
 *  that crashes downstream JSON parsers (issue #1970). Trim one code unit back if needed. */
function sliceAlignedToCodepoint(s: string, end: number): string {
  if (end <= 0) return "";
  if (end >= s.length) return s;
  const last = s.charCodeAt(end - 1);
  if (last >= 0xd800 && last <= 0xdbff) return s.slice(0, end - 1);
  return s.slice(0, end);
}

function sliceSuffixAlignedToCodepoint(s: string, len: number): string {
  if (len <= 0) return "";
  if (len >= s.length) return s;
  const start = s.length - len;
  const first = s.charCodeAt(start);
  if (first >= 0xdc00 && first <= 0xdfff) return s.slice(start + 1);
  return s.slice(start);
}

/** Never tokenizes full input — pathological repetitive text (`AAAA…`) costs 30s+ on the pure-TS BPE port. */
export function truncateForModelByTokens(s: string, maxTokens: number, extraNote?: string): string {
  if (maxTokens <= 0) return "";
  if (s.length <= maxTokens) return s;
  // Sample-based estimate: only ever tokenizes 2KB of head+tail regardless
  // of input size. If a healthy safety margin still puts us under budget,
  // skip the precise check — a few-percent under-truncation is far cheaper
  // than tokenizing every fat tool result.
  if (s.length <= maxTokens * 4) {
    const est = countTokensBounded(s);
    if (Math.ceil(est * 1.15) <= maxTokens) return s;
    if (est <= maxTokens) {
      const tokens = countTokens(s);
      if (tokens <= maxTokens) return s;
    }
  }

  const markerOverhead = 48; // rough token cost of the truncation marker
  const contentBudget = Math.max(0, maxTokens - markerOverhead);
  const tailBudget = Math.min(256, Math.floor(contentBudget * 0.1));
  const headBudget = Math.max(0, contentBudget - tailBudget);

  const head = sizePrefixToTokens(s, headBudget);
  const tail = sizeSuffixToTokens(s, tailBudget);
  const droppedChars = s.length - head.length - tail.length;
  // Estimate dropped tokens from the per-slice char/token ratio we
  // already measured, rather than paying another full-string tokenize.
  // The marker says "~N tokens" so the ≤10% slop is visible to readers.
  const headTokens = head ? countTokens(head) : 0;
  const tailTokens = tail ? countTokens(tail) : 0;
  const sampleChars = head.length + tail.length;
  const sampleTokens = headTokens + tailTokens;
  const ratio = sampleChars > 0 ? sampleTokens / sampleChars : 0.3;
  const estTotalTokens = Math.ceil(s.length * ratio);
  const droppedTokens = Math.max(0, estTotalTokens - sampleTokens);
  const note = extraNote ? ` — ${extraNote}` : "";
  return `${head}\n\n[…truncated ~${droppedTokens} tokens (${droppedChars} chars) — raise BridgeOptions.maxResultTokens, or call the tool with a narrower scope (filter, head, pagination)${note}…]\n\n${tail}`;
}

function sizePrefixToTokens(s: string, budget: number): string {
  if (budget <= 0 || s.length === 0) return "";
  // Optimistic starting size: assume ~4 chars/token (English/code
  // average). If the content is denser (CJK ~1 char/token), the first
  // tokenize will show we're over and we shrink.
  let size = Math.min(s.length, budget * 4);
  for (let iter = 0; iter < 6; iter++) {
    if (size <= 0) return "";
    const slice = sliceAlignedToCodepoint(s, size);
    const count = countTokens(slice);
    if (count <= budget) return slice;
    // Shrink by the overshoot fraction plus a small safety margin.
    const next = Math.floor(size * (budget / count) * 0.95);
    if (next >= size) return sliceAlignedToCodepoint(s, Math.max(0, size - 1));
    size = next;
  }
  return sliceAlignedToCodepoint(s, Math.max(0, size));
}

/** Slice `s` from the end to the largest suffix that fits `budget` tokens. */
function sizeSuffixToTokens(s: string, budget: number): string {
  if (budget <= 0 || s.length === 0) return "";
  let size = Math.min(s.length, budget * 4);
  for (let iter = 0; iter < 6; iter++) {
    if (size <= 0) return "";
    const slice = sliceSuffixAlignedToCodepoint(s, size);
    const count = countTokens(slice);
    if (count <= budget) return slice;
    const next = Math.floor(size * (budget / count) * 0.95);
    if (next >= size) return sliceSuffixAlignedToCodepoint(s, Math.max(0, size - 1));
    size = next;
  }
  return sliceSuffixAlignedToCodepoint(s, Math.max(0, size));
}

function blockToString(block: McpContentBlock): string {
  if (block.type === "text") return block.text;
  if (block.type === "image") return `[image ${block.mimeType}, ${block.data.length} chars base64]`;
  // Unknown block type — preserve for diagnostics.
  return `[unknown block: ${JSON.stringify(block)}]`;
}
