export function isLikelyInlineMath(math: string): boolean {
  if (!math || math !== math.trim() || math.includes("\n")) return false;
  if (math.includes("://") || math.includes("](")) return false;
  if (/^\$?\d+(?:\.\d+)?%?$/.test(math)) return false;

  if (/\\[A-Za-z]+\b/.test(math)) return true;
  if (/[\^_{}|]/.test(math)) return true;
  if (/\b(?:alpha|beta|gamma|sum|int|prod|lim|infty|sqrt|frac|sin|cos|tan|log|ln|max|min|partial|nabla|left|right)\b/.test(math)) return true;
  if (/^[A-Za-z]\s*\([^)]{1,80}\)$/.test(math)) return true;
  if (/[A-Za-z0-9)\]}]\s*[+\-*/=<>]\s*[A-Za-z0-9([{\\]/.test(math)) return true;

  if (/[A-Za-z]\s+[A-Za-z]/.test(math)) return false;
  if (/^[A-Z][A-Z0-9_]{1,}$/.test(math)) return false;
  if (/^v\d+(?:\.\d+)*$/i.test(math)) return false;
  if (/^[A-Za-z]{2,}$/.test(math)) return false;

  // Only allow a single lowercase letter as a math token. Capitalised
  // single letters (I, A, V) are too often English / Roman numerals /
  // acronyms to risk misclassifying them.
  return /^[a-z]$/.test(math);
}
