export type InlineMathDecision = "math" | "currency" | "literal";

export interface InlineMathContext {
  before?: string;
  after?: string;
}

const PURE_NUMBER = /^\d+(?:,\d{3})*(?:\.\d+)?$/;

const CURRENCY_BEFORE =
  /(?:\b(?:costs?|prices?|priced|pay|paid|worth|fees?|budget|salary|total|amount|spend|spent|save|saved|charge|charged|buy|bought|sell|sold|usd|dollars?)\b|(?:价格|售价|花费|成本|预算|金额|合计|总计|总价|单价|支付|付款|收费|价值|卖价|买价|薪资|工资|优惠|节省|美元|美金))\s*(?:(?:is|was|are|were|of|at)\s+|(?:是|为|达|约|共|了)\s*|[:：=]\s*)?$/i;

const CURRENCY_AFTER =
  /^\s*(?:usd\b|dollars?\b|(?:in\s+)?cash\b|each\b|per\b|\/\s*(?:day|week|month|year)\b|美元|美金|人民币|元|块钱|现金|每(?:个|天|周|月|年))/i;

const NUMERIC_MATH_BEFORE =
  /(?:\d(?:,\d{3})*(?:\.\d+)?\s*[-–—−]\s*|(?:[=<>≤≥≈≃~+*/^(,[{]|\\(?:approx|sim|le|ge|lt|gt))\s*)$/;

const SCIENTIFIC_UNIT_PREFIX = String.raw`(?:da|[qryzafpnumcdhkMGTPEZYRQµμ])?`;
const SCIENTIFIC_UNIT_SYMBOL = String.raw`(?:${SCIENTIFIC_UNIT_PREFIX}(?:eV|Hz|mol|cd|Pa|Wb|lm|lx|Bq|Gy|Sv|kat|Wh|Da|bar|m|g|s|A|K|N|J|W|C|V|F|S|T|H|L)|dB|atm|Torr|rpm|bps|Ω|ohms?|bits?|bytes?|minutes?|hours?|min|hr)`;
const SCIENTIFIC_UNIT_BOUNDARY = String.raw`(?=$|[\s,.;:!?()[\]{}\/·⋅×^²³⁴⁵⁶⁷⁸⁹⁰-])`;
const NUMERIC_MATH_AFTER = new RegExp(
  String.raw`^\s*(?:[-–—−]\s*\d|${SCIENTIFIC_UNIT_SYMBOL}${SCIENTIFIC_UNIT_BOUNDARY}|°[CFK]${SCIENTIFIC_UNIT_BOUNDARY})`,
  "i",
);

function currencyContext(context: InlineMathContext): Required<InlineMathContext> {
  return {
    before: (context.before ?? "").replace(/[\s()[\]{}"'“”‘’（）【】《》〈〉「」『』]+$/u, ""),
    after: (context.after ?? "").replace(/^[\s()[\]{}"'“”‘’（）【】《》〈〉「」『』,;:，；：。、—–-]+/u, ""),
  };
}

/**
 * Decide how an `inlineMath` node produced by remark-math should render.
 *
 * Pure-number spans remain literal unless their surrounding source provides
 * positive mathematical context (for example a range endpoint or scientific
 * unit). High-confidence currency context repairs paired input such as
 * `costs $5$ today` to the prose token `$5`. Other non-math content is
 * restored verbatim as text.
 */
export function classifyInlineMath(
  math: string,
  context: InlineMathContext = {},
): InlineMathDecision {
  if (!math || math !== math.trim() || math.includes("\n")) return "literal";
  if (math.includes("://") || math.includes("](")) return "literal";

  if (PURE_NUMBER.test(math)) {
    const currency = currencyContext(context);
    if (CURRENCY_BEFORE.test(currency.before) || CURRENCY_AFTER.test(currency.after)) {
      return "currency";
    }
    if (NUMERIC_MATH_BEFORE.test(context.before ?? "") || NUMERIC_MATH_AFTER.test(context.after ?? "")) {
      return "math";
    }
    return "literal";
  }

  // A percentage enclosed in explicit delimiters is mathematical notation,
  // not a currency amount. latexNormalizeForKatex escapes its `%` for KaTeX.
  if (/^\d+(?:,\d{3})*(?:\.\d+)?%$/.test(math)) return "math";

  // Number followed by variable: implicit multiplication (2.5x, 3y^2).
  if (/^\d+(?:\.\d+)?[A-Za-z](?:[A-Za-z0-9^_{}]*)?$/.test(math)) return "math";
  // Number with LaTeX escape: 10\%, 5\cdot3.
  if (/^\d+(?:\.\d+)?\\(?:%|[A-Za-z]+)(?:\{[^{}]*\})?(?:[A-Za-z0-9\\{}^_+\-*/=<>.()]*)$/.test(math)) return "math";

  // Unary plus/minus: +2, -x, +\alpha, - 3.14.
  if (/^[+\-]\s*(?:\d+(?:\.\d+)?|[A-Za-z\\])/.test(math)) return "math";

  // LaTeX command (\alpha, \frac{x}{y}, \tfrac12, ...).
  if (/\\[A-Za-z]+/.test(math)) return "math";
  if (/[\^_{}|]/.test(math)) return "math";
  if (/^[+\-=<>±∓]$/.test(math)) return "math";
  if (/\b(?:alpha|beta|gamma|sum|int|prod|lim|infty|sqrt|frac|sin|cos|tan|log|ln|max|min|partial|nabla|left|right)\b/.test(math)) return "math";
  if (/^[A-Za-z]{1,6}\s*\([^)]{1,80}\)$/.test(math)) return "math";
  if (/^[A-Za-z]'{1,}\s*\([^)]{1,80}\)$/.test(math)) return "math";
  if (/^(?:\(\d+\))+$/.test(math)) return "math";
  if (/[A-Za-z0-9)\]}]\s*[+\-*/=<>]\s*[+\-]?\s*[A-Za-z0-9([{\\]/.test(math)) return "math";

  // One-sided relation/equality against an implicit operand. The complete
  // anchors are important: `=1 dollar` must not be accepted by a prefix match.
  const operand = String.raw`(?:[+\-]?\s*(?:\d+(?:\.\d+)?|[A-Za-z]|\\[A-Za-z]+))`;
  const relation = String.raw`(?:<=?|>=?|=|≠|≤|≥)`;
  if (new RegExp(`^(?:${relation}\\s*${operand}|${operand}\\s*${relation})$`).test(math)) return "math";

  if (/^\(?(?:[A-Za-z0-9]|\\[A-Za-z]+)(?:\s*,\s*(?:[A-Za-z0-9]|\\[A-Za-z]+)){1,10}\)?$/.test(math)) return "math";
  if (/^\[[A-Za-z0-9^_+\-,.\\\s{}]+\]$/.test(math)) return "math";

  if (/[A-Za-z]\s+[A-Za-z]/.test(math)) return "literal";
  if (/^[A-Z][A-Z0-9_]{1,}$/.test(math)) return "literal";
  if (/^v\d+(?:\.\d+)*$/i.test(math)) return "literal";
  if (/^[A-Za-z]{2,}$/.test(math)) return "literal";

  if (/^(?:[A-Za-z]|\\[A-Za-z]+)'{1,}$/.test(math)) return "math";
  return /^[A-Za-z]$/.test(math) ? "math" : "literal";
}

export function isLikelyInlineMath(math: string): boolean {
  return classifyInlineMath(math) === "math";
}
