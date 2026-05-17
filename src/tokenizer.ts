/** Encode-only DeepSeek V4 tokenizer port. Applies V4 chat template so token count tracks API `prompt_tokens`. */

import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { gunzipSync } from "node:zlib";

interface AddedToken {
  id: number;
  content: string;
  special: boolean;
  normalized: boolean;
}

interface SplitPretokenizer {
  type: "Split";
  pattern: { Regex: string };
  behavior: "Isolated" | "Removed" | string;
  invert: boolean;
}

interface ByteLevelPretokenizer {
  type: "ByteLevel";
  add_prefix_space: boolean;
  trim_offsets: boolean;
  use_regex: boolean;
}

type Pretokenizer = SplitPretokenizer | ByteLevelPretokenizer;

interface TokenizerData {
  added_tokens: AddedToken[];
  pre_tokenizer: {
    type: "Sequence";
    pretokenizers: Pretokenizer[];
  };
  model: {
    type: "BPE";
    vocab: Record<string, number>;
    merges: string[];
  };
}

interface LoadedTokenizer {
  vocab: Record<string, number>;
  mergeRank: Map<string, number>;
  splitRegexes: RegExp[];
  byteToChar: string[];
  /** Non-special added tokens only — special tokens in user text tokenize byte-by-byte (HF default). */
  addedPattern: RegExp | null;
  addedMap: Map<string, number>;
}

/** GPT-2 byte→unicode map; lets byte-level BPE vocab serialize as readable JSON strings. */
function buildByteToChar(): string[] {
  const result: string[] = new Array(256);
  const bs: number[] = [];
  for (let b = 33; b <= 126; b++) bs.push(b);
  for (let b = 161; b <= 172; b++) bs.push(b);
  for (let b = 174; b <= 255; b++) bs.push(b);
  const cs = bs.slice();
  let n = 0;
  for (let b = 0; b < 256; b++) {
    if (!bs.includes(b)) {
      bs.push(b);
      cs.push(256 + n);
      n++;
    }
  }
  for (let i = 0; i < bs.length; i++) {
    result[bs[i]!] = String.fromCodePoint(cs[i]!);
  }
  return result;
}

let cached: LoadedTokenizer | null = null;

/** Two ../data candidates needed: dist/index.js AND dist/cli/index.js resolve to different roots. */
export function resolveDataPath(): string {
  if (process.env.REASONIX_TOKENIZER_PATH) return process.env.REASONIX_TOKENIZER_PATH;
  const candidates: string[] = [];
  try {
    const here = dirname(fileURLToPath(import.meta.url));
    candidates.push(join(here, "..", "data", "deepseek-tokenizer.json.gz"));
    candidates.push(join(here, "..", "..", "data", "deepseek-tokenizer.json.gz"));
  } catch {
    /* import.meta.url unavailable — skip to the package resolution step. */
  }
  try {
    const req = createRequire(import.meta.url);
    candidates.push(
      join(dirname(req.resolve("reasonix/package.json")), "data", "deepseek-tokenizer.json.gz"),
    );
  } catch {
    /* Not installed as `reasonix/` — the earlier candidates still may hit. */
  }
  for (const p of candidates) {
    if (existsSync(p)) return p;
  }
  // Nothing exists — return the first candidate anyway so readFileSync
  // surfaces a concrete path in the ENOENT message (better than silent miss).
  return candidates[0] ?? join(process.cwd(), "data", "deepseek-tokenizer.json.gz");
}

function loadTokenizer(): LoadedTokenizer {
  if (cached) return cached;
  const buf = readFileSync(resolveDataPath());
  const json = gunzipSync(buf).toString("utf8");
  const data = JSON.parse(json) as TokenizerData;

  const mergeRank = new Map<string, number>();
  for (let i = 0; i < data.model.merges.length; i++) {
    mergeRank.set(data.model.merges[i]!, i);
  }

  const splitRegexes: RegExp[] = [];
  for (const p of data.pre_tokenizer.pretokenizers) {
    if (p.type === "Split") {
      // All three Split rules use Isolated — matches become their own
      // pre-tokens and so do the in-between stretches. The ByteLevel
      // stage in the Sequence does no extra splitting here
      // (use_regex:false), so our 3 Split regexes are the whole story.
      splitRegexes.push(new RegExp(p.pattern.Regex, "gu"));
    }
  }

  const addedMap = new Map<string, number>();
  const addedContents: string[] = [];
  for (const t of data.added_tokens) {
    if (!t.special) {
      addedMap.set(t.content, t.id);
      addedContents.push(t.content);
    }
  }
  // Longest-first ensures greedy matching doesn't lose a longer token
  // to a shorter prefix (e.g. `<think>` before `<`).
  addedContents.sort((a, b) => b.length - a.length);
  const addedPattern = addedContents.length
    ? new RegExp(addedContents.map(escapeRegex).join("|"), "g")
    : null;

  cached = {
    vocab: data.model.vocab,
    mergeRank,
    splitRegexes,
    byteToChar: buildByteToChar(),
    addedPattern,
    addedMap,
  };
  return cached;
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function applySplit(chunks: string[], re: RegExp): string[] {
  const out: string[] = [];
  for (const chunk of chunks) {
    if (!chunk) continue;
    // Reset lastIndex — reusing a /g regex across matchAll iterations
    // is safe (matchAll internally advances), but across different
    // input strings we want a clean start.
    re.lastIndex = 0;
    let last = 0;
    for (const m of chunk.matchAll(re)) {
      const idx = m.index ?? 0;
      if (idx > last) out.push(chunk.slice(last, idx));
      if (m[0].length > 0) out.push(m[0]);
      last = idx + m[0].length;
    }
    if (last < chunk.length) out.push(chunk.slice(last));
  }
  return out;
}

/** UTF-8 bytes of `s`, each mapped to its byte-level visible char. */
function byteLevelEncode(s: string, byteToChar: string[]): string {
  const bytes = new TextEncoder().encode(s);
  let out = "";
  for (let i = 0; i < bytes.length; i++) out += byteToChar[bytes[i]!];
  return out;
}

function bpeEncode(piece: string, mergeRank: Map<string, number>): string[] {
  if (piece.length <= 1) return piece ? [piece] : [];
  let word: string[] = Array.from(piece);
  while (true) {
    let bestIdx = -1;
    let bestRank = Number.POSITIVE_INFINITY;
    for (let i = 0; i < word.length - 1; i++) {
      const pair = `${word[i]} ${word[i + 1]}`;
      const rank = mergeRank.get(pair);
      if (rank !== undefined && rank < bestRank) {
        bestRank = rank;
        bestIdx = i;
        if (rank === 0) break; // 0 is already the best possible
      }
    }
    if (bestIdx < 0) break;
    word = [
      ...word.slice(0, bestIdx),
      word[bestIdx]! + word[bestIdx + 1]!,
      ...word.slice(bestIdx + 2),
    ];
    if (word.length === 1) break;
  }
  return word;
}

export function encode(text: string): number[] {
  if (!text) return [];
  const t = loadTokenizer();
  const ids: number[] = [];

  const process = (segment: string) => {
    if (!segment) return;
    let chunks: string[] = [segment];
    for (const re of t.splitRegexes) chunks = applySplit(chunks, re);
    for (const chunk of chunks) {
      if (!chunk) continue;
      const byteLevel = byteLevelEncode(chunk, t.byteToChar);
      const pieces = bpeEncode(byteLevel, t.mergeRank);
      for (const p of pieces) {
        const id = t.vocab[p];
        // If not in vocab we silently skip: shouldn't happen for
        // byte-level BPE (every single byte has its own vocab entry),
        // but if a future tokenizer update breaks that invariant we'd
        // rather under-count than throw from a UI gauge.
        if (id !== undefined) ids.push(id);
      }
    }
  };

  if (t.addedPattern) {
    t.addedPattern.lastIndex = 0;
    let last = 0;
    for (const m of text.matchAll(t.addedPattern)) {
      const idx = m.index ?? 0;
      if (idx > last) process(text.slice(last, idx));
      const id = t.addedMap.get(m[0]);
      if (id !== undefined) ids.push(id);
      last = idx + m[0].length;
    }
    if (last < text.length) process(text.slice(last));
  } else {
    process(text);
  }
  return ids;
}

export function countTokens(text: string): number {
  return encode(text).length;
}

export const DEFAULT_BOUNDED_TOKENIZE_CHARS = 2 * 1024;

export function countTokensBounded(
  text: string,
  maxChars = DEFAULT_BOUNDED_TOKENIZE_CHARS,
): number {
  if (text.length === 0) return 0;
  const cap = Math.floor(maxChars);
  if (cap > 0 && text.length <= cap) return countTokens(text);
  if (cap <= 0) return Math.max(1, Math.ceil(text.length * 0.3));

  const headChars = Math.ceil(cap / 2);
  const tailChars = Math.floor(cap / 2);
  const head = text.slice(0, headChars);
  const tail = tailChars > 0 ? text.slice(-tailChars) : "";
  const sampleChars = head.length + tail.length;
  const sampleTokens = countTokens(head) + countTokens(tail);
  const ratio = sampleChars > 0 ? sampleTokens / sampleChars : 0.3;
  return Math.max(1, Math.ceil(text.length * ratio));
}

const BOS = "<｜begin▁of▁sentence｜>";
const EOS = "<｜end▁of▁sentence｜>";
const USER_SP = "<｜User｜>";
const ASSISTANT_SP = "<｜Assistant｜>";
const THINK_START = "<think>";
const THINK_END = "</think>";

const DSML = "｜DSML｜";
const TC_BEGIN = `<${DSML}tool_calls>`;
const TC_END = `</${DSML}tool_calls>`;
const INVOKE_BEGIN = `<${DSML}invoke name="`;
const INVOKE_END = `</${DSML}invoke>`;
const PARAM_TEMPLATE = `<${DSML}parameter name="{key}" string="{is_str}">{value}</${DSML}parameter>`;
const TOOL_RESULT_TEMPLATE = "<tool_result>{content}</tool_result>";

/** Keyed by `ImmutablePrefix._toolSpecs` identity — stable for the prefix's lifetime. */
const toolsTemplateCache = new WeakMap<ReadonlyArray<unknown>, string>();

function renderTools(tools: ReadonlyArray<unknown>): string {
  const cached = toolsTemplateCache.get(tools);
  if (cached !== undefined) return cached;

  const schemas = tools
    .map((t) => {
      const fn = (t as { function?: unknown }).function ?? t;
      return JSON.stringify(fn);
    })
    .join("\n");
  const rendered = `## Tools\n\nYou have access to a set of tools to help answer the user's question. You can invoke tools by writing a \"<${DSML}tool_calls>" block like the following:\n\n<${DSML}tool_calls>\n<${DSML}invoke name="$TOOL_NAME">\n<${DSML}parameter name="$PARAMETER_NAME" string="true|false">$PARAMETER_VALUE</${DSML}parameter>\n...\n</${DSML}invoke>\n<${DSML}invoke name="$TOOL_NAME2">\n...\n</${DSML}invoke>\n</${DSML}tool_calls>\n\nString parameters should be specified as is and set \`string="true"\`. For all other types (numbers, booleans, arrays, objects), pass the value in JSON format and set \`string="false"\`.\n\nIf thinking_mode is enabled (triggered by ${THINK_START}), you MUST output your complete reasoning inside ${THINK_START}...${THINK_END} BEFORE any tool calls or final response.\n\nOtherwise, output directly after ${THINK_END} with tool calls or final response.\n\n### Available Tool Schemas\n\n${schemas}\n\nYou MUST strictly follow the above defined tool name and parameter schemas to invoke tool calls.`;

  toolsTemplateCache.set(tools, rendered);
  return rendered;
}

interface ToolCall {
  function?: { name?: string; arguments?: string };
  [k: string]: unknown;
}

interface V4Message {
  role?: string;
  content?: string | null;
  tool_calls?: ToolCall[];
  tool_call_id?: string;
  reasoning_content?: string | null;
  _toolBlocks?: string[];
  _textParts?: string[];
}

function encodeArgumentsToDsml(argsJson: string): string {
  let args: Record<string, unknown>;
  try {
    args = JSON.parse(argsJson) as Record<string, unknown>;
  } catch {
    args = { arguments: argsJson };
  }
  return Object.entries(args)
    .map(([k, v]) =>
      PARAM_TEMPLATE.replace("{key}", k)
        .replace("{is_str}", typeof v === "string" ? "true" : "false")
        .replace("{value}", typeof v === "string" ? v : JSON.stringify(v)),
    )
    .join("\n");
}

function renderToolCallsDsml(toolCalls: ToolCall[]): string {
  const invokes = toolCalls
    .map((tc) => {
      const name = tc.function?.name ?? "";
      const argsJson = tc.function?.arguments ?? "{}";
      return `${INVOKE_BEGIN + name}">\n${encodeArgumentsToDsml(argsJson)}\n${INVOKE_END}`;
    })
    .join("\n");
  return `\n\n${TC_BEGIN}\n${invokes}\n${TC_END}`;
}

function mergeToolMessages(messages: V4Message[]): V4Message[] {
  const merged: V4Message[] = [];
  for (const msg of messages) {
    const role = msg.role ?? "user";
    if (role === "tool") {
      const toolBlock = TOOL_RESULT_TEMPLATE.replace("{content}", msg.content ?? "");
      const last = merged[merged.length - 1];
      if (
        last &&
        last.role === "user" &&
        Array.isArray(last._toolBlocks) &&
        Array.isArray(last._textParts)
      ) {
        last._toolBlocks.push(toolBlock);
        last.content = `${last._textParts.join("\n\n")}\n\n${last._toolBlocks.join("\n")}`.replace(
          /^\n\n/,
          "",
        );
      } else {
        merged.push({
          role: "user",
          content: toolBlock,
          _textParts: [],
          _toolBlocks: [toolBlock],
        });
      }
    } else if (role === "user") {
      const text = msg.content ?? "";
      const last = merged[merged.length - 1];
      if (
        last &&
        last.role === "user" &&
        Array.isArray(last._toolBlocks) &&
        Array.isArray(last._textParts)
      ) {
        last._textParts.push(text);
        last.content =
          `${last._textParts.join("\n\n")}\n\n${last._toolBlocks.join("\n\n")}`.replace(
            /^\n\n/,
            "",
          );
      } else {
        merged.push({
          ...msg,
          role: "user",
          content: text,
          _textParts: [text],
          _toolBlocks: [],
        });
      }
    } else {
      merged.push({ ...msg });
    }
  }
  for (const m of merged) {
    m._textParts = undefined;
    m._toolBlocks = undefined;
  }
  return merged;
}

/** Drop `reasoning_content` from assistant messages before the last user/developer message. Matches Python `_drop_thinking_messages`. */
function dropThinkingMessages(messages: V4Message[]): V4Message[] {
  let lastUserIdx = -1;
  for (let i = messages.length - 1; i >= 0; i--) {
    const role = messages[i]!.role;
    if (role === "user" || role === "developer") {
      lastUserIdx = i;
      break;
    }
  }
  if (lastUserIdx < 0) return messages;

  // Match Python `_drop_thinking_messages`:
  //   - developer messages before lastUserIdx are dropped entirely
  //   - assistant messages before lastUserIdx keep content & tool_calls
  //     but have reasoning_content stripped
  const result: V4Message[] = [];
  for (let i = 0; i < messages.length; i++) {
    const msg = messages[i]!;
    if (i < lastUserIdx && msg.role === "developer") continue;
    if (i < lastUserIdx && msg.role === "assistant") {
      result.push({ ...msg, reasoning_content: null });
    } else {
      result.push(msg);
    }
  }
  return result;
}

/** Apply DeepSeek V4 chat template. Matches `encoding_dsv4.py`: tool results merged into user messages, assistant tool_calls in DSML, generation suffix appended. */
export function formatDeepSeekPrompt(
  messages: Array<{
    role?: string;
    content?: string | null;
    tool_calls?: unknown;
    tool_call_id?: string;
    reasoning_content?: string | null;
  }>,
  drop_thinking = false,
): string {
  if (messages.length === 0) return ASSISTANT_SP + THINK_END;

  let msgs = messages as V4Message[];
  if (drop_thinking) {
    msgs = dropThinkingMessages(msgs);
  }
  const merged = mergeToolMessages(msgs);

  let prompt = BOS;

  for (let i = 0; i < merged.length; i++) {
    const msg = merged[i]!;
    const role = msg.role ?? "user";
    const nextRole = i + 1 < merged.length ? (merged[i + 1]!.role ?? "user") : null;

    if (role === "system") {
      prompt += msg.content ?? "";
    } else if (role === "user") {
      prompt += USER_SP + (msg.content ?? "");
      if (nextRole === "assistant" || nextRole === null) {
        prompt += ASSISTANT_SP + THINK_END;
      }
    } else if (role === "assistant") {
      if (msg.reasoning_content) {
        prompt += THINK_START + msg.reasoning_content + THINK_END;
      }
      if (msg.content) prompt += msg.content;
      const tcs = msg.tool_calls;
      if (Array.isArray(tcs) && tcs.length > 0) {
        prompt += renderToolCallsDsml(tcs);
      }
      prompt += EOS;
    }
  }

  return prompt;
}

/** Token-count the FULL conversation as the API would see it: wraps messages in V4 chat template, then encodes once. */
export function estimateConversationTokens(
  messages: Array<{
    role?: string;
    content?: string | null;
    tool_calls?: unknown;
    tool_call_id?: string;
    reasoning_content?: string | null;
  }>,
  drop_thinking = false,
): number {
  if (messages.length === 0) return 0;
  return countTokensBounded(formatDeepSeekPrompt(messages, drop_thinking));
}

/** Total request tokens (messages + tool specs) as the API counts them. Tool specs rendered via V4 TOOLS_TEMPLATE and added to message token count. */
export function estimateRequestTokens(
  messages: Array<{
    role?: string;
    content?: string | null;
    tool_calls?: unknown;
    tool_call_id?: string;
    reasoning_content?: string | null;
  }>,
  toolSpecs?: ReadonlyArray<unknown> | null,
  drop_thinking = false,
): number {
  let total = estimateConversationTokens(messages, drop_thinking);
  if (toolSpecs && toolSpecs.length > 0) {
    total += countTokensBounded(renderTools(toolSpecs));
  }
  return total;
}

/** Exposed for tests — resets the lazy-load singleton. */
export function _resetForTests(): void {
  cached = null;
}
