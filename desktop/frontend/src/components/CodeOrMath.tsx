import { stripMathDelimiters } from "./latexNormalize";
import { InlineMath } from "./InlineMath";

const LATEX_COMMANDS = /\\(?:frac|sqrt|text|mathbf|mathit|mathrm|mathcal|mathbb|mathfrak|mathscr|alpha|beta|gamma|delta|epsilon|zeta|eta|theta|iota|kappa|lambda|mu|nu|xi|omicron|pi|rho|sigma|tau|upsilon|phi|chi|psi|omega|Gamma|Delta|Theta|Lambda|Xi|Pi|Sigma|Upsilon|Phi|Psi|Omega|sum|prod|int|iint|iiint|oint|lim|infty|partial|nabla|forall|exists|neg|in|notin|subset|subseteq|supset|supseteq|cup|cap|emptyset|varnothing|times|cdot|circ|div|pm|mp|leq|geq|ll|gg|approx|equiv|sim|simeq|propto|neq|perp|parallel|mid|angle|triangle|square|diamond|big|Big|bigg|Bigg|binom|choose|over|atop|underbrace|overbrace|widetilde|widehat|overline|underline|vec|hat|tilde|bar|dot|ddot|mathring|acute|grave|breve|check|begin|end)(?![A-Za-z])/;

const CODE_TOKENS = /[{}]\s*return\b|=>|===|!==|;\s*$|;\s*\/[/*]|^[ \t]*(const|let|var|function|func|class|interface|type|enum|import|export|require|from|SELECT|INSERT|UPDATE|DELETE|CREATE|DROP|ALTER)\b|\b(?:async|await|yield)\b/;

const MATH_WRAPPERS = /^(?:\\\(.*\\\)|\$.*\$)$/;

const SUB_SUPER = /[a-zA-Z0-9]\^\{?[a-zA-Z0-9]|[a-zA-Z0-9]_\{?[a-zA-Z0-9]/;

const CODE_LIKE = /^(?:[a-zA-Z]:[/\\]|(?:\.{1,2}[/\\])+|(?:npm|yarn|pnpm|npx|git|docker|kubectl|ssh|curl|wget|make|cmake)\s|\.(?:tsx?|jsx?|go|py|rs|java|cpp|c|h|json|yaml|yml|toml|sh|bash|zsh|env|css|html|xml|sql|md)$)/;

const PATH_LIKE = /[/\\][\w.-]+(?:\.[a-z]+)?$|^\.[a-zA-Z]+$/;

/**
 * Classify inline code content as math or real code.
 * Returns true when the content should be rendered as math.
 */
export function isInlineMathLike(content: string): boolean {
  const trimmed = content.trim();
  if (!trimmed) return false;

  // If wrapped in math delimiters, it's math
  if (MATH_WRAPPERS.test(trimmed)) return true;

  // Strong LaTeX signals
  if (LATEX_COMMANDS.test(trimmed)) return true;

  // Subscript / superscript patterns
  if (SUB_SUPER.test(trimmed)) return true;

  // Contains \ followed by alpha chars (generic LaTeX detection)
  if (/\\[A-Za-z]+/.test(trimmed) && !CODE_LIKE.test(trimmed)) return true;

  // Strong code signals → keep as code
  if (CODE_TOKENS.test(trimmed)) return false;
  if (CODE_LIKE.test(trimmed)) return false;
  if (PATH_LIKE.test(trimmed)) return false;
  if (/^[$]?\d+[kmg]?b?$/i.test(trimmed)) return false; // file sizes

  // Default to code for ambiguous short strings
  if (trimmed.length < 3 && !LATEX_COMMANDS.test(trimmed)) return false;

  return false;
}

interface CodeOrMathProps {
  source: string;
}

/**
 * Renders inline code as either InlineMath or a styled code span,
 * classifying based on content.
 */
export function CodeOrMath({ source }: CodeOrMathProps) {
  const raw = source.trim();

  // If the original source is wrapped in math delimiters, treat it as math
  // even if the inner content has no strong LaTeX signals (e.g. `\(x+1\)`).
  if (MATH_WRAPPERS.test(raw)) {
    return <InlineMath source={stripMathDelimiters(raw)} />;
  }

  const trimmed = stripMathDelimiters(raw);

  if (isInlineMathLike(trimmed)) {
    return <InlineMath source={trimmed} />;
  }

  return <code className="md-code">{raw}</code>;
}
