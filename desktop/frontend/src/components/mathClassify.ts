export function isLikelyInlineMath(math: string): boolean {
  if (!math || math !== math.trim() || math.includes("\n")) return false;
  if (math.includes("://") || math.includes("](")) return false;
  // Number followed by variable: implicit multiplication (2.5x, 3y^2)
  if (/^\d+(?:\.\d+)?[A-Za-z](?:[A-Za-z0-9^_{}]*)?$/.test(math)) return true;
  // Number with LaTeX escape: 10\%, 5\cdot3
  if (/^\d+(?:\.\d+)?\\(?:%|[A-Za-z]+)(?:\{[^{}]*\})?(?:[A-Za-z0-9\\{}^_+\-*/=<>.()]*)$/.test(math)) return true;
  // Pure numbers, decimals, and percentages are too often currency or
  // prose percentages to parse as math. More explicit numeric math
  // forms are accepted above/below: 2.5x, 10\%, +2, x=50%, etc.
  if (/^\d+(?:\.\d+)?%?$/.test(math)) return false;

  // Unary plus/minus: +2, -x, +\alpha, - 3.14
  if (/^[+\-]\s*(?:\d+(?:\.\d+)?|[A-Za-z\\])/.test(math)) return true;

  if (/\\[A-Za-z]+\b/.test(math)) return true;
  if (/[\^_{}|]/.test(math)) return true;
  if (/\b(?:alpha|beta|gamma|sum|int|prod|lim|infty|sqrt|frac|sin|cos|tan|log|ln|max|min|partial|nabla|left|right)\b/.test(math)) return true;
  if (/^[A-Za-z]\s*\([^)]{1,80}\)$/.test(math)) return true;
  if (/[A-Za-z0-9)\]}]\s*[+\-*/=<>]\s*[A-Za-z0-9([{\\]/.test(math)) return true;
  // One-sided comparison: < B, > 0, B < — comparison against an implicit
  // operand is common in prose.
  if (/^(?:<=?|>=?|≠|≤|≥)\s*[A-Za-z0-9]|[A-Za-z0-9]\s*(?:<=?|>=?|≠|≤|≥)$/.test(math)) return true;
  // Comma-separated tokens: ordered pairs, tuples, sets (A, B), 1, 2, 3,
  // \alpha, \beta. Currency/env-var usage never looks like this.
  if (/^\(?(?:[A-Za-z0-9]|\\[A-Za-z]+)(?:\s*,\s*(?:[A-Za-z0-9]|\\[A-Za-z]+)){1,10}\)?$/.test(math)) return true;

  if (/[A-Za-z]\s+[A-Za-z]/.test(math)) return false;
  if (/^[A-Z][A-Z0-9_]{1,}$/.test(math)) return false;
  if (/^v\d+(?:\.\d+)*$/i.test(math)) return false;
  if (/^[A-Za-z]{2,}$/.test(math)) return false;

  // Single letter (a-z, A-Z). Uppercase single letters (S, A, G, …) are
  // common math names (sets, algebras, groups) and $X$ is essentially
  // never written for the English word I/A by hand.
  return /^[A-Za-z]$/.test(math);
}
