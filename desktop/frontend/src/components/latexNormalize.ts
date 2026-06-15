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

// Environments whose first {...} argument is a column specification (preamble).
// Inside that brace group, `|` means "draw a vertical rule" (not \vert) and
// `@{...}` is an inter-column decoration — both must be copied verbatim, or
// the | → \vert rewrite below corrupts `{c|c}` into `{c\vert c}` and KaTeX
// fails with "Unknown column alignment: \vert".
const COLUMN_SPEC_ENVS = new Set([
  "array",
  "tabular",
  "tabularx",
  "longtable",
  "matrix", // pmatrix/bmatrix etc. have no preamble, but harmless to list
  "subarray",
]);

export function latexNormalizeForKatex(source: string) {
  // ── Ket-pipe fix ────────────────────────────────────────────────────────
  // In GFM Markdown tables, `|` is the column delimiter, so an LLM that
  // writes a ket like |uud⟩ must escape it as \|uud\rangle to avoid breaking
  // the table. But `\|` in LaTeX/KaTeX is the *parallel-to* symbol (double
  // bar ‖, U+2225), NOT a ket bar (single bar ∣, U+2223). The result is
  // kets rendered with heavy double bars instead of light single bars.
  //
  // We distinguish kets from norms:
  //   \|x\|              → norm (matched \|...\| pair) → keep ‖
  //   \|uud\rangle       → ket (unpaired, ends in \rangle)  → convert to \vert
  //   \langle\psi\|      → bra (unpaired, starts with \langle) → convert to \vert
  //
  // Strategy: find every \|...\rangle or \langle...\| span and rewrite the
  // lone \| inside it to \vert. Matched \|...\| pairs (norms) are left alone.
  source = fixKetPipes(source);

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
        // \begin{array} / \begin{tabular} / etc.: the next {...} argument is
        // a column specification (preamble). Inside it, `|` means "vertical
        // rule" and must NOT be rewritten to \vert — that corrupts `{c|c}`
        // into `{c\vert c}` which KaTeX rejects ("Unknown column alignment").
        // `%` inside a column spec is rare but also belongs to the spec, not
        // the equation, so we copy the whole brace group verbatim.
        if (cmd.name === "begin") {
          const envName = readBeginEnvName(source, cmd.end);
          if (envName && COLUMN_SPEC_ENVS.has(envName)) {
            const specEnd = findMatchingBrace(source, cmd.end, envName);
            if (specEnd > 0) {
              // Copy from the `\` of \begin through the closing `}` of the
              // column spec verbatim — no | or % rewriting inside it.
              out += source.slice(i, specEnd + 1);
              i = specEnd + 1;
              continue;
            }
          }
        }
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

    // KaTeX treats unescaped `%` as a LaTeX comment char and silently
    // truncates the formula — e.g. `$x = 50%$` renders as just `x = 50`.
    // Escape every top-level `%` to `\%`. Already-escaped `\%` is handled
    // above as a 2-char command, so we never reach this branch for it.
    if (source[i] === "%") {
      out += "\\%";
      i += 1;
      continue;
    }

    out += source[i];
    i += 1;
  }

  return out;
}

/**
 * Convert `\|` (parallel-to, double bar) to `\vert` (single bar) when it is
 * used as a ket/bra delimiter rather than a norm.
 *
 * In GFM Markdown tables, `|` is the column delimiter. To write a ket
 * `|ψ⟩` inside a table, the `|` must be escaped as `\|` — otherwise the
 * table breaks. But `\|` in LaTeX/KaTeX renders as the *parallel-to* glyph
 * (‖, U+2225), the heavy double bar used for norms, not the light single
 * bar (∣, U+2223) that kets use. So `\|uud\rangle` renders as `‖uud⟩`
 * instead of `|uud⟩`.
 *
 * We distinguish kets/bra from norms:
 *  - A **norm** is a matched `\|...\|` pair: `\|x\|`, `\|\vec{v}\|^2`.
 *    These correctly need the double bar and are left untouched.
 *  - A **ket** is an unpaired `\|` followed by content ending in
 *    `\rangle`: `\|uud\rangle`. The `\|` is converted to `\vert`.
 *  - A **bra** is `\langle...\|`: `\langle\psi\|`. The trailing `\|` is
 *    converted to `\vert`.
 *
 * We process left-to-right. When we find `\|`, we scan ahead for the next
 * `\|` or `\rangle`:
 *  - next delimiter is `\rangle` → this `\|` is a ket opener → `\vert`
 *  - next delimiter is `\|`     → this is a norm opening → keep `\|`,
 *    and skip the matching closing `\|` so it isn't treated as a ket opener.
 */
function fixKetPipes(source: string): string {
  let out = "";
  let i = 0;
  const len = source.length;

  while (i < len) {
    // Match \| (backslash immediately followed by pipe).
    if (source[i] === "\\" && source[i + 1] === "|") {
      // Scan ahead to find the next \| or \rangle (at brace depth 0).
      let j = i + 2;
      let depth = 0;
      let nextIs = "";
      while (j < len) {
        const ch = source[j];
        if (ch === "\\") {
          // Check for \| or \rangle or \langle
          if (source[j + 1] === "|") {
            nextIs = "pipe";
            break;
          }
          if (source.startsWith("\\rangle", j)) {
            nextIs = "rangle";
            break;
          }
          if (source.startsWith("\\langle", j)) {
            nextIs = "langle";
            break;
          }
          // Skip the escaped command so braces inside it aren't counted.
          const cmd = readCommand(source, j);
          j = cmd ? cmd.end : j + 2;
          continue;
        }
        if (ch === "{") depth += 1;
        else if (ch === "}") depth -= 1;
        j += 1;
      }

      if (nextIs === "rangle") {
        // Ket opener: \|...\rangle → \vert ...\rangle
        out += "\\vert ";
        i += 2;
        continue;
      }
      if (nextIs === "pipe") {
        // Norm pair: \|...\| — keep the opening \| as a double bar and
        // let the content + closing \| process through the main loop
        // normally. We only emit the opening \| here; the closing \| will
        // be handled when we reach it (it will find no \rangle ahead and
        // no unmatched \langle, so it stays \|).
        out += "\\|";
        i += 2;
        continue;
      }
      // No \| or \rangle ahead. This could be a bra closer:
      // \langle\psi\| — the \| is at the end, preceded by \langle{...}.
      // Scan backward through `out` for an unmatched \langle.
      if (hasUnmatchedAngleOpen(out)) {
        out += "\\vert";
        i += 2;
        continue;
      }
      // Truly unpaired \| with no context: conservative — leave as-is.
      out += "\\|";
      i += 2;
      continue;
    }

    out += source[i];
    i += 1;
  }

  return out;
}

/**
 * Check whether `out` (the already-emitted prefix of the output) contains an
 * unmatched `\langle` — i.e. a `\langle` not yet closed by a `\rangle`.
 * Used to detect bra closers: in `\langle\psi\|`, by the time we reach the
 * trailing `\|`, `out` contains `\langle\psi` with no matching `\rangle`,
 * so the `\|` is a bra closer (single bar) not a norm (double bar).
 */
function hasUnmatchedAngleOpen(out: string): boolean {
  // Count \langle vs \rangle in the emitted output.
  let opens = 0;
  let k = 0;
  while (k < out.length) {
    if (out.startsWith("\\langle", k)) {
      opens += 1;
      k += 7;
      continue;
    }
    if (out.startsWith("\\rangle", k)) {
      opens -= 1;
      k += 8;
      continue;
    }
    k += 1;
  }
  return opens > 0;
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

/**
 * After `\begin`, read the environment name in the immediately-following
 * `{...}`. Returns the name (e.g. "array") or null if the structure doesn't
 * match. `cmdEnd` is the index just past the `n` of `\begin`.
 */
function readBeginEnvName(s: string, cmdEnd: number): string | null {
  // Skip whitespace between \begin and {
  let j = cmdEnd;
  while (j < s.length && (s[j] === " " || s[j] === "\t")) j += 1;
  if (s[j] !== "{") return null;
  const close = s.indexOf("}", j + 1);
  if (close < 0) return null;
  const name = s.slice(j + 1, close).trim();
  return name || null;
}

/**
 * Find the matching `}` for the column-spec `{...}` that follows an
 * environment name. `cmdEnd` is the index just past `\begin`. We skip
 * whitespace, consume the env-name `{...}`, then return the index of the
 * closing `}` of the column spec itself. Returns -1 if not found.
 */
function findMatchingBrace(s: string, cmdEnd: number, _envName: string): number {
  let j = cmdEnd;
  // Skip whitespace before the env-name brace.
  while (j < s.length && (s[j] === " " || s[j] === "\t")) j += 1;
  if (s[j] !== "{") return -1;
  // Skip past the env-name {...} group.
  const nameClose = s.indexOf("}", j + 1);
  if (nameClose < 0) return -1;
  // Skip whitespace before the column-spec brace.
  let k = nameClose + 1;
  while (k < s.length && (s[k] === " " || s[k] === "\t")) k += 1;
  if (s[k] !== "{") return -1;
  // Find the matching close brace at depth 0 (column specs like
  // {c|c@{\;}c} contain nested braces, so we track depth).
  let depth = 1;
  let p = k + 1;
  while (p < s.length && depth > 0) {
    const ch = s[p];
    if (ch === "\\") {
      p += 2; // skip escaped char (\{, \}, etc.)
      continue;
    }
    if (ch === "{") depth += 1;
    else if (ch === "}") depth -= 1;
    if (depth === 0) return p;
    p += 1;
  }
  return -1;
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
