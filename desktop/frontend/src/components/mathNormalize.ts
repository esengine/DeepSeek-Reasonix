// Pre-pass that converts LLM-typical math delimiters into the $/$$ syntax
// that remark-math expects, and runs KaTeX-specific normalisations on the
// resulting math sources.
//
// Pipeline:
//   1. Protect Markdown code spans/fences from all math rewrites.
//   2. Protect LaTeX line-break spacing (\\[...]) from the LLM-delimiter rewrite.
//   3. \(...)/\[...] → $/$$.
//   4. $$...$$ is recognised first and replaced with display placeholders so
//      the single-$ classifier pass can't accidentally match dollars that
//      belong to a display-math block.
//   5. $\cmd{...}$ patterns (e.g. $\text{cost is $5}$) are recognised before
//      plain $...$ so that a stray $ inside \text{} doesn't terminate the
//      match early — KaTeX needs that internal $ to be escaped as
//      \textdollar{}.
//   6. The remaining $...$ pairs go through isLikelyInlineMath; non-math
//      pairs use Markdown dollar entities so remark-math leaves them alone
//      while the rendered prose still shows normal dollar signs.
//   7. Each recognised math source is run through latexNormalizeForKatex
//      (text-mode escapes, |→\vert).

import { isLikelyInlineMath } from "./mathClassify";
import { latexNormalizeForKatex } from "./latexNormalize";

// Matches $\cmd{...}...$ where the body may contain $ and one level of nested
// braces. Group 1 captures the full \cmd{...} including the outer }. After
// the closing }, [^$]*? consumes any trailing content (e.g. " + x^2") up to
// the closing $, so patterns like $\text{a} + x^2$ are handled as a whole
// rather than split at stray $ signs inside \text{}.
const TEXT_MODE_PAIR = /\$\s*(\\[A-Za-z]+\{(?:[^{}]|\{[^{}]*\})*\}[^$]*?)\s*\$/g;

const DM = "__REASONIX_MATH_DISPLAY__";
const IM = "__REASONIX_MATH_INLINE__";
const LB = "__REASONIX_LATEX_LINEBREAK__";
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

  // Step 2: convert LLM-native delimiters to standard $/$$ syntax.
  // Arrow functions are required because "$$" in a JS replace string means
  // a single literal $.
  r = r
    .replace(/\\\[/g, () => "$$")
    .replace(/\\\]/g, () => "$$")
    .replace(/\\\(/g, () => "$")
    .replace(/\\\)/g, () => "$");
  r = r.replace(new RegExp(LB, "g"), "\\\\[");

  // Step 3: $$…$$ → display placeholders. The KaTeX-specific normalisation
  // runs here so |→\vert (with \| protected) and \text{} escapes both apply
  // to display math.
  r = r.replace(/\$\$([\s\S]*?)\$\$/g, (_m, m) => `${DM}${latexNormalizeForKatex(m)}${DM}`);

  // Step 4: $\cmd{...}$ pairs where the body may contain a stray $ (e.g.
  // $\text{price is $5}$). We recognise these first so the stray $ doesn't
  // terminate a plain $...$ match early, then run latexNormalizeForKatex
  // which escapes the inner $ to \textdollar{}.
  r = r.replace(TEXT_MODE_PAIR, (_match, m) => {
    if (!isLikelyInlineMath(m.trim())) return `${DOLLAR}${m}${DOLLAR}`;
    return `${IM}${latexNormalizeForKatex(m)}${IM}`;
  });

  // Step 5: remaining $…$ → classifier-gated inline math. Non-math pairs
  // use Markdown dollar entities so remark-math leaves them as literal text
  // instead of raising on bare currency / env-var / version tokens.
  r = r.replace(/\$([^$\n]+)\$/g, (_m, m) => {
    if (!isLikelyInlineMath(m.trim())) return `${DOLLAR}${m}${DOLLAR}`;
    return `${IM}${latexNormalizeForKatex(m)}${IM}`;
  });

  // Step 6: restore standard $/$$ delimiters for remark-math to parse.
  return r
    .replace(new RegExp(DM, "g"), () => "$$")
    .replace(new RegExp(IM, "g"), () => "$");
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
  if (openingLineEnd >= s.length) return s.length;

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
