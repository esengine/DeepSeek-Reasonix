import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");
const targets = process.argv.slice(2);
const files = targets.length > 0 ? targets : ["src/styles.css"];

let failed = false;

for (const file of files) {
  const fullPath = path.resolve(frontendRoot, file);
  const source = fs.readFileSync(fullPath, "utf8");
  const result = checkCssDelimiters(source);
  if (result.ok) {
    console.log(`CSS syntax check passed: ${file}`);
    continue;
  }

  failed = true;
  console.error(`CSS syntax check failed: ${file}:${result.line}:${result.column}`);
  console.error(result.message);
}

if (failed) {
  process.exit(1);
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
