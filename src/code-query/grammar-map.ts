/** File-extension → tree-sitter grammar name. Pure data — kept free of web-tree-sitter so callers that only need language detection don't pull in the Emscripten runtime. */

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

export function grammarForPath(filePath: string): GrammarName | null {
  const lower = filePath.toLowerCase();
  for (const ext of Object.keys(EXT_TO_GRAMMAR)) {
    if (lower.endsWith(ext)) return EXT_TO_GRAMMAR[ext]!;
  }
  return null;
}
