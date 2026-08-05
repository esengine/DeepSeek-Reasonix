const WORD_CHARACTER_RE = /[\p{L}\p{N}_]/u;

export const MAX_SEARCH_MATCHES = 10_000;
export const MAX_REGEX_PATTERN_LENGTH = 256;
export const MAX_REGEX_SOURCE_LENGTH = 2 * 1024 * 1024;
export const REGEX_SEARCH_TIMEOUT_MS = 200;

export interface CodeSearchMatch {
  lineIndex: number;
  start: number;
  end: number;
  absoluteStart: number;
  absoluteEnd: number;
}

export interface CodeSearchResult {
  matches: CodeSearchMatch[];
  truncated: boolean;
}

export type RegexSearchErrorCode =
  | "invalid_pattern"
  | "pattern_too_long"
  | "source_too_large"
  | "zero_length_unsupported"
  | "multiline_unsupported"
  | "timeout"
  | "unavailable";

export interface RegexSearchRequest {
  requestId: number;
  source: string;
  pattern: string;
  caseSensitive: boolean;
  wholeWord: boolean;
  maxMatches: number;
}

export type RegexSearchResponse =
  | {
      requestId: number;
      ok: true;
      result: CodeSearchResult;
    }
  | {
      requestId: number;
      ok: false;
      error: RegexSearchErrorCode;
      detail?: string;
    };

export function findCodeMatches(
  source: string | readonly string[],
  query: string,
  caseSensitive = false,
  wholeWord = false,
  maxMatches = MAX_SEARCH_MATCHES,
): CodeSearchResult {
  if (!query) return emptySearchResult();

  return collectMatches(
    source,
    new RegExp(escapeRegex(query), caseSensitive ? "gu" : "giu"),
    wholeWord,
    maxMatches,
  );
}

export function findRegexCodeMatches(request: RegexSearchRequest): RegexSearchResponse {
  if (request.pattern.length > MAX_REGEX_PATTERN_LENGTH) {
    return {
      requestId: request.requestId,
      ok: false,
      error: "pattern_too_long",
    };
  }
  if (request.source.length > MAX_REGEX_SOURCE_LENGTH) {
    return {
      requestId: request.requestId,
      ok: false,
      error: "source_too_large",
    };
  }
  if (!request.pattern) {
    return {
      requestId: request.requestId,
      ok: true,
      result: emptySearchResult(),
    };
  }

  let pattern: RegExp;
  try {
    // Search anchors should continue to address individual source lines, while
    // the full source is still scanned so newline-spanning matches can be
    // rejected explicitly instead of disappearing silently.
    pattern = new RegExp(request.pattern, request.caseSensitive ? "gmu" : "gimu");
  } catch (error) {
    return {
      requestId: request.requestId,
      ok: false,
      error: "invalid_pattern",
      detail: error instanceof Error ? error.message : String(error),
    };
  }

  const result = collectRegexMatches(
    request.source,
    pattern,
    request.wholeWord,
    Math.max(0, Math.min(request.maxMatches, MAX_SEARCH_MATCHES)),
  );
  if ("error" in result) {
    return {
      requestId: request.requestId,
      ok: false,
      error: result.error,
    };
  }
  return {
    requestId: request.requestId,
    ok: true,
    result,
  };
}

function collectMatches(
  source: string | readonly string[],
  pattern: RegExp,
  wholeWord: boolean,
  maxMatches: number,
  rejectZeroLength?: false,
): CodeSearchResult;
function collectMatches(
  source: string | readonly string[],
  pattern: RegExp,
  wholeWord: boolean,
  maxMatches: number,
  rejectZeroLength: true,
): CodeSearchResult | { error: "zero_length_unsupported" };
function collectMatches(
  source: string | readonly string[],
  pattern: RegExp,
  wholeWord: boolean,
  maxMatches: number,
  rejectZeroLength = false,
): CodeSearchResult | { error: "zero_length_unsupported" } {
  const matches: CodeSearchMatch[] = [];
  const lines = typeof source === "string" ? source.split("\n") : source;
  let absoluteOffset = 0;
  let sawZeroLengthMatch = false;
  let sawNonEmptyMatch = false;

  for (let lineIndex = 0; lineIndex < lines.length; lineIndex += 1) {
    const line = lines[lineIndex];
    pattern.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(line)) !== null) {
      if (match[0].length === 0) {
        sawZeroLengthMatch = true;
        const nextCodePoint = line.codePointAt(pattern.lastIndex);
        pattern.lastIndex += nextCodePoint != null && nextCodePoint > 0xffff ? 2 : 1;
        continue;
      }
      sawNonEmptyMatch = true;

      const start = match.index;
      const end = start + match[0].length;
      const startsInsideWord = start > 0 && isWordCharacter(codePointBefore(line, start));
      const endsInsideWord = end < line.length && isWordCharacter(codePointAt(line, end));
      if (!wholeWord || (!startsInsideWord && !endsInsideWord)) {
        if (matches.length >= maxMatches) {
          return { matches, truncated: true };
        }
        matches.push({
          lineIndex,
          start,
          end,
          absoluteStart: absoluteOffset + start,
          absoluteEnd: absoluteOffset + end,
        });
      }
    }
    absoluteOffset += line.length + 1;
  }

  if (rejectZeroLength && sawZeroLengthMatch && !sawNonEmptyMatch) {
    return { error: "zero_length_unsupported" };
  }
  return { matches, truncated: false };
}

function collectRegexMatches(
  source: string,
  pattern: RegExp,
  wholeWord: boolean,
  maxMatches: number,
): CodeSearchResult | { error: "zero_length_unsupported" | "multiline_unsupported" } {
  const matches: CodeSearchMatch[] = [];
  const lineStarts = [0];
  for (let index = 0; index < source.length; index += 1) {
    if (source[index] === "\n") lineStarts.push(index + 1);
  }

  pattern.lastIndex = 0;
  let sawZeroLengthMatch = false;
  let sawNonEmptyMatch = false;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(source)) !== null) {
    if (match[0].length === 0) {
      sawZeroLengthMatch = true;
      const nextCodePoint = source.codePointAt(pattern.lastIndex);
      pattern.lastIndex += nextCodePoint != null && nextCodePoint > 0xffff ? 2 : 1;
      continue;
    }
    sawNonEmptyMatch = true;

    const start = match.index;
    const end = start + match[0].length;
    if (source.slice(start, end).includes("\n")) {
      return { error: "multiline_unsupported" };
    }

    const lineIndex = findLineIndex(lineStarts, start);
    const lineStart = lineStarts[lineIndex] ?? 0;
    const startsInsideWord = start > 0 && isWordCharacter(codePointBefore(source, start));
    const endsInsideWord = end < source.length && isWordCharacter(codePointAt(source, end));
    if (!wholeWord || (!startsInsideWord && !endsInsideWord)) {
      if (matches.length >= maxMatches) {
        return { matches, truncated: true };
      }
      matches.push({
        lineIndex,
        start: start - lineStart,
        end: end - lineStart,
        absoluteStart: start,
        absoluteEnd: end,
      });
    }
  }

  if (sawZeroLengthMatch && !sawNonEmptyMatch) {
    return { error: "zero_length_unsupported" };
  }
  return { matches, truncated: false };
}

function findLineIndex(lineStarts: readonly number[], offset: number): number {
  let low = 0;
  let high = lineStarts.length - 1;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    if ((lineStarts[middle] ?? 0) <= offset) low = middle + 1;
    else high = middle - 1;
  }
  return Math.max(0, high);
}

function emptySearchResult(): CodeSearchResult {
  return { matches: [], truncated: false };
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function isWordCharacter(value: string): boolean {
  return value !== "" && WORD_CHARACTER_RE.test(value);
}

function codePointBefore(value: string, offset: number): string {
  const codePoints = Array.from(value.slice(0, offset));
  return codePoints[codePoints.length - 1] ?? "";
}

function codePointAt(value: string, offset: number): string {
  return Array.from(value.slice(offset))[0] ?? "";
}
