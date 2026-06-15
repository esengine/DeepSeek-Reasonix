// Golden-case verification for the math rendering pipeline.
//
// Run: tsx src/__tests__/math-golden.test.ts
//
// We import the *production* modules (mathNormalize, latexNormalize,
// mathClassify) rather than reimplementing them inline, so this file
// catches regressions in the actual code path that runs inside <Markdown>.

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import katex from "katex";
import { latexNormalizeForKatex, stripMathDelimiters } from "../components/latexNormalize";
import { isLikelyInlineMath } from "../components/mathClassify";
import { normalizeMath } from "../components/mathNormalize";
import { expandYoungDiagrams } from "../components/youngDiagrams";

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
eq(latexNormalizeForKatex("100%"), "100\\%", "raw % escaped to \\% (KaTeX comment-char fix)");
eq(latexNormalizeForKatex("x = 50%"), "x = 50\\%", "% at end of math escaped");
eq(latexNormalizeForKatex("a%b"), "a\\%b", "% between letters escaped");
eq(latexNormalizeForKatex("a\\%b"), "a\\%b", "already-escaped \\% not double-escaped");
eq(latexNormalizeForKatex("\\textrm{test #}"), "\\textrm{test \\#}", "\\textrm also handled");
eq(latexNormalizeForKatex("\\textbf{hello world}"), "\\textbf{hello world}", "\\textbf no special chars");
eq(latexNormalizeForKatex("\\tfrac{a}{b}"), "\\tfrac{a}{b}", "nested braces in command");
eq(latexNormalizeForKatex("\\|x\\|"), "\\|x\\|", "\\| is left alone (readCommand handles \\|, not | branch)");
eq(latexNormalizeForKatex("\\\\|x|"), "\\\\\\vert x\\vert", "\\\\| line break + pipe: both | → \\vert");

// ── latexNormalizeForKatex — array column-spec pipes (regression) ──────────────
// Inside \begin{array}{c|c} the | means "draw a vertical rule" — it must
// NOT be rewritten to \vert, or KaTeX fails with "Unknown column alignment:
// \vert". The whole {...} preamble is copied verbatim.
eq(latexNormalizeForKatex("\\begin{array}{c|c} a & b \\\\ c & d \\end{array}"),
  "\\begin{array}{c|c} a & b \\\\ c & d \\end{array}", "array column-spec | preserved (c|c)");
eq(latexNormalizeForKatex("\\begin{array}{|c|c|} a & b \\end{array}"),
  "\\begin{array}{|c|c|} a & b \\end{array}", "array column-spec ||| preserved");
eq(latexNormalizeForKatex("\\begin{array}{cc|c} a & b & c \\end{array}"),
  "\\begin{array}{cc|c} a & b & c \\end{array}", "array column-spec cc|c preserved");
eq(latexNormalizeForKatex("\\begin{array}{c|c} a & b \\end{array} |x|"),
  "\\begin{array}{c|c} a & b \\end{array} \\vert x\\vert", "pipe OUTSIDE array still → \\vert");
eq(latexNormalizeForKatex("\\begin{tabular}{c|c} a & b \\end{tabular}"),
  "\\begin{tabular}{c|c} a & b \\end{tabular}", "tabular column-spec | preserved");

// ── latexNormalizeForKatex — ket-pipe disambiguation (regression) ─────────────
// In GFM Markdown tables, | is the column delimiter, so kets are written as
// \|uud\rangle. But \| is the "parallel-to" double bar ‖ in LaTeX, not a ket
// bar. We convert \| to \vert when it's a ket opener (\|...\rangle) or bra
// closer (\langle...\|), but leave matched \|...\| norms alone.
eq(latexNormalizeForKatex("\\|uud\\rangle"), "\\vert uud\\rangle", "ket \\|uud\\rangle → \\vert");
eq(latexNormalizeForKatex("\\|\\alpha\\rangle"), "\\vert \\alpha\\rangle", "ket \\|\\alpha\\rangle → \\vert");
eq(latexNormalizeForKatex("\\|u\\uparrow d\\rangle"), "\\vert u\\uparrow d\\rangle", "ket with content → \\vert");
eq(latexNormalizeForKatex("\\frac{1}{\\sqrt{2}}\\|\\psi\\rangle"), "\\frac{1}{\\sqrt{2}}\\vert \\psi\\rangle", "ket in fraction → \\vert");
eq(latexNormalizeForKatex("\\|a\\rangle + \\|b\\rangle"), "\\vert a\\rangle + \\vert b\\rangle", "two kets both → \\vert");
// Norms (matched \|...\| pair) must KEEP the double bar
eq(latexNormalizeForKatex("\\|x\\|"), "\\|x\\|", "norm \\|x\\| preserved (double bar)");
eq(latexNormalizeForKatex("\\|v\\|^2"), "\\|v\\|^2", "norm \\|v\\|^2 preserved");
eq(latexNormalizeForKatex("\\|\\vec{v}\\|"), "\\|\\vec{v}\\|", "norm with content preserved");
// Bra closers (\langle...\|)
eq(latexNormalizeForKatex("\\langle\\psi\\|"), "\\langle\\psi\\vert", "bra \\langle\\psi\\| → \\vert");
// Inner product: \langle x \| y \rangle — the \| between bra and ket content
eq(latexNormalizeForKatex("\\langle x \\| y \\rangle"), "\\langle x \\vert  y \\rangle", "inner product \\| → \\vert");

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
check("uppercase $I$ → math (math name in non-English prose)", () => isLikelyInlineMath("I") === true);
check("uppercase $A$ → math", () => isLikelyInlineMath("A") === true);
check("uppercase $V$ → math", () => isLikelyInlineMath("V") === true);

console.log("\nisLikelyInlineMath — minimal LaTeX patterns (regression)");
// LLMs frequently emit minimal LaTeX in math contexts that the older
// classifier rejected as currency / word tokens. These tests pin down the
// deliberately-permissive rules for common math patterns while keeping pure
// numeric dollar pairs literal because they are common in prose prices.
check("single-digit $1$, $2$, $5$ → NOT math (currency-shaped)", () => isLikelyInlineMath("1") === false);
check("multi-digit $42$ → NOT math (currency-shaped)", () => isLikelyInlineMath("42") === false);
check("$2.5x$ is math (number with variable)", () => isLikelyInlineMath("2.5x") === true);
check("$10\%$ is math (percentage with LaTeX)", () => isLikelyInlineMath("10\\%") === true);
check("$2.5x dollars$ → NOT math (prefix-only numeric variable)", () => isLikelyInlineMath("2.5x dollars") === false);
check("$10\\% off$ → NOT math (prefix-only escaped percentage)", () => isLikelyInlineMath("10\\% off") === false);
check("$5\\cdot3$ is math (number with LaTeX command)", () => isLikelyInlineMath("5\\cdot3") === true);

check("comma-separated $A, B$ → math (ordered pair)", () => isLikelyInlineMath("A, B") === true);
check("comma-separated $1, 2, 3$ → math (sequence)", () => isLikelyInlineMath("1, 2, 3") === true);
check("comma-separated $\\alpha, \\beta$ → math (Greek pair)", () => isLikelyInlineMath("\\alpha, \\beta") === true);
check("parens-wrapped $(A, B)$ inner → math", () => isLikelyInlineMath("(A, B)") === true);
check("$S$ (set name) → math", () => isLikelyInlineMath("S") === true);
check("$S$ with surrounding prose (regression)", () => {
  return normalizeMath("$S$ 非空\n$S$ 有上界") === "$S$ 非空\n$S$ 有上界";
});
check("one-sided comparison $< B$ → math", () => isLikelyInlineMath("< B") === true);
check("one-sided comparison $<= 0$ → math", () => isLikelyInlineMath("<= 0") === true);
check("one-sided comparison $> 5$ → math", () => isLikelyInlineMath("> 5") === true);
check("one-sided comparison $A <$ → math", () => isLikelyInlineMath("A <") === true);
check("$< B$ with surrounding prose", () => {
  return normalizeMath("A 的每个元素 $< B$ 的每个元素") === "A 的每个元素 $< B$ 的每个元素";
});

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

console.log("\nnormalizeMath — \\slashed conversion (regression)");
// KaTeX has no \slashed (Feynman slash notation); it is rewritten to \not.
eq(normalizeMath("$\\slashed{p}$"), "$\\not{p}$", "\\slashed{p} → \\not{p}");
eq(normalizeMath("$\\slashed{\\partial}$"), "$\\not{\\partial}$", "\\slashed{\\partial} → \\not{\\partial}");
eq(normalizeMath("The momentum $\\slashed{p}$ is conserved"), "The momentum $\\not{p}$ is conserved", "\\slashed in prose");
eq(normalizeMath("$\\slashed\\epsilon(0)$"), "$\\not{\\epsilon(0)}$", "\\slashed\\epsilon(0) → \\not{\\epsilon(0)} (unbraced fn)");
eq(normalizeMath("$\\slashed a$"), "$\\not a$", "\\slashed a → \\not a (unbraced letter)");

console.log("\nnormalizeMath — inline $$ glued to prose (regression)");
// User-reported: "…decomposes as$$\n\mathbf{6}…" — block math glued to prose.
// Without a blank line, remark-math parses the opening $$ as an empty math node
// and the formula leaks out as literal text. normalizeMath must insert a blank
// line before any $$ preceded by a letter/closing bracket/etc.
check("inline $$ after prose", () => {
  const out = normalizeMath("decomposes as$$\n\\mathbf{6}.$$");
  return /^decomposes as\n\n\$\$/.test(out) && out.includes("\\mathbf{6}");
});
check("inline $$ after closing bracket", () => {
  const out = normalizeMath("(octet)$$ \\mathbf{56}.$$");
  return out.startsWith("(octet)\n\n$$");
});
check("inline $$ after closing brace (\\end{...}$$)", () => {
  // A model that writes `\end{array}$$` or `\frac{a}{b}$$` on one line
  // has the same micromark-fence problem as the comma case. The
  // closing brace is the most common end-of-content marker in LaTeX
  // math, so the repair-regex character class includes it.
  const out = normalizeMath("$$\\begin{pmatrix}a&b\\\\c&d\\end{pmatrix}$$");
  return out.includes("\\end{pmatrix},\n\n$$") || out.includes("\\end{pmatrix}\n\n$$");
});
check("inline $$ after comma on same line as content", () => {
  // User-reported (2026-06-12, soft-pion chat): the model wrote the
  // closing $$ of a display block on the same line as the trailing
  // comma of the equation content, like
  //   …D(q^2),$$
  //   with $P=…$
  // Without a blank line before the closing $$, micromark-extension-math
  // does not recognise the closing fence (it only checks for $$ at
  // the start of a new line) and consumes the rest of the document
  // as math, which then fails to render with "Can't use function '$'
  // in math mode" on the stray $ inside the equation body.
  const out = normalizeMath("…D(q^2),$$\nwith $P=…$");
  return out.includes("D(q^2),\n\n$$");
});
check("well-formed $$ already on own line is normalised consistently", () => {
  // Whether the model writes `decomposes as$$\n\mathbf{6}.$$` or
  // `decomposes as\n\n$$\n\mathbf{6}.$$`, both must produce the same
  // remark-math-parseable form: opening $$ on its own line, body, blank
  // line, closing $$ on its own line.
  const inline = normalizeMath("decomposes as$$\n\\mathbf{6}.$$");
  const block = normalizeMath("decomposes as\n\n$$\n\\mathbf{6}.$$");
  const expected = "decomposes as\n\n$$\n\\mathbf{6}.\n\n$$";
  return inline === expected && block === expected;
});
check("\\[…\\] → $$…$$ still works (no spurious blank line)", () => {
  return normalizeMath("\\[E=mc^2\\]") === "$$E=mc^2$$";
});
check("digit before $$ is NOT a prose boundary (preserves c^2$$)", () => {
  const out = normalizeMath("c^2$$ x $$");
  return out === "c^2$$ x $$";
});

console.log("\nnormalizeMath — non-math dollar filtering");
eq(normalizeMath("costs $1$ today"), "costs &#36;1&#36; today", "$1$ not math");
eq(normalizeMath("env $PATH$ here"), "env &#36;PATH&#36; here", "$PATH$ not math (env var → &#36; entities so remark-math leaves it literal)");
eq(normalizeMath("solve $x^2 + y^2 = z^2$ please"), "solve $x^2 + y^2 = z^2$ please", "$x^2+y^2$ is math");
eq(normalizeMath("$\\alpha + \\beta$"), "$\\alpha + \\beta$", "$\\alpha+\\beta$ is math");
eq(normalizeMath("price is $10.50$ each"), "price is &#36;10.50&#36; each", "$10.50$ not math");
eq(normalizeMath("$I$ think"), "$I$ think", "$I$ is math (uppercase single letter)");
eq(normalizeMath("it costs $5 and $10 total"), "it costs &#36;5 and &#36;10 total", "multiple prose $ → &#36; entities (dollars preserved, not parsed as math)");

console.log("\nnormalizeMath — Markdown code regions stay literal");
eq(normalizeMath("`$PATH$`"), "`$PATH$`", "inline code with env token");
eq(normalizeMath("Use `$HOME` and `$PATH$`."), "Use `$HOME` and `$PATH$`.", "multiple inline code spans");
eq(normalizeMath("```sh\necho $PATH$\n```"), "```sh\necho $PATH$\n```", "fenced code with env token");
eq(normalizeMath("```\necho $PATH$\n```\n\nsolve $x^2$"), "```\necho $PATH$\n```\n\nsolve $x^2$", "fenced code protected while prose math renders");
eq(normalizeMath("Code: `r.replace(/\\$\\$/, ...)`"), "Code: `r.replace(/\\$\\$/, ...)`", "escaped $ in inline code stays literal");
eq(normalizeMath("```javascript\nr = r.replace(/\\$\\$([\\s\\S]*?)\\$\\$/g, ...);\n```"), "```javascript\nr = r.replace(/\\$\\$([\\s\\S]*?)\\$\\$/g, ...);\n```", "regex patterns with $ in code blocks stay literal");
eq(normalizeMath("Code: `` `${DOLLAR}${m}${DOLLAR}` ``"), "Code: `` `${DOLLAR}${m}${DOLLAR}` ``", "template literals with $ in inline code stay literal");

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
check("$\\text{cost is \\$5}$ escaped dollar stays literal", () => {
  return normalizeMath("$\\text{cost is \\$5}$") === "$\\text{cost is \\$5}$";
});
check("$\\textrm{cost is \\$5}$ escaped dollar stays literal", () => {
  return normalizeMath("$\\textrm{cost is \\$5}$") === "$\\textrm{cost is \\$5}$";
});
check("$\\sqrt{x}$ non-text command preserved", () => {
  return normalizeMath("$\\sqrt{x}$") === "$\\sqrt{x}$";
});

// ── normalizeMath — TEXT_MODE_PAIR trailing content ──────────────────────────────
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

// ── normalizeMath — % in math (KaTeX comment-char) ─────────────────────────────
// KaTeX treats unescaped % as a LaTeX comment to end-of-line, silently
// truncating `$x = 50%$` to `$x = 50$`. Top-level % must be escaped.

console.log("\nnormalizeMath — % in math");
eq(normalizeMath("$x = 50%$"), "$x = 50\\%$", "trailing % escaped");
eq(normalizeMath("$100%$"), "&#36;100%&#36;", "pure percentage stays literal");
eq(normalizeMath("$10\\%$"), "$10\\%$", "already-escaped \\% left alone");

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
  // Array with column-spec pipe — regression: |→\vert used to corrupt {c|c}
  // into {c\vert c} (KaTeX: "Unknown column alignment"). Must render cleanly.
  ["$$\\begin{array}{c|c} a & b \\\\ c & d \\end{array}$$", "array with c|c column spec"],
  ["$$\\begin{array}{cc|c} a & b & c \\\\ d & e & f \\end{array}$$", "array with cc|c column spec"],
  ["$$\\begin{array}{|c|c|} a & b \\\\ c & d \\end{array}$$", "array with |c|c| column spec"],
  // Ket with \| delimiter (common in GFM tables where | must be escaped)
  ["$\\|\\psi\\rangle$", "ket with \\| → single bar (regression)"],
  ["$\\frac{1}{\\sqrt{2}}\\|uud\\rangle$", "ket in fraction with \\|"],
  ["$\\|x\\|$", "norm \\|x\\| → double bar (regression)"],
  ["$\\langle\\psi\\|$", "bra closer \\| → single bar (regression)"],
];
for (const [src, label] of e2e) {
  check(`${label}: ${src}`, () => katexOf(normalizeMath(src), false));
}

// Inputs that contain no math delimiters must survive normalizeMath
// unchanged — KaTeX isn't involved here.
console.log("\nnormalizeMath — non-math inputs pass through");
type Passthrough = { src: string; expected: string; label: string };
const passthrough: Passthrough[] = [
  { src: "costs $100$ today", expected: "costs &#36;100&#36; today", label: "multi-digit currency stays literal" },
  { src: "line break \\\\[4pt] here", expected: "line break \\\\[4pt] here", label: "LaTeX line-break spacing" },
  { src: "hello world", expected: "hello world", label: "plain text" },
];
for (const { src, expected, label } of passthrough) {
  check(`${label}: ${src}`, () => normalizeMath(src) === expected);
}

// ── remark-math render boundary ────────────────────────────────────────────────
// A literal $…$ in normalizeMath output is NOT enough to keep a non-math token
// out of KaTeX: remark-math parses any $…$ it sees, so the classifier's reject
// verdict only holds when the $ is hidden as a &#36; entity. These render through
// the real react-markdown + remark-math + rehype-katex path; the normalizeMath-only
// golden cases above never cross the prose→parser boundary.

console.log("\nnormalizeMath → remark-math render boundary");

function renderHtml(src: string): string {
  return renderToStaticMarkup(
    createElement(ReactMarkdown, {
      remarkPlugins: [remarkGfm, remarkMath],
      rehypePlugins: [rehypeKatex],
      children: normalizeMath(src),
    }),
  );
}

check("currency '$5 and $6' renders as literal dollars, not math", () => {
  const html = renderHtml("These two apples cost $5 and $6");
  return !html.includes("katex") && html.includes("$5") && html.includes("$6");
});
check("env var $PATH$ renders as literal, not math", () => {
  const html = renderHtml("env $PATH$ here");
  return !html.includes("katex") && html.includes("$PATH$");
});
check("real inline math $x^2$ still renders as KaTeX", () => {
  const html = renderHtml("the value $x^2$ here");
  return html.includes("katex");
});

// ── Young diagram / tableau macros ─────────────────────────────────────────────
// `\yng` (ytableau) and `\young` (youngtab) are common in physics —
// SU(N) irreps, tensor decompositions, character tables — but KaTeX
// doesn't bundle either macro package. The pre-pass translates them
// to KaTeX-compatible `\boxed{array}` forms inside the math body so
// the diagram renders as a grid of boxes.

console.log("\nnormalizeMath — Young diagram macros");

check("\\yng(2,1) renders as (2,1) Young diagram", () => {
  const html = renderHtml("$$\\yng(2,1)$$");
  return html.includes("katex-display") && !html.includes("katex-error");
});
check("\\yng(2,1) in prose (no $ delimiters) gets wrapped and rendered", () => {
  // A model that writes "the partition \\yng(2,1) corresponds to the
  // (2,1) irrep" doesn't usually put $$ around the macro. The
  // translator wraps bare \\yng in `$…$` so remark-math sees it as
  // inline math and katex renders the diagram.
  const html = renderHtml("The partition \\yng(2,1) is symmetric.");
  return html.includes("katex") && !html.includes("katex-error") && !html.includes("\\yng");
});
check("\\yng inside \\(...\\) does not get double-wrapped", () => {
  const out = normalizeMath("\\(\\yng(2,1)\\)");
  return out === "$\\begin{array}{l}\\square \\! \\square \\\\[-0.525em] \\square\\end{array}$";
});
check("\\yng inside \\[...\\] stays display math without triple dollars", () => {
  const out = normalizeMath("\\[\\yng(2,1)\\]");
  return out.startsWith("$$\\begin{array}{l}")
    && out.endsWith("$$")
    && !out.includes("$$$");
});
check("escaped dollar before bare \\yng does not suppress wrapping", () => {
  const src = String.raw`Price is \$5; shape \yng(2,1)`;
  const expected = String.raw`Price is \$5; shape $\begin{array}{l}\square \! \square \\[-0.525em] \square\end{array}$`;
  return normalizeMath(src) === expected;
});
check("digit-starting inline math with \\yng does not get nested wrappers", () => {
  const out = normalizeMath("$3\\,\\yng(2,1)$");
  return out === "$3\\,\\begin{array}{l}\\square \\! \\square \\\\[-0.525em] \\square\\end{array}$";
});
check("digit-starting inline math with \\young does not get nested wrappers", () => {
  const out = normalizeMath("$2 + \\young(ab,c)$");
  return out === "$2 + \\begin{array}{l}\\boxed{a} \\! \\boxed{b} \\\\[-0.525em] \\boxed{c}\\end{array}$";
});
check("display math ending in digit closes before following bare \\yng", () => {
  const out = normalizeMath("$$x^2$$ \\yng(1)");
  return out === "$$x^2$$ $\\begin{array}{l}\\square\\end{array}$";
});
check("bare \\yng after inline math is separated from adjacent dollars", () => {
  const out = normalizeMath("$x$\\yng(1)");
  return out === "$x$ $\\begin{array}{l}\\square\\end{array}$";
});
check("bare \\yng before inline math is separated from adjacent dollars", () => {
  const out = normalizeMath("\\yng(1)$x$");
  return out === "$\\begin{array}{l}\\square\\end{array}$ $x$";
});
check("\\yng (2,1) with a space before parens gets wrapped and rendered", () => {
  const html = renderHtml("The partition \\yng (2,1) is symmetric.");
  return html.includes("katex") && !html.includes("katex-error") && !html.includes("\\yng");
});
check("\\yng(3,2,1) renders as (3,2,1) Young diagram", () => {
  const html = renderHtml("$$\\yng(3,2,1)$$");
  return html.includes("katex-display") && !html.includes("katex-error");
});
check("\\yng(2,1){a&b\\\\c\\\\d&e} renders filled Young tableau", () => {
  const html = renderHtml("$$\\yng(2,1){a&b\\\\c\\\\d&e}$$");
  return html.includes("katex-display") && !html.includes("katex-error");
});
check("\\young(2 1) compatibility shorthand renders as (2,1) diagram", () => {
  const html = renderHtml("$$\\young(2 1)$$");
  return html.includes("katex-display") && !html.includes("katex-error");
});
check("\\young(ab,c) labelled youngtab syntax renders labels", () => {
  const html = renderHtml("$$\\young(ab,c)$$");
  return html.includes("katex-display")
    && !html.includes("katex-error")
    && !html.includes("\\young")
    && ["a", "b", "c"].every((label) => html.includes(label));
});
check("\\young(ab,c) labelled cells keep boxes", () => {
  const out = expandYoungDiagrams("\\young(ab,c)");
  return out.includes("\\boxed{a}")
    && out.includes("\\boxed{b}")
    && out.includes("\\boxed{c}");
});
check("\\young(abcd,:cd,:c) skew placeholders are invisible offsets", () => {
  const out = expandYoungDiagrams("\\young(abcd,:cd,:c)");
  return out.includes("\\hphantom{\\boxed{x}}")
    && !out.includes("\\boxed{:}");
});
check("\\yng(4,3,2,1) renders as (4,3,2,1) Young diagram", () => {
  const html = renderHtml("$$\\yng(4,3,2,1)$$");
  return html.includes("katex-display") && !html.includes("katex-error");
});
check("\\yng(3,2,1) uses left-aligned array (rows start at same x)", () => {
  // A Young diagram's shorter rows must start at the same x-position
  // as the longest row's first cell — `{l}` (left) instead of `{c}`
  // (centered) gives that layout. Without this, the diagram looks
  // like each row is independently centred, which isn't a Young
  // diagram.
  const out = expandYoungDiagrams("\\yng(3,2,1)");
  return out.includes("\\begin{array}{l}")
    && !out.includes("\\begin{array}{c}");
});
check("expandYoungDiagrams uses flush cells (\\! cancels \\,) ", () => {
  // Adjacent \square boxes should be flush — the convention for Young
  // diagrams. The translator uses `\!` (negative thin space, -0.1667em)
  // which exactly cancels `\,` so cells touch without visible gap.
  // `\,` (positive thin space) would leave a gap.
  const out = expandYoungDiagrams("\\yng(3)");
  return out.includes("\\!") && !out.includes("\\, ");
});
check("expandYoungDiagrams uses flush rows (\\[-0.525em] closes the math-axis gap)", () => {
  // The math axis positions a \square glyph centred on the row
  // baseline, which leaves a visible ~0.4em gap between the bottom of
  // one row's box and the top of the next row's box when the default
  // 1.2em baseline-to-baseline spacing is used. Using `\\[-0.4em]`
  // between rows pulls each subsequent row up by the math-axis offset,
  // so consecutive rows touch. (Earlier versions tried wrapping each
  // cell in `\raisebox{-0.35em}` which does NOT close the gap —
  // uniform translation can't change the relative distance between
  // row baselines.)
  const out = expandYoungDiagrams("\\yng(2,1)");
  return out.includes("\\\\[-0.525em]");
});
check("expandYoungDiagrams substitutes correct array form", () => {
  // Direct unit test on the translator — no need to go through the
  // full pipeline for this assertion.
  const out = expandYoungDiagrams("\\yng(2,1)");
  return out.includes("\\begin{array}{l}")
    && out.includes("\\square")
    && out.includes(" \\\\[-0.525em] ");
});
check("expandYoungDiagrams handles \\yng with content", () => {
  // Bare \yng in prose gets wrapped in `$…$` so remark-math sees it as
  // math; macros already inside a `$…$` block just substitute the inner
  // form (the surrounding delimiters are preserved).
  // Cells are joined with `\!` (negative thin space) so adjacent
  // boxes are flush. Row separators use `\\[-0.525em]` (per-row
  // negative spacing) so consecutive rows touch — the visible
  // glyph height of `\square` is 0.675em (measured from katex's
  // single-glyph strut), and the default 1.2em baseline spacing
  // leaves 0.525em of gap. `\\[-0.525em]` subtracts exactly that.
  const out = expandYoungDiagrams("\\yng(2,1){a&b\\\\c}");
  return out === "$\\begin{array}{l}\\boxed{a} \\! \\boxed{b} \\\\[-0.525em] \\boxed{c}\\end{array}$";
});
check("expandYoungDiagrams handles labelled \\young rows", () => {
  const out = expandYoungDiagrams("\\young(ab,c)");
  return out === "$\\begin{array}{l}\\boxed{a} \\! \\boxed{b} \\\\[-0.525em] \\boxed{c}\\end{array}$";
});
check("expandYoungDiagrams treats comma-separated numeric \\young as labels, not a 12-cell row", () => {
  const out = expandYoungDiagrams("\\young(12,3)");
  return out === "$\\begin{array}{l}\\boxed{1} \\! \\boxed{2} \\\\[-0.525em] \\boxed{3}\\end{array}$";
});
check("expandYoungDiagrams leaves invalid negative \\yng shape alone", () => {
  const out = expandYoungDiagrams("\\yng(-1)");
  return out === "\\yng(-1)";
});
check("expandYoungDiagrams leaves oversized \\yng shape alone", () => {
  const out = expandYoungDiagrams("\\yng(513)");
  return out === "\\yng(513)";
});
check("expandYoungDiagrams leaves non-Young macros alone", () => {
  const out = expandYoungDiagrams("\\frac{a}{b}");
  return out === "\\frac{a}{b}";
});

// ── Summary ───────────────────────────────────────────────────────────────────

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
