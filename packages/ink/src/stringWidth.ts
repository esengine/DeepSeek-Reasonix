import emojiRegex from 'emoji-regex';
import { eastAsianWidth } from 'get-east-asian-width';
import stripAnsi from 'strip-ansi';
import { getGraphemeSegmenter } from './_internal/intl.js';

const EMOJI_REGEX = emojiRegex();

/** Compute how many terminal cells a string will occupy when printed. */
function stringWidthJavaScript(str: string): number {
  if (typeof str !== 'string' || str.length === 0) {
    return 0;
  }

  // Fast path 1: pure printable ASCII. A single linear scan rules out the
  // expensive cases (ANSI escapes, wide chars, combining marks) and lets us
  // return char count without invoking Intl segmenters or east-asian-width.
  let isPureAscii = true;
  for (let i = 0; i < str.length; i++) {
    const code = str.charCodeAt(i);
    if (code >= 127 || code === 0x1b) {
      isPureAscii = false;
      break;
    }
  }
  if (isPureAscii) {
    let width = 0;
    for (let i = 0; i < str.length; i++) {
      const code = str.charCodeAt(i);
      // Control characters take zero cells; everything from space upward
      // counts as one.
      if (code > 0x1f) {
        width++;
      }
    }
    return width;
  }

  // Strip SGR/CSI sequences if present. We only do this when an ESC was
  // actually seen — stripAnsi has measurable allocation cost.
  if (str.includes('\x1b')) {
    str = stripAnsi(str);
    if (str.length === 0) {
      return 0;
    }
  }

  // Fast path 2: non-ASCII but without emoji, variation selectors, or ZWJ.
  // No need to run the grapheme segmenter — per-code-point width is enough.
  if (!needsSegmentation(str)) {
    let width = 0;
    for (const char of str) {
      const codePoint = char.codePointAt(0)!;
      if (!isZeroWidth(codePoint)) {
        width += eastAsianWidth(codePoint, { ambiguousAsWide: false });
      }
    }
    return width;
  }

  // Slow path: full grapheme segmentation. Emoji presentation sequences
  // and Indic conjuncts both depend on neighbouring code points, so we
  // can't compute per-code-point widths in isolation.
  let width = 0;

  for (const { segment: grapheme } of getGraphemeSegmenter().segment(str)) {
    EMOJI_REGEX.lastIndex = 0;
    if (EMOJI_REGEX.test(grapheme)) {
      width += getEmojiWidth(grapheme);
      continue;
    }

    // For complex clusters (e.g. base + virama + ZWJ + base), count only
    // the first spacing code point. The cluster renders as one glyph and
    // we don't want to double-count the trailing combiners.
    for (const char of grapheme) {
      const codePoint = char.codePointAt(0)!;
      if (!isZeroWidth(codePoint)) {
        width += eastAsianWidth(codePoint, { ambiguousAsWide: false });
        break;
      }
    }
  }

  return width;
}

// Cheap pre-check: does this string contain anything that the grapheme
// segmenter could combine? Pictographic ranges, regional indicators, the
// variation selectors and ZWJ all participate in joiner sequences.
function needsSegmentation(str: string): boolean {
  for (const char of str) {
    const cp = char.codePointAt(0)!;
    if (cp >= 0x1f300 && cp <= 0x1faff) return true;
    if (cp >= 0x2600 && cp <= 0x27bf) return true;
    if (cp >= 0x1f1e6 && cp <= 0x1f1ff) return true;
    if (cp >= 0xfe00 && cp <= 0xfe0f) return true;
    if (cp === 0x200d) return true;
  }
  return false;
}

function getEmojiWidth(grapheme: string): number {
  const first = grapheme.codePointAt(0)!;

  // Regional indicators: a single RI is width 1 (treated as a letter), a
  // pair renders as a flag glyph at width 2.
  if (first >= 0x1f1e6 && first <= 0x1f1ff) {
    let count = 0;
    for (const _ of grapheme) count++;
    return count === 1 ? 1 : 2;
  }

  // Incomplete keycap (digit/#/*/ + VS16 but no U+20E3 combining mark) —
  // the variation selector forces emoji presentation but without the
  // enclosing keycap glyph it's still rendered narrow on most terminals.
  if (grapheme.length === 2) {
    const second = grapheme.codePointAt(1);
    if (
      second === 0xfe0f &&
      ((first >= 0x30 && first <= 0x39) || first === 0x23 || first === 0x2a)
    ) {
      return 1;
    }
  }

  return 2;
}

// Classification table for "this code point contributes zero cells".
// Organized by frequency-of-hit on typical inputs: the hottest range
// (printable ASCII, Latin-1) short-circuits first.
function isZeroWidth(codePoint: number): boolean {
  if (codePoint >= 0x20 && codePoint < 0x7f) return false;
  if (codePoint >= 0xa0 && codePoint < 0x0300) return codePoint === 0x00ad;

  // C0 and C1 control bands.
  if (codePoint <= 0x1f || (codePoint >= 0x7f && codePoint <= 0x9f)) return true;

  // Format characters that intentionally produce no glyph.
  if (
    (codePoint >= 0x200b && codePoint <= 0x200d) ||
    codePoint === 0xfeff ||
    (codePoint >= 0x2060 && codePoint <= 0x2064)
  ) {
    return true;
  }

  // Variation selectors VS1-VS16 plus the supplementary VS17-VS256 block.
  if (
    (codePoint >= 0xfe00 && codePoint <= 0xfe0f) ||
    (codePoint >= 0xe0100 && codePoint <= 0xe01ef)
  ) {
    return true;
  }

  // Combining marks: general, extended, supplement, enclosing, half-marks.
  if (
    (codePoint >= 0x0300 && codePoint <= 0x036f) ||
    (codePoint >= 0x1ab0 && codePoint <= 0x1aff) ||
    (codePoint >= 0x1dc0 && codePoint <= 0x1dff) ||
    (codePoint >= 0x20d0 && codePoint <= 0x20ff) ||
    (codePoint >= 0xfe20 && codePoint <= 0xfe2f)
  ) {
    return true;
  }

  // Indic combining marks (Devanagari through Malayalam). The script
  // blocks are spaced at 0x80 boundaries; within each block the same
  // offsets carry signs and vowel marks, so we mask once and dispatch.
  if (codePoint >= 0x0900 && codePoint <= 0x0d4f) {
    const offset = codePoint & 0x7f;
    if (offset <= 0x03) return true; // anusvara / chandrabindu / visarga
    if (offset >= 0x3a && offset <= 0x4f) return true; // dependent vowel signs + virama
    if (offset >= 0x51 && offset <= 0x57) return true; // stress / accent
    if (offset >= 0x62 && offset <= 0x63) return true; // L/LL vowel signs
  }

  // Thai and Lao combining marks. We deliberately do NOT include U+0E32,
  // U+0E33, U+0EB2, U+0EB3 — those are spacing vowels (width 1) despite
  // sitting in the same script block.
  if (
    codePoint === 0x0e31 ||
    (codePoint >= 0x0e34 && codePoint <= 0x0e3a) ||
    (codePoint >= 0x0e47 && codePoint <= 0x0e4e) ||
    codePoint === 0x0eb1 ||
    (codePoint >= 0x0eb4 && codePoint <= 0x0ebc) ||
    (codePoint >= 0x0ec8 && codePoint <= 0x0ecd)
  ) {
    return true;
  }

  // Arabic format controls: sub/super script, end-of-ayah, etc.
  if (
    (codePoint >= 0x0600 && codePoint <= 0x0605) ||
    codePoint === 0x06dd ||
    codePoint === 0x070f ||
    codePoint === 0x08e2
  ) {
    return true;
  }

  // Surrogate halves (shouldn't appear in well-formed strings) and tag
  // characters (used in flag sequences, glyphless on their own).
  if (codePoint >= 0xd800 && codePoint <= 0xdfff) return true;
  if (codePoint >= 0xe0000 && codePoint <= 0xe007f) return true;

  return false;
}

// Resolve once at module load. `typeof Bun` triggers a deopt on every call
// in V8 — we hit stringWidth roughly 100k times per frame in the renderer,
// so the branch needs to fold to a constant indirect call.
const bunStringWidth =
  typeof Bun !== 'undefined' && typeof Bun.stringWidth === 'function'
    ? Bun.stringWidth
    : null;

const BUN_STRING_WIDTH_OPTS = { ambiguousIsNarrow: true } as const;

export const stringWidth: (str: string) => number = bunStringWidth
  ? str => bunStringWidth(str, BUN_STRING_WIDTH_OPTS)
  : stringWidthJavaScript;
