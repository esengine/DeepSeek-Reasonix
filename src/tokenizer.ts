/** Encode-only DeepSeek V3 tokenizer port; ~3% drift vs API (chat-template framing not replayed). */

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

/** Doesn't add chat-template framing overhead; under-counts ~3-6% vs real `prompt_tokens`. */
export function estimateConversationTokens(
  messages: Array<{ content?: string | null; tool_calls?: unknown }>,
): number {
  let total = 0;
  for (const m of messages) {
    if (typeof m.content === "string" && m.content) {
      total += countTokens(m.content);
    }
    // Tool-call arguments are serialized as JSON in the prompt by the
    // chat template; their bytes WILL count upstream, so we count
    // them too. Stringify-once is cheap relative to the tokenize.
    if (m.tool_calls && Array.isArray(m.tool_calls) && m.tool_calls.length > 0) {
      total += countTokens(JSON.stringify(m.tool_calls));
    }
  }
  return total;
}

/** Tool specs ride in a separate request blob; must be counted separately for an accurate preflight. */
export function estimateRequestTokens(
  messages: Array<{ content?: string | null; tool_calls?: unknown }>,
  toolSpecs?: ReadonlyArray<unknown> | null,
): number {
  let total = estimateConversationTokens(messages);
  if (toolSpecs && toolSpecs.length > 0) {
    total += countTokens(JSON.stringify(toolSpecs));
  }
  return total;
}

/** Exposed for tests — resets the lazy-load singleton. */
export function _resetForTests(): void {
  cached = null;
}
