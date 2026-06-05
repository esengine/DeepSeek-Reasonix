// Pure language resolution — no highlight.js dependency. Non-lazy consumers (e.g.
// ToolCard) guess a language from a path without pulling the highlighter into the
// main bundle; highlight.js stays behind the lazy editor seam. highlight.ts
// imports ALIASES from here and adds the hljs-backed validation.

export const ALIASES: Record<string, string> = {
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  sh: "bash",
  shell: "bash",
  zsh: "bash",
  py: "python",
  rs: "rust",
  yml: "yaml",
  html: "xml",
  md: "markdown",
};

const EXT: Record<string, string> = {
  go: "go",
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  json: "json",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  py: "python",
  rs: "rust",
  html: "xml",
  xml: "xml",
  css: "css",
  yaml: "yaml",
  yml: "yaml",
  md: "markdown",
};

// extToLang infers a language name from a file path's extension (for tool diffs).
export function extToLang(path: string): string {
  const dot = path.lastIndexOf(".");
  if (dot < 0) return "";
  return EXT[path.slice(dot + 1).toLowerCase()] ?? "";
}
