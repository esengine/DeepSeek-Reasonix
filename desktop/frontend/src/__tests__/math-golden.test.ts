// Golden-case verification for the math rendering pipeline.
//
// Run: tsx src/__tests__/math-golden.test.ts
//
// We import the *production* modules (mathNormalize, latexNormalize,
// mathClassify) rather than reimplementing them inline, so this file
// catches regressions in the actual code path that runs inside <Markdown>.

import katex from "katex";
import { latexNormalizeForKatex, stripMathDelimiters } from "../components/latexNormalize";
import { isLikelyInlineMath } from "../components/mathClassify";
import { normalizeMath } from "../components/mathNormalize";

let passed = 0;
let failed = 0;

function check(label: string, fn: () => boolean) {
  try {
    if (fn()) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
    else      { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
  } catch (e) {
    process.stdout.write(`  ERROR ${label}: ${(e as Error).message}\n`); failed += 1;
  }
}

function eq(a: unknown, b: unknown, label: string) {
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
eq(latexNormalizeForKatex("\\|x\\|"), "\\|x\\|", "\\| is left alone (readCommand handles \\|, not | branch)");
eq(latexNormalizeForKatex("\\\\|x|"), "\\\\\\vert x\\vert", "\\\\| line break + pipe: both | → \\vert");

// ── latexNormalizeForKatex — \tag → align conversion (regression for KaTeX "Multiple \tag") ──
eq(latexNormalizeForKatex("a = b \\tag{10}"), "a = b \\tag{10}", "\\tag without aligned passes through");
eq(latexNormalizeForKatex("\\begin{aligned} a &= b \\\\ \\end{aligned}"),
  "\\begin{aligned} a &= b \\\\ \\end{aligned}", "aligned without \\tag unchanged");
eq(latexNormalizeForKatex("\\begin{aligned} a &= b \\tag{10}\\\\ c &= d \\end{aligned}"),
  "\\begin{align} a &= b \\tag{10}\\\\ c &= d \\end{align}", "aligned with \\tag → align");
eq(latexNormalizeForKatex("\\begin{aligned} a &= b \\tag{10}\\\\ c &= d \\tag{11} \\end{aligned}"),
  "\\begin{align} a &= b \\tag{10}\\\\ c &= d \\tag{11} \\end{align}", "aligned with multiple \\tag → align");
eq(latexNormalizeForKatex("\\boxed{\\begin{aligned} a &= b \\tag{10}\\\\ c &= d \\end{aligned}}"),
  "\\boxed{\\begin{align} a &= b \\tag{10}\\\\ c &= d \\end{align}}", "boxed aligned with \\tag → boxed align");
eq(latexNormalizeForKatex("\\begin{gathered} a = b \\tag{10}\\\\ c = d \\end{gathered}"),
  "\\begin{gather} a = b \\tag{10}\\\\ c = d \\end{gather}", "gathered with \\tag → gather");

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

console.log("\nisLikelyInlineMath — single-letter regression");
check("lowercase $x$ → math", () => isLikelyInlineMath("x") === true);
check("uppercase $I$ → NOT math (Roman numeral / acronym)", () => isLikelyInlineMath("I") === false);
check("uppercase $A$ → NOT math", () => isLikelyInlineMath("A") === false);
check("uppercase $V$ → NOT math", () => isLikelyInlineMath("V") === false);

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
// These exercise the *production* normalizeMath, not a copy of it.

console.log("\nnormalizeMath — LLM delimiter conversion");
eq(normalizeMath("\\(x^2\\)"), "$x^2$", "\\(…\\) → $…$");
eq(normalizeMath("\\[E=mc^2\\]"), "$$E=mc^2$$", "\\[…\\] → $$…$$");
eq(normalizeMath("\\\\[4pt]"), "\\\\[4pt]", "\\\\[ line-break spacing protected");

// ── normalizeMath — \slashed conversion (regression) ──────────────────────────
// KaTeX has no \slashed (Feynman slash notation); it is rewritten to \not.
eq(normalizeMath("$\\slashed{p}$"), "$\\not{p}$", "\\slashed{p} → \\not{p}");
eq(normalizeMath("$\\slashed{\\partial}$"), "$\\not{\\partial}$", "\\slashed{\\partial} → \\not{\\partial}");
eq(normalizeMath("The momentum $\\slashed{p}$ is conserved"), "The momentum $\\not{p}$ is conserved", "\\slashed in prose");
eq(normalizeMath("$\\slashed\\epsilon(0)$"), "$\\not{\\epsilon(0)}$", "\\slashed\\epsilon(0) → \\not{\\epsilon(0)} (unbraced fn)");
eq(normalizeMath("$\\slashed a$"), "$\\not a$", "\\slashed a → \\not a (unbraced letter)");

console.log("\nnormalizeMath — non-math dollar filtering");
eq(normalizeMath("costs $5$ today"), "costs &#36;5&#36; today", "$5$ not math");
eq(normalizeMath("env $PATH$ here"), "env &#36;PATH&#36; here", "$PATH$ not math");
eq(normalizeMath("solve $x^2 + y^2 = z^2$ please"), "solve $x^2 + y^2 = z^2$ please", "$x^2+y^2$ is math");
eq(normalizeMath("$\\alpha + \\beta$"), "$\\alpha + \\beta$", "$\\alpha+\\beta$ is math");
eq(normalizeMath("price is $10.50$ each"), "price is &#36;10.50&#36; each", "$10.50$ not math");
eq(normalizeMath("$I$ think"), "&#36;I&#36; think", "$I$ not math");
eq(normalizeMath("it costs $5 and $10 total"), "it costs &#36;5 and &#36;10 total", "multiple prose $ stays literal");

console.log("\nnormalizeMath — Markdown code regions stay literal");
eq(normalizeMath("`$PATH$`"), "`$PATH$`", "inline code with env token");
eq(normalizeMath("Use `$HOME` and `$PATH$`."), "Use `$HOME` and `$PATH$`.", "multiple inline code spans");
eq(normalizeMath("```sh\necho $PATH$\n```"), "```sh\necho $PATH$\n```", "fenced code with env token");
eq(normalizeMath("```\necho $PATH$\n```\n\nsolve $x^2$"), "```\necho $PATH$\n```\n\nsolve $x^2$", "fenced code protected while prose math renders");

// ── normalizeMath — text-mode escapes (regression for PR #3287) ───────────────
// The whole point of running latexNormalizeForKatex inside normalizeMath is
// that LLM output like "$\text{price is $5}$" reaches KaTeX with the inner
// $ escaped to \textdollar{}. Before this fix it errored.

console.log("\nnormalizeMath — text-mode escapes (regression)");
check("$\\text{cost is $5}$ inner $ escaped", () => {
  const out = normalizeMath("$\\text{cost is $5}$");
  // After normalisation the inner $ becomes \textdollar{} so KaTeX can render.
  return out.includes("\\textdollar{}") && out === "$\\text{cost is \\textdollar{}5}$";
});
check("$\\text{baryon #}$ # escaped", () => {
  return normalizeMath("$\\text{baryon #}$") === "$\\text{baryon \\#}$";
});
check("$\\text{a & b}$ & escaped", () => {
  return normalizeMath("$\\text{a & b}$") === "$\\text{a \\& b}$";
});
check("$\\sqrt{x}$ non-text command preserved", () => {
  return normalizeMath("$\\sqrt{x}$") === "$\\sqrt{x}$";
});

// ── normalizeMath — TEXT_MODE_PAIR trailing content (Bug 3 fix) ────────────────
// $\cmd{...} + extra$ should be handled as a whole, not split at inner $.

console.log("\nnormalizeMath — TEXT_MODE_PAIR trailing content");
check("$\\text{cost is $5} + x^2$ inner $ escaped with trailing", () => {
  const out = normalizeMath("$\\text{cost is $5} + x^2$");
  return out.includes("\\textdollar{}") && out.includes("+ x^2");
});
check("$\\text{a} | b$ pipe after text command", () => {
  const out = normalizeMath("$\\text{a} | b$");
  return out.includes("\\vert") && out === "$\\text{a} \\vert b$";
});
check("$\\text{abc}$ simple text-mode (no trailing)", () => {
  return normalizeMath("$\\text{abc}$") === "$\\text{abc}$";
});

// ── normalizeMath — pipe handling (| to \vert, \\| preserved) ──────────────────

console.log("\nnormalizeMath — pipe handling");
check("$|x+1|$ absolute value", () => {
  return normalizeMath("$|x+1|$") === "$\\vert x+1\\vert$";
});
check("$\\|x\\|$ norm preserved (no \\vert mangling)", () => {
  return normalizeMath("$\\|x\\|$") === "$\\|x\\|$";
});

// ── normalizeMath — end-to-end KaTeX render of common LLM outputs ──────────────

console.log("\nnormalizeMath → KaTeX end-to-end");
function katexOf(normalized: string, display: boolean): boolean {
  let inner: string;
  if (normalized.startsWith("$$") && normalized.endsWith("$$")) {
    inner = normalized.slice(2, -2);
    display = true;
  } else if (normalized.startsWith("$") && normalized.endsWith("$")) {
    inner = normalized.slice(1, -1);
  } else {
    return false; // no math delimiters — nothing for KaTeX to render
  }
  try {
    katex.renderToString(inner, { throwOnError: true, displayMode: display });
    return true;
  } catch {
    return false;
  }
}

const e2e: Array<[string, string]> = [
  ["$\\text{cost is $5}$", "text mode with literal $"],
  ["$\\text{baryon #}$", "text mode with #"],
  ["$\\text{a & b}$", "text mode with &"],
  ["$\\|x\\|$", "norm"],
  ["$|x+1|$", "abs value"],
  ["$x=1$", "simple equation"],
  ["$\\frac{a}{b}$", "fraction"],
  ["$\\alpha + \\beta$", "greek letters"],
  ["$ \\sqrt{x} $", "sqrt with surrounding spaces"],
  ["$$E=mc^2$$", "display equation"],
  ["\\(\\alpha\\)", "LLM-native inline delimiter"],
  ["\\[\\sum_{i=1}^n i\\]", "LLM-native display delimiter"],
  ["$$ |a| = |b| $$", "display with absolute values"],
  ["$$\\boxed{\\begin{aligned}\nr_A E_\\pi(k;0) &= B(k^2) \\\\\nF_R(k;0) + 2r_A F_\\pi(k;0) &= A(k^2)\n\\end{aligned}}$$", "boxed aligned (no \\tag)"],
  ["$$\\boxed{\\begin{aligned}\nr_A E_\\pi(k;0) &= B(k^2) \\tag{10}\\\\\nF_R(k;0) + 2r_A F_\\pi(k;0) &= A(k^2) \\tag{11}\n\\end{aligned}}$$", "boxed aligned with \\tag → align (no error)"],
  ["\\[\\boxed{\\begin{aligned}\nx &= 1 \\\\\ny &= 2\n\\end{aligned}}\\]", "LLM-native boxed aligned"],
];
for (const [src, label] of e2e) {
  check(`${label}: ${src}`, () => katexOf(normalizeMath(src), false));
}

// Inputs that contain no math delimiters must survive normalizeMath
// unchanged — KaTeX isn't involved here.
console.log("\nnormalizeMath — non-math inputs pass through");
type Passthrough = { src: string; expected: string; label: string };
const passthrough: Passthrough[] = [
  // $5$ is filtered to dollar entities so remark-math leaves it literal
  // and the rendered prose still shows normal dollar signs.
  { src: "costs $5$ today", expected: "costs &#36;5&#36; today", label: "currency stays literal" },
  { src: "line break \\\\[4pt] here", expected: "line break \\\\[4pt] here", label: "LaTeX line-break spacing" },
  { src: "hello world", expected: "hello world", label: "plain text" },
];
for (const { src, expected, label } of passthrough) {
  check(`${label}: ${src}`, () => normalizeMath(src) === expected);
}

// ── Summary ───────────────────────────────────────────────────────────────────

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
