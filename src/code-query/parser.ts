/** Tree-sitter parser singleton + bundled grammar registry. */

import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { Language, Parser, type Tree } from "web-tree-sitter";

const localRequire = createRequire(import.meta.url);

export type GrammarName = "typescript" | "tsx" | "javascript" | "python" | "go" | "rust" | "java";

const EXT_TO_GRAMMAR: Record<string, GrammarName> = {
  ".ts": "typescript",
  ".mts": "typescript",
  ".cts": "typescript",
  ".tsx": "tsx",
  ".js": "javascript",
  ".mjs": "javascript",
  ".cjs": "javascript",
  ".jsx": "javascript",
  ".py": "python",
  ".pyi": "python",
  ".go": "go",
  ".rs": "rust",
  ".java": "java",
};

export interface ParserOptions {
  grammarDir?: string;
}

let parserInitPromise: Promise<void> | null = null;
const languageCache = new Map<GrammarName, Promise<Language>>();
let resolvedGrammarDir: string | null = null;

export function grammarForPath(filePath: string): GrammarName | null {
  const lower = filePath.toLowerCase();
  for (const ext of Object.keys(EXT_TO_GRAMMAR)) {
    if (lower.endsWith(ext)) return EXT_TO_GRAMMAR[ext]!;
  }
  return null;
}

export function setGrammarDir(dir: string): void {
  resolvedGrammarDir = dir;
  languageCache.clear();
}

export async function getParser(grammar: GrammarName, opts: ParserOptions = {}): Promise<Parser> {
  if (!parserInitPromise) {
    parserInitPromise = Parser.init({
      locateFile: (name: string) =>
        name === "web-tree-sitter.wasm"
          ? localRequire.resolve("web-tree-sitter/web-tree-sitter.wasm")
          : name,
    });
  }
  await parserInitPromise;
  const language = await loadLanguage(grammar, opts);
  const parser = new Parser();
  parser.setLanguage(language);
  return parser;
}

export async function parseSource(
  filePath: string,
  source: string,
  opts: ParserOptions = {},
): Promise<{ grammar: GrammarName; tree: Tree } | null> {
  const grammar = grammarForPath(filePath);
  if (!grammar) return null;
  const parser = await getParser(grammar, opts);
  const tree = parser.parse(source);
  parser.delete();
  if (!tree) return null;
  return { grammar, tree };
}

function loadLanguage(grammar: GrammarName, opts: ParserOptions): Promise<Language> {
  const cached = languageCache.get(grammar);
  if (cached) return cached;
  const wasmPath = resolveGrammarPath(grammar, opts.grammarDir);
  const bytes = readFileSync(wasmPath);
  const promise = Language.load(new Uint8Array(bytes));
  languageCache.set(grammar, promise);
  return promise;
}

function resolveGrammarPath(grammar: GrammarName, overrideDir?: string): string {
  const filename = `tree-sitter-${grammar}.wasm`;
  const candidates: string[] = [];
  if (overrideDir) candidates.push(resolve(overrideDir, filename));
  if (resolvedGrammarDir) candidates.push(resolve(resolvedGrammarDir, filename));
  candidates.push(resolve(dirname(fileURLToPath(import.meta.url)), "..", "grammars", filename));
  candidates.push(resolve(dirname(fileURLToPath(import.meta.url)), "grammars", filename));
  for (const pkg of DEV_PACKAGE_FOR_GRAMMAR[grammar]) {
    try {
      candidates.push(resolve(dirname(localRequire.resolve(`${pkg}/package.json`)), filename));
    } catch {
      /* dev-only grammar package not installed — fine in production builds */
    }
  }
  for (const candidate of candidates) {
    if (existsSync(candidate)) return candidate;
  }
  throw new Error(`tree-sitter grammar ${grammar} not found. Looked in: ${candidates.join(", ")}`);
}

const DEV_PACKAGE_FOR_GRAMMAR: Record<GrammarName, string[]> = {
  typescript: ["tree-sitter-typescript"],
  tsx: ["tree-sitter-typescript"],
  javascript: ["tree-sitter-javascript"],
  python: ["tree-sitter-python"],
  go: ["tree-sitter-go"],
  rust: ["tree-sitter-rust"],
  java: ["tree-sitter-java"],
};
