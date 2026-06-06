// Golden-case verification for math rendering pipeline.
// Run: tsx src/__tests__/math-golden.test.ts
import katex from "katex";
import { stripMathDelimiters, latexNormalizeForKatex } from "../components/latexNormalize";
import { isLikelyInlineMath } from "../components/mathClassify";

let passed = 0;
let failed = 0;

function check(label: string, fn: () => boolean) {
  try {
    if (fn()) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
    else      { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
  } catch (e) {
    process.stdout.write(`  ERROR ${label}: ${e}\n`); failed += 1;
  }
}

function eq(a: any, b: any, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

// ── stripMathDelimiters ────────────────────────────────────────────────────────

console.log("\nstripMathDelimiters");
eq(stripMathDelimiters("\\(x+1\\)"), "x+1", "\\(...\\)");
eq(stripMathDelimiters("\\[E=mc^2\\]"), "E=mc^2", "\\[...\\]");
eq(stripMathDelimiters("$$\\frac{a}{b}$$"), "\\frac{a}{b}", "$$...$$");
eq(stripMathDelimiters("$x_i^2$"), "x_i^2", "$...$");
eq(stripMathDelimiters("plain text"), "plain text", "no delimiters");
eq(stripMathDelimiters("$a|b$"), "a|b", "inline with pipe");

// ── latexNormalizeForKatex ─────────────────────────────────────────────────────

console.log("\nlatexNormalizeForKatex");
eq(latexNormalizeForKatex("x+1"), "x+1", "plain unchanged");
eq(latexNormalizeForKatex("\\text{baryon #}"), "\\text{baryon \\#}", "escapes # in \\text");
eq(latexNormalizeForKatex("\\text{cost is $5}"), "\\text{cost is \\textdollar{}5}", "escapes $ in \\text");
eq(latexNormalizeForKatex("\\text{a & b % c_d ^ e ~ f}"),
  "\\text{a \\& b \\% c\\_d \\textasciicircum{} e \\textasciitilde{} f}",
  "escapes & % _ ^ ~ in \\text");
eq(latexNormalizeForKatex("\\text{already\\_escaped}"), "\\text{already\\_escaped}", "no double-escape");
eq(latexNormalizeForKatex("\\alpha + \\beta"), "\\alpha + \\beta", "non-text commands");
eq(latexNormalizeForKatex("a | b"), "a \\vert b", "| to \\vert without doubled space");
eq(latexNormalizeForKatex("|x|"), "\\vert x\\vert", "|x| keeps command boundary");
eq(latexNormalizeForKatex("\\text{foo \\$ bar}"), "\\text{foo \\$ bar}", "already escaped $");
eq(latexNormalizeForKatex("\\textrm{test #}"), "\\textrm{test \\#}", "\\textrm also handled");
eq(latexNormalizeForKatex("\\textbf{hello world}"), "\\textbf{hello world}", "\\textbf no special chars");
eq(latexNormalizeForKatex("\\tfrac{a}{b}"), "\\tfrac{a}{b}", "nested braces in command");

// ── isLikelyInlineMath (mathClassify) ──────────────────────────────────────────

console.log("\nisLikelyInlineMath — math");
check("$x$ (single var)", () => isLikelyInlineMath("x") === true);
check("$E=mc^2$", () => isLikelyInlineMath("E=mc^2") === true);
check("$x_i^2$", () => isLikelyInlineMath("x_i^2") === true);
check("$\\alpha$", () => isLikelyInlineMath("\\alpha") === true);
check("$a \\le b$", () => isLikelyInlineMath("a \\le b") === true);
check("$\\frac{a}{b}$", () => isLikelyInlineMath("\\frac{a}{b}") === true);
check("$f(x)$", () => isLikelyInlineMath("f(x)") === true);
check("$x+1$", () => isLikelyInlineMath("x+1") === true);

console.log("\nisLikelyInlineMath — currency/link (NOT math)");
check("$5", () => isLikelyInlineMath("5") === false);
check("$10", () => isLikelyInlineMath("10") === false);
check("$10.50", () => isLikelyInlineMath("10.50") === false);
check("$100%", () => isLikelyInlineMath("100%") === false);
check("URL", () => isLikelyInlineMath("https://example.com") === false);
check("prose text", () => isLikelyInlineMath("hello world today") === false);
check("prose $x y z$ (spaces)", () => isLikelyInlineMath("x y z") === false);
check("$PATH$ env token", () => isLikelyInlineMath("PATH") === false);
check("$TODO$ word token", () => isLikelyInlineMath("TODO") === false);
check("$OK$ word token", () => isLikelyInlineMath("OK") === false);
check("$v1$ version token", () => isLikelyInlineMath("v1") === false);
check("$foo$ plain word", () => isLikelyInlineMath("foo") === false);

// ── KaTeX end-to-end rendering ────────────────────────────────────────────────

const chiralSource = String.raw`
\underbrace{N}_{\text{baryon #}}
=
\underbrace{\frac{1+\tau_3}{2}}_{\text{isospin}}
+
\underbrace{g_A \gamma^\mu \gamma_5}_{\text{axial}}
+
\underbrace{SU(2)_L \times SU(2)_R}_{\text{chiral}}
`;

function renderDisplay(source: string): string {
  return katex.renderToString(latexNormalizeForKatex(source), {
    throwOnError: true,
    displayMode: true,
  });
}

console.log("\nKaTeX renderToString — end to end");
check("chiral decomposition renders", () => {
  const html = renderDisplay(chiralSource);
  return !html.includes("katex-error")
    && ["baryon", "isospin", "axial", "chiral"].every((label) => html.includes(label));
});
check("\\|x\\| renders as double bars", () => {
  const html = renderDisplay(String.raw`\|x\|`);
  return !html.includes("katex-error") && html.includes("∥");
});

// ── normalizeMath pre-pass (LLM delimiters + classifier) ───────────────────────
// Simulates Markdown.normalizeMath: converts \(…\)/\[…\] → $/$$, protects
// \\[ line-break spacing, and filters non-math single-$ pairs.

function simulateNormalizeMath(s: string): string {
  const DM = "\x00DM\x00";
  const IM = "\x00IM\x00";
  const lb = "\x00LB\x00";
  let r = s.replace(/\\\\\[/g, lb);
  r = r.replace(/\\\[/g, () => "$$").replace(/\\\]/g, () => "$$");
  r = r.replace(/\\\(/g, () => "$").replace(/\\\)/g, () => "$");
  r = r.replace(/\x00LB\x00/g, "\\\\[");
  // $$…$$ first, then $…$ (avoids cross-matching)
  r = r.replace(/\$\$([\s\S]*?)\$\$/g, (_m: string, m: string) => `${DM}${m}${DM}`);
  r = r.replace(/\$([^$\n]+)\$/g, (_m: string, m: string) => {
    if (!isLikelyInlineMath(m.trim())) return `＄${m}＄`;
    return `${IM}${m}${IM}`;
  });
  return r.replace(/\x00DM\x00/g, () => "$$").replace(/\x00IM\x00/g, () => "$");
}

console.log("\nnormalizeMath — LLM delimiter conversion");
eq(simulateNormalizeMath("\\(x^2\\)"), "$x^2$", "\\(…\\) → $…$");
eq(simulateNormalizeMath("\\[E=mc^2\\]"), "$$E=mc^2$$", "\\[…\\] → $$…$$");
eq(simulateNormalizeMath("\\\\[4pt]"), "\\\\[4pt]", "\\\\[ line-break spacing protected");

console.log("\nnormalizeMath — non-math dollar filtering");
eq(simulateNormalizeMath("costs $5$ today"), "costs ＄5＄ today", "$5$ not math");
eq(simulateNormalizeMath("env $PATH$ here"), "env ＄PATH＄ here", "$PATH$ not math");
eq(simulateNormalizeMath("solve $x^2 + y^2 = z^2$ please"), "solve $x^2 + y^2 = z^2$ please", "$x^2+y^2$ is math");
eq(simulateNormalizeMath("$\\alpha + \\beta$"), "$\\alpha + \\beta$", "$\\alpha+\\beta$ is math");
eq(simulateNormalizeMath("price is $10.50$ each"), "price is ＄10.50＄ each", "$10.50$ not math");

// ── Summary ────────────────────────────────────────────────────────────────────

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
