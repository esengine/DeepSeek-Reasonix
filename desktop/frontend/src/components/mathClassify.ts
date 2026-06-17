export function isLikelyInlineMath(math: string): boolean {
  if (!math || math !== math.trim() || math.includes("\n")) return false;
  if (math.includes("://") || math.includes("](")) return false;
  // Number followed by variable: implicit multiplication (2.5x, 3y^2)
  if (/^\d+(?:\.\d+)?[A-Za-z](?:[A-Za-z0-9^_{}]*)?$/.test(math)) return true;
  // Number with LaTeX escape: 10\%, 5\cdot3
  if (/^\d+(?:\.\d+)?\\(?:%|[A-Za-z]+)(?:\{[^{}]*\})?(?:[A-Za-z0-9\\{}^_+\-*/=<>.()]*)$/.test(math)) return true;
  // Pure numbers (single/multi-digit, optional decimal/percentage) are
  // math in context (mass eigenvalue $0$, $42$ elements, etc.). Currency
  // in prose is typically written without the closing $ (costs $5), so
  // the $N$ form almost always means math.
  if (/^\d+(?:\.\d+)?%?$/.test(math)) return true;

  // Unary plus/minus: +2, -x, +\alpha, - 3.14
  if (/^[+\-]\s*(?:\d+(?:\.\d+)?|[A-Za-z\\])/.test(math)) return true;

  // LaTeX command (\alpha, \frac{x}{y}, \tfrac12, …). No \b after the
  // command name: \tfrac12 / \sqrt2 / \log3 have no word boundary between
  // the name and a trailing digit, so \b would wrongly reject them.
  if (/\\[A-Za-z]+/.test(math)) return true;
  if (/[\^_{}|]/.test(math)) return true;
  // Lone math operators: +, -, =, <, >, ±, ∓. Common in physics prose
  // ("the sign of $+$", "the $<$ relation"). Must be exactly one operator.
  if (/^[+\-=<>±∓]$/.test(math)) return true;
  if (/\b(?:alpha|beta|gamma|sum|int|prod|lim|infty|sqrt|frac|sin|cos|tan|log|ln|max|min|partial|nabla|left|right)\b/.test(math)) return true;
  // Function / group notation: a short identifier (1–6 letters) immediately
  // followed by parenthesised arguments. Single-letter covers f(x), g(x);
  // multi-letter covers standard group notation such as SO(3,1), SU(2),
  // SL(2), GL(n), Sp(2n), Spin(n), Diff(M), Hom(A,B) — all of which would
  // otherwise fall through to the final "single letter" check and be
  // misclassified as prose. The 6-letter cap and the requirement that the
  // whole token sits inside one $...$ span keep prose parentheticals out.
  if (/^[A-Za-z]{1,6}\s*\([^)]{1,80}\)$/.test(math)) return true;
  // Primed function call: f'(x), g''(x), L'(t). The prime between the
  // function name and the argument list is a LaTeX prime symbol, not an
  // English apostrophe — contractions like "don't" never appear in the
  // shape letter+'+paren.
  if (/^[A-Za-z]'{1,}\s*\([^)]{1,80}\)$/.test(math)) return true;
  // Permutation cycle notation: (12), (123), (12)(34), (123)(45), … —
  // one or more parenthesised digit groups with no separating commas.
  // Without this, $(12)$ falls through every accept rule and is rendered
  // as a literal dollar span instead of reaching KaTeX. (Comma-separated
  // tuples like $(1, 2, 3)$ are already accepted by the rule above.)
  if (/^(?:\(\d+\))+$/.test(math)) return true;
  // Binary operator between operands: A = B, x + 1, E = mc^2. The RHS
  // allows a leading sign ([+\-]?) so that K = -iJ, p = +\alpha,
  // a = -b are recognised (the sign starts a unary operand).
  if (/[A-Za-z0-9)\]}]\s*[+\-*/=<>]\s*[+\-]?\s*[A-Za-z0-9([{\\]/.test(math)) return true;
  // One-sided comparison: < B, > 0, B < — comparison against an implicit
  // operand is common in prose.
  if (/^(?:<=?|>=?|≠|≤|≥)\s*[A-Za-z0-9]|[A-Za-z0-9]\s*(?:<=?|>=?|≠|≤|≥)$/.test(math)) return true;
  // Comma-separated tokens: ordered pairs, tuples, sets (A, B), 1, 2, 3,
  // \alpha, \beta. Currency/env-var usage never looks like this.
  if (/^\(?(?:[A-Za-z0-9]|\\[A-Za-z]+)(?:\s*,\s*(?:[A-Za-z0-9]|\\[A-Za-z]+)){1,10}\)?$/.test(math)) return true;

  // Bracketed label/index: [56], [8], [N], [\mathbf{56}], [56,0^+]. Square
  // brackets in a $...$ context are math — group-theory irrep labels
  // ([56] is the SU(6) multiplet), array subscripts, interval notation.
  // Markdown link syntax [text](url) is already rejected above by the "]("
  // check, so a bare [content] token is unambiguous.
  if (/^\[[A-Za-z0-9^_+\-,.\\\s{}]+\]$/.test(math)) return true;

  if (/[A-Za-z]\s+[A-Za-z]/.test(math)) return false;
  if (/^[A-Z][A-Z0-9_]{1,}$/.test(math)) return false;
  if (/^v\d+(?:\.\d+)*$/i.test(math)) return false;
  if (/^[A-Za-z]{2,}$/.test(math)) return false;

  // Letter or Greek command followed by one or more primes: x', S', y'',
  // \psi'. The prime (') is the LaTeX prime symbol — ubiquitous in math
  // for derivatives, transformed quantities, and labelled states (e.g.
  // the S' mixed-symmetry baryon). An English word never ends in ' in a
  // $...$ context, so this is unambiguously math. Placed after the
  // multi-letter-word rejection so plain prose words stay non-math.
  if (/^(?:[A-Za-z]|\\[A-Za-z]+)'{1,}$/.test(math)) return true;

  // Single letter (a-z, A-Z). Uppercase single letters (S, A, G, …) are
  // common math names (sets, algebras, groups) and $X$ is essentially
  // never written for the English word I/A by hand.
  return /^[A-Za-z]$/.test(math);
}
