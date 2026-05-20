import { Highlight, type PrismTheme } from "prism-react-renderer";
import { useState } from "react";

const THEME: PrismTheme = {
  plain: { color: "#dde1ea", backgroundColor: "transparent" },
  styles: [
    { types: ["comment", "prolog", "doctype", "cdata"], style: { color: "#6d6e80", fontStyle: "italic" } },
    { types: ["punctuation"], style: { color: "#a8a9b8" } },
    { types: ["property", "tag", "boolean", "number", "constant", "symbol", "deleted"], style: { color: "#fbbf24" } },
    { types: ["selector", "attr-name", "string", "char", "builtin", "inserted"], style: { color: "#86dcb1" } },
    { types: ["operator", "entity", "url"], style: { color: "#84b9e8" } },
    { types: ["atrule", "attr-value", "keyword"], style: { color: "#b4a8f0" } },
    { types: ["function", "class-name", "maybe-class-name"], style: { color: "#84b9e8", fontWeight: "500" } },
    { types: ["regex", "important", "variable"], style: { color: "#f0c062" } },
    { types: ["important", "bold"], style: { fontWeight: "bold" } },
    { types: ["italic"], style: { fontStyle: "italic" } },
  ],
};

export const PRISM_THEME = THEME;

const EXTS: Record<string, string> = {
  ts: "typescript", tsx: "tsx", mts: "typescript", cts: "typescript",
  js: "javascript", jsx: "jsx", mjs: "javascript", cjs: "javascript",
  py: "python", pyi: "python",
  rs: "rust",
  go: "go",
  json: "json", jsonc: "json",
  md: "markdown", mdx: "markdown",
  css: "css", scss: "scss", less: "less",
  html: "markup", htm: "markup", xml: "markup", svg: "markup",
  yaml: "yaml", yml: "yaml",
  toml: "toml",
  sh: "bash", bash: "bash", zsh: "bash", fish: "bash",
  sql: "sql",
  rb: "ruby",
  java: "java", kt: "kotlin",
  swift: "swift",
  c: "c", h: "c",
  cpp: "cpp", cc: "cpp", cxx: "cpp", hpp: "cpp", hxx: "cpp",
  cs: "csharp",
  php: "php",
  lua: "lua",
  dart: "dart",
  ex: "elixir", exs: "elixir",
  erl: "erlang",
  hs: "haskell",
  clj: "clojure", cljs: "clojure",
  zig: "zig",
  vue: "markup",
  svelte: "markup",
  graphql: "graphql", gql: "graphql",
  proto: "protobuf",
  dockerfile: "docker",
};

export function langFromPath(path: string | null | undefined): string | null {
  if (!path) return null;
  const base = path.split(/[\\/]/).pop() ?? "";
  if (base.toLowerCase() === "dockerfile") return "docker";
  if (base.toLowerCase() === "makefile") return "makefile";
  const m = /\.([a-z0-9]+)$/i.exec(base);
  if (!m) return null;
  return EXTS[m[1].toLowerCase()] ?? null;
}

export function CodeView({
  text,
  lang,
  startLine = 1,
  showLineNumbers = true,
}: {
  text: string;
  lang: string;
  startLine?: number;
  showLineNumbers?: boolean;
}) {
  return (
    <Highlight theme={THEME} code={text} language={lang}>
      {({ className, tokens, getLineProps, getTokenProps }) => (
        <pre className={`codeview ${className}`}>
          {tokens.map((line, i) => (
            <div key={i} {...getLineProps({ line })} className="codeview-line">
              {showLineNumbers && (
                <span className="codeview-line-num">{i + startLine}</span>
              )}
              <span className="codeview-line-content">
                {line.map((token, k) => (
                  <span key={k} {...getTokenProps({ token })} />
                ))}
              </span>
            </div>
          ))}
        </pre>
      )}
    </Highlight>
  );
}

export function CollapsibleCode({
  text,
  lang,
  startLine = 1,
  maxLines = 20,
}: {
  text: string;
  lang: string;
  startLine?: number;
  maxLines?: number;
}) {
  const [open, setOpen] = useState(false);
  const lines = text.split("\n");
  const tooLong = lines.length > maxLines;
  const shown = open || !tooLong ? text : lines.slice(0, maxLines).join("\n");
  return (
    <>
      <CodeView text={shown} lang={lang} startLine={startLine} />
      {tooLong && (
        <button type="button" className="tool-more" onClick={() => setOpen((v) => !v)}>
          {open ? "less" : `+ ${lines.length - maxLines} more lines`}
        </button>
      )}
    </>
  );
}
