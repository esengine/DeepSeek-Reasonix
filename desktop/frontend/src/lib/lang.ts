// Pure language resolution — no highlight.js dependency. Non-lazy consumers (e.g.
// ToolCard) guess a language from a path without pulling the highlighter into the
// main bundle; highlight.js stays behind the lazy editor seam. highlight.ts
// imports ALIASES from here and adds the hljs-backed validation.

export const ALIASES: Record<string, string> = {
  // JavaScript ecosystem
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  coffee: "coffeescript",
  // Shell
  sh: "bash",
  shell: "bash",
  zsh: "bash",
  // Python
  py: "python",
  // Rust
  rs: "rust",
  // YAML
  yml: "yaml",
  // XML / markup dialects — no native hljs lexer for XAML, so map to XML
  html: "xml",
  xaml: "xml",
  // Markdown
  md: "markdown",
  // C family
  cs: "csharp",
  "c#": "csharp",
  cxx: "cpp",
  cc: "cpp",
  h: "c",
  hpp: "cpp",
  // Ruby
  rb: "ruby",
  // Kotlin
  kt: "kotlin",
  kts: "kotlin",
  // PowerShell
  ps1: "powershell",
  psd1: "powershell",
  psm1: "powershell",
  // Config / data
  toml: "ini",
  // Docker / build
  dockerfile: "dockerfile",
  makefile: "makefile",
  // Objective-C
  objc: "objectivec",
  // F#
  "f#": "fsharp",
  // Perl
  pm: "perl",
  // Haskell
  lhs: "haskell",
  // Erlang
  hrl: "erlang",
  // Clojure
  cljc: "clojure",
  cljs: "clojure",
  // Julia
  jl: "julia",
  // R
  r: "r",
  // LaTeX
  tex: "latex",
  ltx: "latex",
  // SCSS
  scss: "scss",
  // Less
  less: "less",
  // Vim
  vim: "vim",
  // Nginx
  nginx: "nginx",
  // Apache
  apache: "apache",
  // Protobuf
  proto: "protobuf",
  // GraphQL
  graphql: "graphql",
  gql: "graphql",
  // CMake
  cmake: "cmake",
  // Gradle
  gradle: "gradle",
  // Properties
  properties: "properties",
  // Groovy
  groovy: "groovy",
  gvy: "groovy",
  // Lua
  lua: "lua",
  // Dart
  dart: "dart",
  // MATLAB
  matlab: "matlab",
};

// Keep these values as semantic fence tags rather than highlight.js lexer names.
// UI consumers pass them through resolveLang(), while plain-text consumers (such
// as plan revisions) preserve useful distinctions like TSX vs TypeScript and
// XAML vs XML for the model.
export const FILE_LANGUAGE_BY_EXTENSION: Readonly<Record<string, string>> = {
  // JavaScript
  go: "go",
  ts: "typescript",
  tsx: "tsx",
  js: "javascript",
  jsx: "jsx",
  mjs: "javascript",
  cjs: "javascript",
  coffee: "coffeescript",
  // Data
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  xml: "xml",
  xaml: "xaml",
  html: "html",
  // Shell
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  // Python
  py: "python",
  // Rust
  rs: "rust",
  // CSS
  css: "css",
  scss: "scss",
  less: "less",
  // Markdown
  md: "markdown",
  // C family
  cs: "csharp",
  c: "c",
  cpp: "cpp",
  cc: "cpp",
  cxx: "cpp",
  h: "c",
  hpp: "cpp",
  // Java / JVM
  java: "java",
  kt: "kotlin",
  kts: "kotlin",
  scala: "scala",
  groovy: "groovy",
  gvy: "groovy",
  gradle: "gradle",
  // .NET
  fs: "fsharp",
  fsx: "fsharp",
  vb: "vbnet",
  // Database
  sql: "sql",
  // Scripting
  rb: "ruby",
  php: "php",
  lua: "lua",
  dart: "dart",
  perl: "perl",
  pl: "perl",
  pm: "perl",
  r: "r",
  // Apple
  swift: "swift",
  m: "objectivec",
  mm: "objectivec",
  // Functional
  haskell: "haskell",
  hs: "haskell",
  lhs: "haskell",
  elixir: "elixir",
  ex: "elixir",
  exs: "elixir",
  clojure: "clojure",
  clj: "clojure",
  cljs: "clojure",
  cljc: "clojure",
  erlang: "erlang",
  erl: "erlang",
  hrl: "erlang",
  fsharp: "fsharp",
  // Scientific
  julia: "julia",
  jl: "julia",
  matlab: "matlab",
  // Shell / config
  ps1: "powershell",
  psd1: "powershell",
  psm1: "powershell",
  dockerfile: "dockerfile",
  nginx: "nginx",
  properties: "properties",
  protobuf: "protobuf",
  proto: "protobuf",
  graphql: "graphql",
  gql: "graphql",
  toml: "toml",
  makefile: "makefile",
  cmake: "cmake",
  // Document
  latex: "latex",
  tex: "latex",
  ltx: "latex",
  // Editor
  vim: "vim",
};

export const FILE_LANGUAGE_BY_NAME: Readonly<Record<string, string>> = {
  ".htaccess": "apache",
  "cmakelists.txt": "cmake",
  containerfile: "dockerfile",
  dockerfile: "dockerfile",
  "httpd.conf": "apache",
  gnumakefile: "makefile",
  makefile: "makefile",
  "nginx.conf": "nginx",
};

// pathToLang infers a semantic language tag from either a special basename or
// a file extension. Matching both slash styles keeps desktop previews and tool
// diffs consistent when a Windows path reaches the frontend.
export function pathToLang(path: string): string {
  const name = path.split(/[\\/]/).filter(Boolean).pop()?.toLowerCase() ?? "";
  if (!name) return "";

  const exact = FILE_LANGUAGE_BY_NAME[name];
  if (exact) return exact;
  if (/^(?:dockerfile|containerfile)\./.test(name)) return "dockerfile";
  if (/^(?:gnu)?makefile\./.test(name)) return "makefile";

  const dot = name.lastIndexOf(".");
  if (dot < 0) return "";
  return FILE_LANGUAGE_BY_EXTENSION[name.slice(dot + 1)] ?? "";
}

// Backward-compatible name for the existing tool-diff callers.
export const extToLang = pathToLang;
