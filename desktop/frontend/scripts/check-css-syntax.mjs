import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");

/** Returns one message per fault site; an empty array means the file is clean. */
export function checkCssFile(fullPath, label) {
  const source = fs.readFileSync(fullPath, "utf8");
  const result = checkCssDelimiters(source);
  if (!result.ok) return [`${label}:${result.line}:${result.column}\n${result.message}`];

  return findGluedPreludes(source).map(
    (site) =>
      `${label}:${site.line}:${site.column}\n` +
      `  A selector list runs into the next rule without its own declaration block: "${site.text}"`,
  );
}

function main(targets) {
  const files = targets.length > 0 ? targets : ["src/styles.css"];
  let failed = false;

  for (const file of files) {
    const failures = checkCssFile(path.resolve(frontendRoot, file), file);
    if (failures.length === 0) {
      console.log(`CSS syntax check passed: ${file}`);
      continue;
    }
    failed = true;
    for (const failure of failures) console.error(`CSS syntax check failed: ${failure}`);
    console.error(
      "The CSS parser drops the orphaned selectors and swallows the block that follows, so the\n" +
        "damage is invisible in the browser. Restore the deleted block or remove the selectors.",
    );
  }

  return failed ? 1 : 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  process.exitCode = main(process.argv.slice(2));
}

/**
 * Finds a selector list that lost its declaration block and ran into the next
 * rule. Delimiters stay balanced in that shape, so checkCssDelimiters passes,
 * but the CSS parser drops the orphaned selectors and swallows the block that
 * follows — a rule silently stops applying. Only multi-line preludes are
 * judged: a selector list puts `,` at every line end but the last, so a line
 * that breaks that pattern is glued text rather than a selector.
 *
 * A single-line prelude cannot be judged (`@media (max-width: 820px)` and
 * `.a {` look alike), and a glued rule whose last orphan happened to end in
 * `,` is textually a legal selector list. Neither is detectable statically;
 * both need the rendered-DOM check.
 */
function findGluedPreludes(source) {
  const found = [];
  let state = "normal";
  let line = 1;
  let column = 0;
  let buffer = "";
  let bufferLine = 1;
  let bufferColumn = 1;

  const judge = () => {
    const lines = buffer.split("\n").map((entry) => entry.trim()).filter(Boolean);
    if (lines.length < 2) return null;
    // A declaration whose value wraps across lines (`--x: linear-gradient(`)
    // is not a prelude. Element selectors with a pseudo-class read the same
    // way, so they pass unchecked — their siblings still get judged.
    if (/^[-*_a-zA-Z][-\w]*\s*:/.test(lines[0])) return null;
    for (let i = 0; i < lines.length - 1; i += 1) {
      if (!lines[i].endsWith(",")) return lines[i];
    }
    return null;
  };

  const reset = (nextLine, nextColumn) => {
    buffer = "";
    bufferLine = nextLine;
    bufferColumn = nextColumn;
  };

  for (let i = 0; i < source.length; i += 1) {
    const char = source[i];
    const next = source[i + 1];

    if (char === "\n") {
      line += 1;
      column = 0;
    } else {
      column += 1;
    }

    if (state === "comment") {
      if (char === "*" && next === "/") {
        i += 1;
        column += 1;
        state = "normal";
      }
      continue;
    }

    if (state === "single" || state === "double") {
      if (char === "\\") {
        i += 1;
        column += 1;
        continue;
      }
      if ((state === "single" && char === "'") || (state === "double" && char === '"')) {
        state = "normal";
      }
      continue;
    }

    if (char === "/" && next === "*") {
      i += 1;
      column += 1;
      state = "comment";
      continue;
    }

    if (char === "'") {
      state = "single";
      continue;
    }

    if (char === '"') {
      state = "double";
      continue;
    }

    if (char === "{" || char === "}") {
      const text = judge();
      if (text !== null) found.push({ line: bufferLine, column: bufferColumn, text });
      reset(line, column);
      continue;
    }

    // A `;` only terminates declarations and at-rules, never a selector list,
    // so the buffer it closes is not a prelude worth judging.
    if (char === ";") {
      reset(line, column);
      continue;
    }

    // Keep the cursor on the first character that is not whitespace, so the
    // reported position names the selector rather than the blank line above it.
    if (buffer.trim() === "") {
      bufferLine = line;
      bufferColumn = column;
    }
    buffer += char;
  }

  const text = judge();
  if (text !== null) found.push({ line: bufferLine, column: bufferColumn, text });
  return found;
}

function checkCssDelimiters(source) {
  const stack = [];
  let state = "normal";
  let line = 1;
  let column = 0;
  let tokenLine = 1;
  let tokenColumn = 1;

  for (let i = 0; i < source.length; i += 1) {
    const char = source[i];
    const next = source[i + 1];

    if (char === "\n") {
      line += 1;
      column = 0;
    } else {
      column += 1;
    }

    if (state === "comment") {
      if (char === "*" && next === "/") {
        i += 1;
        column += 1;
        state = "normal";
      }
      continue;
    }

    if (state === "single" || state === "double") {
      if (char === "\\") {
        i += 1;
        column += 1;
        continue;
      }
      if ((state === "single" && char === "'") || (state === "double" && char === '"')) {
        state = "normal";
      }
      continue;
    }

    if (char === "/" && next === "*") {
      tokenLine = line;
      tokenColumn = column;
      i += 1;
      column += 1;
      state = "comment";
      continue;
    }

    if (char === "'") {
      tokenLine = line;
      tokenColumn = column;
      state = "single";
      continue;
    }

    if (char === '"') {
      tokenLine = line;
      tokenColumn = column;
      state = "double";
      continue;
    }

    if (char === "{") {
      stack.push({ line, column });
      continue;
    }

    if (char === "}") {
      if (stack.length === 0) {
        return {
          ok: false,
          line,
          column,
          message: "Found a closing brace without a matching opening brace.",
        };
      }
      stack.pop();
    }
  }

  if (state === "comment") {
    return {
      ok: false,
      line: tokenLine,
      column: tokenColumn,
      message: "Found an unterminated CSS comment.",
    };
  }

  if (state === "single" || state === "double") {
    return {
      ok: false,
      line: tokenLine,
      column: tokenColumn,
      message: "Found an unterminated CSS string.",
    };
  }

  if (stack.length > 0) {
    const opener = stack[stack.length - 1];
    return {
      ok: false,
      line: opener.line,
      column: opener.column,
      message: "Found an opening brace without a matching closing brace.",
    };
  }

  return { ok: true };
}
