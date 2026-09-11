const DEFAULT_SUMMARY_CHARS = 180;
const WHITESPACE_RE = /\s/u;

export type ReasoningSummaryOptions = {
  /** While streaming, the tail line is the live signal; once done, the head line reads best. */
  streaming: boolean;
  maxChars?: number;
};

type LineBounds = { start: number; end: number };

function hasNonWhitespace(text: string, start: number, end: number): boolean {
  for (let index = start; index < end; index += 1) {
    if (!WHITESPACE_RE.test(text[index])) return true;
  }
  return false;
}

// Scans forward for the first non-blank line without splitting or copying the
// whole string — long streaming reasoning must not allocate a full line per
// token.
function firstNonBlankLine(text: string): LineBounds | null {
  let start = 0;
  while (start < text.length) {
    let end = start;
    while (end < text.length && text[end] !== "\n" && text[end] !== "\r") end += 1;
    if (hasNonWhitespace(text, start, end)) return { start, end };
    start = text[end] === "\r" && text[end + 1] === "\n" ? end + 2 : end + 1;
  }
  return null;
}

// Scans backward for the last non-blank line without copying its content.
// When the current line is longer than the preview budget, it stops at a
// bounded tail window instead of walking all the way back to the line start.
function lastNonBlankLine(text: string, initialWindow: number): LineBounds | null {
  let end = text.length;
  while (end > 0 && (text[end - 1] === "\n" || text[end - 1] === "\r")) end -= 1;
  const minimumWindow = Math.max(32, initialWindow);
  let window = minimumWindow;

  while (end > 0) {
    const windowStart = Math.max(0, end - window);
    let separator = -1;
    for (let index = end - 1; index >= windowStart; index -= 1) {
      if (text[index] === "\n" || text[index] === "\r") {
        separator = index;
        break;
      }
    }
    const start = separator >= 0 ? separator + 1 : windowStart;
    if (hasNonWhitespace(text, start, end)) return { start, end };
    if (separator >= 0) {
      end = separator;
      while (end > 0 && (text[end - 1] === "\n" || text[end - 1] === "\r")) end -= 1;
      window = minimumWindow;
      continue;
    }
    if (windowStart === 0) return null;
    window *= 2;
  }
  return null;
}

function normalizedMaxChars(maxChars: number): number {
  if (!Number.isFinite(maxChars)) return DEFAULT_SUMMARY_CHARS;
  return Math.max(0, Math.floor(maxChars));
}

// Code-point-safe truncation so a surrogate pair is never split. Streaming
// keeps the tail because that is where newly arrived text appears.
function truncateSummary(text: string, maxChars: number, fromEnd: boolean): string {
  const limit = normalizedMaxChars(maxChars);
  if (limit === 0) return "";
  const chars = Array.from(text);
  if (chars.length <= limit) return text;
  if (limit === 1) return "…";
  return fromEnd
    ? `…${chars.slice(-(limit - 1)).join("")}`
    : `${chars.slice(0, limit - 1).join("")}…`;
}

function normalizeLinePreview(text: string, bounds: LineBounds, maxChars: number, fromEnd: boolean): string {
  const limit = normalizedMaxChars(maxChars);
  if (limit === 0) return "";
  // A code point occupies at most two UTF-16 code units. The small cushion
  // covers whitespace that will be collapsed before the character budget is
  // applied, while keeping the per-token allocation bounded.
  const rawWindow = limit * 2 + 16;
  const start = fromEnd ? Math.max(bounds.start, bounds.end - rawWindow) : bounds.start;
  const end = fromEnd ? bounds.end : Math.min(bounds.end, bounds.start + rawWindow);
  const normalized = text.slice(start, end).trim().replace(/\s+/g, " ");
  return truncateSummary(normalized, limit, fromEnd);
}

/** Builds a single-line plain-text preview without invoking another model. */
export function reasoningSummaryText(reasoning: string, { streaming, maxChars = DEFAULT_SUMMARY_CHARS }: ReasoningSummaryOptions): string {
  if (!reasoning) return "";
  const limit = normalizedMaxChars(maxChars);
  if (limit === 0) return "";
  const bounds = streaming ? lastNonBlankLine(reasoning, limit * 2 + 16) : firstNonBlankLine(reasoning);
  return bounds ? normalizeLinePreview(reasoning, bounds, limit, streaming) : "";
}
