// Normalize LaTeX source for KaTeX rendering. Only processes already-identified
// math source — never raw Markdown text.
//
// Handles two things:
// 1. LaTeX text-mode commands (\text{}, \textrm{}, etc.) where KaTeX requires
//    escaped literal characters (#, %, &, _, $, ^, ~).
// 2. | → \vert inside math-mode content.

const TEXT_COMMANDS = new Set([
  "emph",
  "mbox",
  "text",
  "textbf",
  "textit",
  "textmd",
  "textnormal",
  "textrm",
  "textsf",
  "texttt",
  "textup",
]);

export function latexNormalizeForKatex(source: string): string {
  // Convert \slashed{X} → \not{X} and \slashed X → \not X. KaTeX doesn't
  // support \slashed, but \not provides a similar visual effect (slash
  // through the character). This is commonly used in physics for Feynman
  // slash notation (\slashed{p}, \slashed{\partial}).
  // Handles two forms:
  //   1. Braced:    \slashed{X}     → \not{X}
  //   2. Unbraced:  \slashed X      → \not X   (single token, no spaces)
  source = source.replace(/\\slashed\s*\{((?:[^{}]|\{[^{}]*\})*)\}/g, "\\not{$1}");
  // Also handle unbraced forms:
  //   \slashed\epsilon      → \not{\epsilon}
  //   \slashed\epsilon(0)    → \not{\epsilon(0)}
  //   \slashed a              → \not a
  //   \slashed x              → \not x
  // Match a backslash command (optionally followed by (...) for function calls)
  // or a single ASCII letter. Use a function so we can add braces around
  // function-call forms.
  source = source.replace(/\\slashed\s*(\\[A-Za-z]+(?:\([^)]*\))?|[A-Za-z])/g, (_match, inner) => {
    return inner.includes("(") ? `\\not{${inner}}` : `\\not ${inner}`;
  });

  // When \tag is present, convert aligned/gathered/alignedat environments
  // to align/gather/alignat.  KaTeX's aligned/gathered treat the entire
  // block as one equation and only permit a single \tag (parse error
  // "Multiple \tag" on older versions), while align/gather support \tag
  // on every row natively.  We only convert when \tag exists so that
  // plain aligned blocks keep their un-numbered behaviour.
  if (/\\tag\*?\s*\{/.test(source)) {
    source = source
      .replace(/\\begin\{alignedat\}\{(\d+)\}/g, "\\begin{alignat}{$1}")
      .replace(/\\end\{alignedat\}/g, "\\end{alignat}")
      .replace(/\\begin\{aligned\}/g, "\\begin{align}")
      .replace(/\\end\{aligned\}/g, "\\end{align}")
      .replace(/\\begin\{gathered\}/g, "\\begin{gather}")
      .replace(/\\end\{gathered\}/g, "\\end{gather}");
  }

  let out = "";
  let i = 0;

  while (i < source.length) {
    if (source[i] === "\\") {
      const cmd = readCommand(source, i);
      if (cmd && TEXT_COMMANDS.has(cmd.name) && source[cmd.end] === "{") {
        const rewritten = rewriteTextArg(source, cmd.end);
        if (rewritten) {
          out += source.slice(i, cmd.end + 1) + rewritten.content + "}";
          i = rewritten.end + 1;
          continue;
        }
      }
      if (cmd) {
        out += source.slice(i, cmd.end);
        i = cmd.end;
        continue;
      }
      out += source[i];
      i += 1;
      continue;
    }

    if (source[i] === "|") {
      out += "\\vert";
      if (/[A-Za-z]/.test(source[i + 1] ?? "")) out += " ";
      i += 1;
      continue;
    }

    out += source[i];
    i += 1;
  }

  return out;
}

function rewriteTextArg(s: string, openBrace: number): { content: string; end: number } | null {
  let out = "";
  let depth = 1;
  for (let i = openBrace + 1; i < s.length; ) {
    const ch = s[i];
    if (ch === "\\") {
      const cmd = readCommand(s, i);
      const end = cmd?.end ?? i + 1;
      out += s.slice(i, end);
      i = end;
      continue;
    }
    if (ch === "{") {
      depth += 1;
      out += ch;
      i += 1;
      continue;
    }
    if (ch === "}") {
      depth -= 1;
      if (depth === 0) return { content: out, end: i };
      out += ch;
      i += 1;
      continue;
    }
    out += escapeTextChar(ch);
    i += 1;
  }
  return null;
}

function escapeTextChar(ch: string): string {
  if (ch === "$") return "\\textdollar{}";
  if (ch === "#" || ch === "%" || ch === "&" || ch === "_") return `\\${ch}`;
  if (ch === "^") return "\\textasciicircum{}";
  if (ch === "~") return "\\textasciitilde{}";
  return ch;
}

function readCommand(s: string, slash: number): { name: string; end: number } | null {
  if (s[slash] !== "\\" || slash + 1 >= s.length) return null;
  let end = slash + 1;
  while (end < s.length && /[A-Za-z]/.test(s[end])) end += 1;
  if (end > slash + 1) return { name: s.slice(slash + 1, end), end };
  return { name: s[slash + 1], end: slash + 2 };
}

/** Strip outer LaTeX math delimiters from already-identified math content. */
export function stripMathDelimiters(source: string): string {
  const trimmed = source.trim();
  if (trimmed.startsWith("\\[") && trimmed.endsWith("\\]")) {
    return trimmed.slice(2, -2).trim();
  }
  if (trimmed.startsWith("\\(") && trimmed.endsWith("\\)")) {
    return trimmed.slice(2, -2).trim();
  }
  if (trimmed.startsWith("$$") && trimmed.endsWith("$$")) {
    return trimmed.slice(2, -2).trim();
  }
  if (trimmed.startsWith("$") && trimmed.endsWith("$")) {
    return trimmed.slice(1, -1).trim();
  }
  return trimmed;
}
