/**
 * East-Asian-Width-aware text size estimation for transcript rows.
 *
 * The previous estimator assumed 88 characters per wrapped line regardless
 * of glyph width. Full-width scripts (CJK, fullwidth punctuation, most
 * emoji) occupy two terminal columns, so a CJK-heavy paragraph wraps at
 * roughly half the character count — the old estimate undershot real row
 * heights by ~2x for Chinese content. Height estimates feed Virtuoso's
 * initial size tree and every estimate-based scrollToIndex landing, so that
 * bias scaled with transcript length (#8657).
 *
 * This module counts display columns instead of characters: wide code
 * points cost 2 columns, zero-width marks cost 0, everything else costs 1.
 */

/** Wrapped-line capacity in display columns (≈88 half-width glyphs). */
export const TRANSCRIPT_ESTIMATED_LINE_COLUMNS = 88;
export const TRANSCRIPT_ESTIMATED_LINE_HEIGHT = 20;
export const TRANSCRIPT_MAX_ESTIMATED_TEXT_HEIGHT = 12_000;

function codePointColumns(codePoint: number): number {
  // Zero-width: combining marks, ZWJ, variation selectors.
  if (
    (codePoint >= 0x0300 && codePoint <= 0x036f)
    || codePoint === 0x200d
    || (codePoint >= 0xfe00 && codePoint <= 0xfe0f)
  ) {
    return 0;
  }
  // Wide/fullwidth ranges (East Asian Width W/F), plus emoji presentation
  // blocks which render double-width in the desktop font stack.
  if (
    (codePoint >= 0x1100 && codePoint <= 0x115f) // Hangul Jamo
    || (codePoint >= 0x2e80 && codePoint <= 0xa4cf) // CJK radicals … Yi
    || (codePoint >= 0xa960 && codePoint <= 0xa97f) // Hangul Jamo Extended-A
    || (codePoint >= 0xac00 && codePoint <= 0xd7a3) // Hangul syllables
    || (codePoint >= 0xf900 && codePoint <= 0xfaff) // CJK compat ideographs
    || (codePoint >= 0xfe30 && codePoint <= 0xfe6f) // CJK compat forms
    || (codePoint >= 0xff00 && codePoint <= 0xff60) // Fullwidth forms
    || (codePoint >= 0xffe0 && codePoint <= 0xffe6) // Fullwidth signs
    || (codePoint >= 0x1f300 && codePoint <= 0x1faff) // Emoji & symbols
    || (codePoint >= 0x20000 && codePoint <= 0x3fffd) // CJK Ext B+
  ) {
    return 2;
  }
  return 1;
}

/** Display-column width of text, ignoring newline handling. */
export function eastAsianWidthColumns(text: string): number {
  let columns = 0;
  for (const char of text) {
    columns += codePointColumns(char.codePointAt(0) ?? 0);
  }
  return columns;
}

/**
 * Estimated rendered height for a block of text. Each explicit line segment
 * wraps independently at TRANSCRIPT_ESTIMATED_LINE_COLUMNS display columns;
 * the result is floored at `minimum` and capped at
 * TRANSCRIPT_MAX_ESTIMATED_TEXT_HEIGHT. Mirrors the shape of the previous
 * char-based estimator so callers can swap implementations.
 */
export function estimateTranscriptTextHeight(
  text: string | undefined,
  minimum: number,
  options: { lineColumns?: number; maximum?: number } = {},
): number {
  if (!text) return minimum;
  const lineCapacity = Math.max(1, Math.round(options.lineColumns ?? TRANSCRIPT_ESTIMATED_LINE_COLUMNS));
  const maximum = Math.max(minimum, options.maximum ?? TRANSCRIPT_MAX_ESTIMATED_TEXT_HEIGHT);
  const cappedLines = Math.max(1, Math.ceil((maximum - 44) / TRANSCRIPT_ESTIMATED_LINE_HEIGHT));
  let totalLines = 0;
  let segmentColumns = 0;
  const flushSegment = (): boolean => {
    totalLines += Math.max(1, Math.ceil(segmentColumns / lineCapacity));
    segmentColumns = 0;
    return totalLines >= cappedLines;
  };
  for (const char of text) {
    if (char === "\n") {
      if (flushSegment()) return maximum;
      continue;
    }
    segmentColumns += codePointColumns(char.codePointAt(0) ?? 0);
    // Cheap early exit for very long single-line blobs.
    if (totalLines + Math.ceil(segmentColumns / lineCapacity) >= cappedLines) {
      return maximum;
    }
  }
  flushSegment();
  return Math.min(
    maximum,
    Math.max(minimum, 44 + totalLines * TRANSCRIPT_ESTIMATED_LINE_HEIGHT),
  );
}
