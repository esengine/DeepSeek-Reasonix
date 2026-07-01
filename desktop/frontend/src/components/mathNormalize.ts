// Pre-pass that converts LLM-typical math delimiters into the $/$$ syntax
// that remark-math expects, and runs KaTeX-specific normalisations on each
// recognised math source.
//
//   1. Protect Markdown code spans/fences from all math rewrites.
//   2. Protect LaTeX line-break spacing (\\[...]) from the LLM-delimiter rewrite.
//   3. \(...)/\[...] → $/$$.
//   4. Expand \yng/\young to KaTeX-compatible \boxed{array} forms.
//      Stateful: tracks `$…$` so bare macros in prose get wrapped in
//      `$…$` and macros already inside math just substitute.
//   5. Inline `$$` glued to prose gets a blank line inserted before it
//      (CommonMark requires that block math be paragraph-separated).
//   6. $$…$$ → display placeholders, $…$ → inline placeholders, gated by
//      isLikelyInlineMath; currency / env-var tokens become &#36; entities.
//   7. Each recognised math source is run through latexNormalizeForKatex
//      (text-mode escapes, |→\vert, %→\%).

import { isLikelyInlineMath } from "./mathClassify";
import { latexNormalizeForKatex } from "./latexNormalize";
import { expandYoungDiagrams } from "./youngDiagrams";

// Matches $\cmd{...}...$ where the body may contain $ and one level of nested
// braces. Group 1 captures the full \cmd{...} including the outer }. After
// the closing }, [^$]*? consumes any trailing content (e.g. " + x^2") up to
// the closing $, so patterns like $\text{a} + x^2$ are handled as a whole
// rather than split at stray $ signs inside \text{}.
const TEXT_MODE_PAIR = /\$\s*(\\[A-Za-z]+\{(?:[^{}]|\{[^{}]*\})*\}[^$]*?)\s*\$/g;

const DM = "__REASONIX_MATH_DISPLAY__";
const IM = "__REASONIX_MATH_INLINE__";
const LB = "__REASONIX_LATEX_LINEBREAK__";
const ED_BASE = "REASONIXESCAPEDDOLLAR";
const DOLLAR = "&#36;";

export function normalizeMath(s: string): string {
  const protectedCode = protectMarkdownCode(s);
  let r = normalizeMathText(protectedCode.text);
  for (let i = 0; i < protectedCode.segments.length; i += 1) {
    r = r.split(`${protectedCode.prefix}${i}__`).join(protectedCode.segments[i]);
  }
  return r;
}

function normalizeMathText(s: string): string {
  // Step 1: protect LaTeX line-break spacing (\\[4pt], \\[2ex], ...) so the
  // \[ → $$ rewrite below doesn't swallow it.
  let r = s.replace(/\\\\\[/g, LB);

  // Step 2: convert LLM-native delimiters to standard $/$$ syntax. Arrow
  // functions are required because "$$" in a JS replace string means a
  // single literal $.
  r = r
    .replace(/\\\[/g, () => "$$")
    .replace(/\\\]/g, () => "$$")
    .replace(/\\\(/g, () => "$")
    .replace(/\\\)/g, () => "$");
  r = r.replace(new RegExp(LB, "g"), "\\\\[");

  // Step 2.5: expand \yng/\young macros to KaTeX-compatible \boxed{array}
  // forms after LLM-native delimiters have been converted, so macros inside
  // \(...\) or \[...\] are correctly recognised as already being in math.
  r = expandYoungDiagrams(r);

  // Escaped dollars are literal prose dollars, not math delimiters. Hide them
  // before the $...$ classifier passes so they cannot pair with inserted Young
  // macro wrappers.
  const escapedDollarToken = unusedEscapedDollarToken(r);
  r = r.split("\\$").join(escapedDollarToken);

  // Step 3+4: normalise display $$ blocks and run KaTeX-specific
  // normalisation on each recognised display source. remark-math requires
  // opening and closing $$ to sit on their own lines; LLMs often emit
  // single-line displays, opening fences glued to prose, and adjacent display
  // blocks separated by prose. A line parser avoids the old cross-block regex
  // capture that swallowed prose between two display blocks.
  r = normaliseDisplayBlocks(r);

  // Step 5: $\cmd{...}$ pairs where the body may contain a stray $
  // (e.g. $\text{price is $5}$). Recognised first so the inner $ doesn't
  // terminate a plain $...$ match; latexNormalizeForKatex then escapes
  // the inner $ to \textdollar{}.
  r = r.replace(TEXT_MODE_PAIR, (_match, m) => {
    if (!isLikelyInlineMath(m.trim())) return `${DOLLAR}${m}${DOLLAR}`;
    return `${IM}${latexNormalizeForKatex(m)}${IM}`;
  });

  // Step 6: remaining $…$ → classifier-gated inline math. remark-math
  // parses any literal $…$ it sees, so non-math pairs (currency $5,
  // env vars $PATH$) are wrapped in &#36; entities — remark-math never
  // sees a $, and the decoded entity still renders as a literal dollar.
  r = r.replace(/\$([^$\n]+)\$/g, (_m, m) => {
    if (!isLikelyInlineMath(m.trim())) return `${DOLLAR}${m}${DOLLAR}`;
    return `${IM}${latexNormalizeForKatex(m)}${IM}`;
  });

  // Step 7: restore standard $/$$ delimiters for remark-math to parse.
  return r
    .replace(new RegExp(DM, "g"), () => "$$")
    .replace(new RegExp(IM, "g"), "$")
    .split(escapedDollarToken).join("\\$");
}

function unusedEscapedDollarToken(s: string): string {
  let token = ED_BASE;
  let n = 0;
  while (s.includes(token)) {
    n += 1;
    token = `${ED_BASE}${n}`;
  }
  return token;
}

function protectMarkdownCode(s: string): { text: string; prefix: string; segments: string[] } {
  const prefix = unusedPlaceholderPrefix(s);
  const segments: string[] = [];
  let out = "";
  let i = 0;

  const pushSegment = (segment: string) => {
    const token = `${prefix}${segments.length}__`;
    segments.push(segment);
    out += token;
  };

  while (i < s.length) {
    const fenceEnd = fencedCodeEnd(s, i);
    if (fenceEnd > i) {
      pushSegment(s.slice(i, fenceEnd));
      i = fenceEnd;
      continue;
    }

    if (s[i] === "`") {
      const tickEnd = inlineCodeEnd(s, i);
      if (tickEnd > i) {
        pushSegment(s.slice(i, tickEnd));
        i = tickEnd;
        continue;
      }
    }

    out += s[i];
    i += 1;
  }

  return { text: out, prefix, segments };
}

function unusedPlaceholderPrefix(s: string): string {
  let prefix = "__REASONIX_PROTECTED_CODE__";
  let n = 0;
  while (s.includes(prefix)) {
    n += 1;
    prefix = `__REASONIX_PROTECTED_CODE_${n}__`;
  }
  return prefix;
}

function fencedCodeEnd(s: string, start: number): number {
  // Fence must be at the start of a line (or the document) — CommonMark
  // requirement. Allowing mid-line fences would swallow prose like
  // "wrap code in ```blocks``` here" into the code region.
  if (start !== 0 && s[start - 1] !== "\n") return -1;

  let markerStart = start;
  let spaces = 0;
  while (spaces < 4 && s[markerStart] === " ") {
    markerStart += 1;
    spaces += 1;
  }

  const marker = s[markerStart];
  if (marker !== "`" && marker !== "~") return -1;

  let fenceLen = 0;
  while (s[markerStart + fenceLen] === marker) fenceLen += 1;
  if (fenceLen < 3) return -1;

  const openingLineEnd = lineEnd(s, markerStart + fenceLen);

  // Single-line doc: treat the next matching fence as the closing fence.
  if (openingLineEnd >= s.length) {
    const fencePattern = marker.repeat(fenceLen);
    const nextFence = s.indexOf(fencePattern, markerStart + fenceLen);
    if (nextFence === -1) return s.length;
    return nextFence + fenceLen;
  }

  let lineStart = openingLineEnd + 1;
  while (lineStart < s.length) {
    const currentLineEnd = lineEnd(s, lineStart);
    if (isClosingFenceLine(s, lineStart, currentLineEnd, marker, fenceLen)) {
      return currentLineEnd < s.length ? currentLineEnd + 1 : currentLineEnd;
    }
    lineStart = currentLineEnd < s.length ? currentLineEnd + 1 : currentLineEnd;
  }

  return s.length;
}

function isClosingFenceLine(s: string, start: number, end: number, marker: string, minLen: number): boolean {
  let i = start;
  let spaces = 0;
  while (spaces < 4 && s[i] === " ") {
    i += 1;
    spaces += 1;
  }

  let count = 0;
  while (s[i + count] === marker) count += 1;
  if (count < minLen) return false;

  for (let j = i + count; j < end; j += 1) {
    if (s[j] !== " " && s[j] !== "\t") return false;
  }
  return true;
}

function inlineCodeEnd(s: string, start: number): number {
  let tickLen = 0;
  while (s[start + tickLen] === "`") tickLen += 1;

  const ticks = "`".repeat(tickLen);
  const end = s.indexOf(ticks, start + tickLen);
  return end < 0 ? -1 : end + tickLen;
}

function lineEnd(s: string, start: number): number {
  const end = s.indexOf("\n", start);
  return end < 0 ? s.length : end;
}

function normaliseDisplayBlocks(s: string): string {
  const lines = s.split("\n");
  const out: string[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    const $$idx = line.indexOf("$$");

    if ($$idx >= 0 && line.indexOf("$$", $$idx + 2) >= 0
        && !($$idx > 0 && /\d/.test(line[$$idx - 1]))) {
      const m = line.match(/^(.*?)\$\$([^\n]*?)\$\$(.*)$/);
      if (m) {
        const quote = blockquotePrefix(m[1]);
        pushDisplayBefore(out, m[1]);
        out.push(DM);
        out.push(latexNormalizeForKatex(m[2]));
        out.push(DM);
        if (m[3]) out.push(normaliseDisplayBlocks(quote ? quote + m[3].trimStart() : m[3]));
        i += 1;
        continue;
      }
    }

    if ($$idx >= 0 && line.indexOf("$$", $$idx + 2) < 0
        && !($$idx > 0 && /\d/.test(line[$$idx - 1]))) {
      const before = line.slice(0, $$idx);
      const afterOpen = line.slice($$idx + 2);
      const quote = blockquotePrefix(before);

      const formulaLines: string[] = [];
      if (afterOpen) formulaLines.push(afterOpen);

      let j = i + 1;
      let found = false;
      while (j < lines.length) {
        const rawLine = lines[j];
        const fLine = quote ? stripBlockquotePrefix(rawLine, quote) : rawLine;
        const closeIdx = fLine.indexOf("$$");
        if (closeIdx >= 0 && fLine.indexOf("$$", closeIdx + 2) < 0) {
          const formulaPart = fLine.slice(0, closeIdx);
          const afterClose = fLine.slice(closeIdx + 2);
          pushDisplayBefore(out, before);
          if (formulaPart) formulaLines.push(formulaPart);
          const formula = formulaLines.join("\n");
          out.push(DM);
          out.push(latexNormalizeForKatex(formula));
          out.push(DM);
          if (afterClose) out.push(quote ? quote + afterClose.trimStart() : afterClose);
          i = j + 1;
          found = true;
          break;
        }
        formulaLines.push(fLine);
        j += 1;
      }

      if (found) continue;
      pushDisplayBefore(out, before);
      out.push("$$");
      if (afterOpen) out.push(afterOpen);
      i += 1;
      continue;
    }

    out.push(line);
    i += 1;
  }

  return out.join("\n");
}

function pushDisplayBefore(out: string[], before: string): void {
  if (!before.trim()) return;
  out.push(before);
}

function blockquotePrefix(before: string): string | null {
  const m = before.match(/^(\s*>\s*)/);
  return m ? m[1] : null;
}

function stripBlockquotePrefix(line: string, prefix: string): string {
  if (line.startsWith(prefix)) return line.slice(prefix.length);
  const marker = prefix.trimEnd();
  if (marker && line.startsWith(marker)) {
    const rest = line.slice(marker.length);
    return rest.startsWith(" ") ? rest.slice(1) : rest;
  }
  return line;
}
