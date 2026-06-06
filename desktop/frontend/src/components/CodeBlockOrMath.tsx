import { stripMathDelimiters } from "./latexNormalize";
import { BlockMath } from "./BlockMath";
import { CodeViewer } from "./CodeViewer";

const MATH_LANGS = new Set(["math", "latex", "tex", "katex"]);

const CODE_LANGS = new Set([
  "js", "jsx", "ts", "tsx", "go", "py", "python", "rs", "rust",
  "java", "c", "cpp", "c++", "h", "hpp", "rb", "ruby", "swift",
  "kt", "kotlin", "scala", "dart", "lua", "r", "jl", "julia",
  "sh", "bash", "zsh", "fish", "ps1", "powershell", "bat", "cmd",
  "json", "yaml", "yml", "toml", "ini", "xml", "html", "css", "scss",
  "less", "sql", "graphql", "proto", "dockerfile", "makefile",
  "diff", "patch", "log", "text", "plaintext",
  "cs", "csharp", "php", "perl", "vb", "fsharp", "fs",
]);

const LATEX_COMMAND_RE = /\\(?:frac|sqrt|begin|end|sum|int|prod|lim|infty|partial|nabla|alpha|beta|gamma|delta|epsilon|theta|lambda|mu|pi|sigma|tau|phi|omega|mathbf|mathrm|mathcal|mathbb|mathfrak|text|textbf|textit|widehat|widetilde|overline|underline|vec|hat|tilde|bar|dot)(?![A-Za-z])/;

function isCodeLanguage(lang: string | undefined): boolean {
  if (!lang) return false;
  const lc = lang.toLowerCase().trim();
  if (MATH_LANGS.has(lc)) return false;
  if (CODE_LANGS.has(lc)) return true;
  return false;
}

function looksLikeFormula(content: string): boolean {
  const trimmed = content.trim();
  if (!trimmed) return false;

  // Single-line formula with clear LaTeX command
  if (!trimmed.includes("\n") && LATEX_COMMAND_RE.test(trimmed)) return true;

  // Multi-line: only formula if it has \begin{...} or \end{...}
  if (/\\begin\{/.test(trimmed) || /\\end\{/.test(trimmed)) return true;

  // Multi-line LaTeX alignment
  if (/\\(?:align|equation|gather|multline|matrix|pmatrix|bmatrix|array|tabular|split|cases)\b/.test(trimmed)) {
    return true;
  }

  // Default: not a formula for multi-line content without clear signals
  if (trimmed.includes("\n")) return false;

  return false;
}

const LINE_COUNT_THRESHOLD = 50;
const CHAR_COUNT_THRESHOLD = 10000;

interface CodeBlockOrMathProps {
  value: string;
  language?: string;
  maxHeight?: number;
}

/**
 * Classify a fenced code block as display math or real code and render accordingly.
 */
export function CodeBlockOrMath({ value, language, maxHeight = 400 }: CodeBlockOrMathProps) {
  const langLc = language?.toLowerCase().trim() ?? "";
  const cleanValue = value.replace(/\n$/, "");

  // Explicit math language → BlockMath
  if (MATH_LANGS.has(langLc)) {
    const source = stripMathDelimiters(cleanValue);
    return <BlockMath source={source} />;
  }

  // Known code language → CodeViewer
  if (isCodeLanguage(langLc)) {
    return <CodeViewer value={cleanValue} language={language} maxHeight={maxHeight} />;
  }

  // No explicit language or unknown: classify by content
  // Skip classification for very large blocks
  if (cleanValue.length > CHAR_COUNT_THRESHOLD || cleanValue.split("\n").length > LINE_COUNT_THRESHOLD) {
    return <CodeViewer value={cleanValue} language={language} maxHeight={maxHeight} />;
  }

  if (looksLikeFormula(cleanValue)) {
    const source = stripMathDelimiters(cleanValue);
    return <BlockMath source={source} />;
  }

  return <CodeViewer value={cleanValue} language={language} maxHeight={maxHeight} />;
}
