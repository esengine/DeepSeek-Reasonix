import { truncateForModel, truncateForModelByTokens } from "../mcp/registry.js";
import { countTokens } from "../tokenizer.js";
import type { ChatMessage } from "../types.js";

/** UI progress feedback only — NOT a dispatch gate. */
export function looksLikeCompleteJson(s: string): boolean {
  if (!s || !s.trim()) return false;
  try {
    JSON.parse(s);
    return true;
  } catch {
    return false;
  }
}

/** Tool-role only — truncating user prompts would corrupt authored intent. */
export function shrinkOversizedToolResults(
  messages: ChatMessage[],
  maxChars: number,
): { messages: ChatMessage[]; healedCount: number; healedFrom: number } {
  let healedCount = 0;
  let healedFrom = 0;
  const out = messages.map((msg) => {
    if (msg.role !== "tool") return msg;
    const content = typeof msg.content === "string" ? msg.content : "";
    if (content.length <= maxChars) return msg;
    healedCount += 1;
    healedFrom += content.length;
    return { ...msg, content: truncateForModel(content, maxChars) };
  });
  return { messages: out, healedCount, healedFrom };
}

/** Token-cap variant — char cap would let CJK slip past at 2× the intended token cost. */
export function shrinkOversizedToolResultsByTokens(
  messages: ChatMessage[],
  maxTokens: number,
): {
  messages: ChatMessage[];
  healedCount: number;
  tokensSaved: number;
  charsSaved: number;
} {
  let healedCount = 0;
  let tokensSaved = 0;
  let charsSaved = 0;
  const out = messages.map((msg) => {
    if (msg.role !== "tool") return msg;
    const content = typeof msg.content === "string" ? msg.content : "";
    // length ≤ maxTokens ⇒ tokens ≤ maxTokens — skip the per-message tokenize.
    if (content.length <= maxTokens) return msg;
    const beforeTokens = countTokens(content);
    if (beforeTokens <= maxTokens) return msg;
    const truncated = truncateForModelByTokens(content, maxTokens);
    const afterTokens = countTokens(truncated);
    healedCount += 1;
    tokensSaved += Math.max(0, beforeTokens - afterTokens);
    charsSaved += Math.max(0, content.length - truncated.length);
    return { ...msg, content: truncated };
  });
  return { messages: out, healedCount, tokensSaved, charsSaved };
}

/** Caller must gate on paired tool_calls — in-flight calls would crash mid-turn. */
export function shrinkOversizedToolCallArgsByTokens(
  messages: ChatMessage[],
  maxTokens: number,
): {
  messages: ChatMessage[];
  healedCount: number;
  tokensSaved: number;
  charsSaved: number;
} {
  let healedCount = 0;
  let tokensSaved = 0;
  let charsSaved = 0;
  const out = messages.map((msg) => {
    if (msg.role !== "assistant" || !Array.isArray(msg.tool_calls)) return msg;
    let changed = false;
    const newCalls = msg.tool_calls.map((call) => {
      const args = call.function?.arguments;
      if (typeof args !== "string" || args.length <= maxTokens) return call;
      const beforeTokens = countTokens(args);
      if (beforeTokens <= maxTokens) return call;
      const shrunk = shrinkJsonLongStrings(args);
      const afterTokens = countTokens(shrunk);
      // Many-short-strings payloads can come back marginally larger — only swap on real saving.
      if (afterTokens >= beforeTokens) return call;
      changed = true;
      healedCount += 1;
      tokensSaved += beforeTokens - afterTokens;
      charsSaved += args.length - shrunk.length;
      return { ...call, function: { ...call.function, arguments: shrunk } };
    });
    if (!changed) return msg;
    return { ...msg, tool_calls: newCalls };
  });
  return { messages: out, healedCount, tokensSaved, charsSaved };
}

/** Keeps short keys/values (paths, ids) verbatim; only long string values get a marker. */
function shrinkJsonLongStrings(jsonStr: string): string {
  let parsed: unknown;
  try {
    parsed = JSON.parse(jsonStr);
  } catch {
    const head = jsonStr.slice(0, 200);
    return `${head}…[shrunk: ${jsonStr.length} chars, unparsed]`;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return jsonStr;
  }
  const LONG_THRESHOLD = 300;
  const input = parsed as Record<string, unknown>;
  const output: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(input)) {
    if (typeof v === "string" && v.length > LONG_THRESHOLD) {
      const newlines = v.match(/\n/g)?.length ?? 0;
      output[k] =
        `[…shrunk: ${v.length} chars, ${newlines} lines — tool already responded, see result]`;
    } else {
      output[k] = v;
    }
  }
  return JSON.stringify(output);
}
