import { readFile } from "node:fs/promises";
import { resolve as pathResolve } from "node:path";
import {
  type CodeMatchKind,
  type FindInCodeOptions,
  findInCode,
} from "../code-query/find-in-code.js";
import { grammarForPath } from "../code-query/parser.js";
import { extractSymbols } from "../code-query/symbols.js";
import type { ToolRegistry } from "../tools.js";

export interface CodeQueryToolOpts {
  rootDir: string;
}

const UNSUPPORTED =
  "language not supported (TS/TSX/JS/JSX/Python/Go/Rust/Java); use search_content for grep-style matching";

export function registerCodeQueryTools(registry: ToolRegistry, opts: CodeQueryToolOpts): void {
  const { rootDir } = opts;

  registry.register({
    name: "get_symbols",
    description:
      "Outline a single TS/TSX/JS/JSX/Python/Go/Rust/Java file via tree-sitter — returns its top-level + nested symbols (functions, classes, methods, interfaces, types, enums, namespaces) with 1-based line/column. Grammar-aware, ignores names inside comments/strings. Use for 'what's in this file' / 'where is X defined here'; for cross-file scans use search_content. Result: {path, symbols:[{name, kind, line, column, endLine, endColumn, parent?}]} or {path, error}.",
    readOnly: true,
    parallelSafe: true,
    stormExempt: true,
    parameters: {
      type: "object",
      properties: {
        path: {
          type: "string",
          description: "File path (relative to project root or absolute).",
        },
      },
      required: ["path"],
    },
    fn: async (args: { path: string }) => {
      const filePath = resolveProjectPath(rootDir, args.path);
      if (!grammarForPath(filePath)) {
        return JSON.stringify({ path: args.path, error: UNSUPPORTED });
      }
      const source = await readFile(filePath, "utf8");
      const symbols = await extractSymbols(filePath, source);
      return JSON.stringify({ path: args.path, symbols });
    },
  });

  registry.register({
    name: "find_in_code",
    description:
      "Find an identifier `name` in a single TS/TSX/JS/JSX/Python/Go/Rust/Java file, AST-filtered — skips matches inside comments and strings. Optional `kind` narrows by syntactic role: 'call' (function call site), 'definition' (declaration name), 'reference' (other uses), 'any' (default). Within-file only — does NOT resolve cross-file references; use search_content + reading for that. Result: {path, matches:[{line, column, kind, snippet}]} or {path, error}.",
    readOnly: true,
    parallelSafe: true,
    stormExempt: true,
    parameters: {
      type: "object",
      properties: {
        name: {
          type: "string",
          description: "Exact identifier text to find.",
        },
        path: {
          type: "string",
          description: "File path (relative to project root or absolute).",
        },
        kind: {
          type: "string",
          enum: ["any", "call", "definition", "reference"],
          description: "Filter by syntactic role. Default 'any'.",
        },
      },
      required: ["name", "path"],
    },
    fn: async (args: { name: string; path: string; kind?: string }) => {
      const filePath = resolveProjectPath(rootDir, args.path);
      if (!grammarForPath(filePath)) {
        return JSON.stringify({ path: args.path, error: UNSUPPORTED });
      }
      const source = await readFile(filePath, "utf8");
      const kind = (args.kind ?? "any") as CodeMatchKind | "any";
      const findOpts: FindInCodeOptions = kind === "any" ? {} : { kind };
      const matches = await findInCode(filePath, source, args.name, findOpts);
      return JSON.stringify({ path: args.path, matches });
    },
  });
}

function resolveProjectPath(rootDir: string, raw: string): string {
  const stripped = raw.replace(/^[/\\]+/, "");
  return pathResolve(rootDir, stripped.length === 0 ? "." : stripped);
}
