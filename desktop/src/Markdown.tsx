import { invoke } from "@tauri-apps/api/core";
import { openPath, openUrl } from "@tauri-apps/plugin-opener";
import { Check, Copy, ExternalLink, FileText } from "lucide-react";
import {
  Children,
  type ReactNode,
  cloneElement,
  createContext,
  isValidElement,
  memo,
  useContext,
  useState,
} from "react";
import ReactMarkdown from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import { CodeView } from "./CodeView";
import { t, useLang } from "./i18n";

async function openWithEditor(
  editor: string | undefined,
  abs: string,
  line?: number,
): Promise<void> {
  if (editor?.trim()) {
    await invoke("open_in_editor", { command: editor, path: abs, line: line ?? null });
    return;
  }
  await openPath(abs);
}

type TrackedFile = { path: string; status: string };

type WorkspaceCtx = { dir?: string; editor?: string; sessionFiles?: TrackedFile[]; currentToolPaths?: string[] };
const WorkspaceContext = createContext<WorkspaceCtx>({});
export const WorkspaceProvider = WorkspaceContext.Provider;

/**
 * Wrap children with a WorkspaceProvider that merges currentToolPaths
 * into the existing context (preserving dir/editor/sessionFiles).
 */
export function WithToolPaths({ toolPaths, children }: { toolPaths: string[]; children: React.ReactNode }) {
  const ctx = useContext(WorkspaceContext);
  return <WorkspaceProvider value={{ ...ctx, currentToolPaths: toolPaths }}>{children}</WorkspaceProvider>;
}

/**
 * Try to find a tracked session file whose path ends with the displayed text.
 * This handles cases where the AI writes a short/relative path in conversation
 * but the actual file lives at a different absolute path (which is known from
 * the tool call args in sessionFiles).
 */
/**
 * Try to match displayPath against a list of paths (exact → suffix).
 * Returns the best matching path, or null if none match.
 */
function matchPathList(
  displayPath: string,
  paths: string[],
  matchIndex: number = 0,
): string | null {
  if (!paths || paths.length === 0) return null;
  const normalizedDisplay = displayPath.replace(/\\/g, "/");
  let exact: string | null = null;
  const suffixMatches: string[] = [];

  for (const raw of paths) {
    const n = raw.replace(/\\/g, "/");
    if (n === normalizedDisplay) { exact = raw; continue; }
    if (n.endsWith("/" + normalizedDisplay) || n.endsWith("\\" + normalizedDisplay)) {
      suffixMatches.push(raw);
    }
  }

  // Exact match — always correct
  if (exact) return exact;

  // Unique suffix match
  if (suffixMatches.length === 1) return suffixMatches[0]!;

  // Ambiguous — use positional matching:
  // The Nth file pill maps to the Nth tool call path.
  if (suffixMatches.length > 1) {
    const idx = matchIndex % suffixMatches.length;
    return suffixMatches[idx]!;
  }

  return null;
}

function resolveBySessionFiles(
  displayPath: string,
  sessionFiles: TrackedFile[] | undefined,
  currentToolPaths?: string[],
  matchIndex: number = 0,
): string | null {
  // Step 1: Try the current message's tool paths first (most specific context)
  if (currentToolPaths && currentToolPaths.length > 0) {
    const local = matchPathList(displayPath, currentToolPaths, matchIndex);
    if (local) return local;
  }

  // Step 2: Try global session files
  if (!sessionFiles || sessionFiles.length === 0) return null;
  const globalPaths = sessionFiles.map((sf) => sf.path);
  return matchPathList(displayPath, globalPaths, matchIndex);
}

function resolveAgainstWorkspace(
  rel: string,
  ws: string | undefined,
  sessionFiles?: TrackedFile[],
  currentToolPaths?: string[],
  matchIndex: number = 0,
): string {
  // Step 1: Try suffix matching against tracked session files.
  // If found, use the tracked path as the base instead of the display text.
  const sessionResolved = resolveBySessionFiles(rel, sessionFiles, currentToolPaths, matchIndex);
  const basePath = sessionResolved ?? rel;

  // Step 2: If basePath is already absolute, return it normalized
  if (/^[a-zA-Z]:[\\/]/.test(basePath) || basePath.startsWith("/")) {
    return ws?.includes("\\") ? basePath.replace(/\//g, "\\") : basePath;
  }

  // Step 3: Resolve relative basePath against workspace
  if (!ws) return basePath;
  const isWindows = ws.includes("\\");
  const sep = isWindows ? "\\" : "/";
  const trimmed = ws.replace(/[\\/]$/, "");
  const relative = basePath.replace(/^\.[\\/]/, "").replace(/\//g, sep);
  return `${trimmed}${sep}${relative}`;
}

const KNOWN_EXTS =
  "ts|tsx|mts|cts|js|jsx|mjs|cjs|py|pyi|rs|go|json|jsonc|md|mdx|css|scss|less|html|htm|xml|svg|yaml|yml|toml|sh|bash|zsh|fish|sql|rb|java|kt|swift|c|cpp|cc|cxx|h|hpp|hxx|cs|php|lua|dart|ex|exs|erl|hs|clj|cljs|zig|vue|svelte|graphql|gql|proto";
const PATH_SEG = "[\\w.@()+~%#=-]+";
const FILE_NAME_SOURCE = `${PATH_SEG}\\.(?:${KNOWN_EXTS})`;
const FILE_REF_SOURCE = [
  `[A-Za-z]:[\\\\/](?:${PATH_SEG}[\\\\/])*${FILE_NAME_SOURCE}`,
  `/(?:${PATH_SEG}[\\\\/])*${FILE_NAME_SOURCE}`,
  `(?:\\.{1,2}[\\\\/])?(?:${PATH_SEG}[\\\\/])+${FILE_NAME_SOURCE}`,
  FILE_NAME_SOURCE,
].join("|");
const LINE_REF_SOURCE = "(?::(\\d+(?::\\d+)?(?:-\\d+)?))?";
// No lookbehind here — Tauri's WKWebView on macOS Monterey (Safari < 16.4)
// can't parse `(?<=...)` and the whole bundle fails to load with an
// "invalid group specifier name" error. Capture the leading char as
// group 1 instead and let splitFilePaths skip past it. Issue #1209.
const FILE_PATH_RE = new RegExp(
  `(^|[\\s\`'"(\\[])(${FILE_REF_SOURCE})${LINE_REF_SOURCE}(?=[\\s.,;!?\\(\\[\\]\\)'"\`]|$)`,
  "g",
);
const EXACT_FILE_REF_RE = new RegExp(`^(${FILE_REF_SOURCE})${LINE_REF_SOURCE}$`);

type ParsedFileRef = { path: string; line?: string };

function firstLine(line?: string): number | undefined {
  if (!line) return undefined;
  const parsed = Number.parseInt(line.split(/[:-]/)[0] ?? line, 10);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function parseFileRef(value: string): ParsedFileRef | null {
  const trimmed = value.trim();
  const m = EXACT_FILE_REF_RE.exec(trimmed);
  if (!m) return null;
  return { path: m[1]!, line: m[2] };
}

function decodeMaybeUri(value: string): string {
  try {
    return decodeURI(value);
  } catch {
    return value;
  }
}

function stripFileScheme(value: string): string {
  if (!/^file:\/\//i.test(value)) return value;
  let raw = decodeMaybeUri(value.replace(/^file:\/\//i, ""));
  if (/^\/[a-zA-Z]:[\\/]/.test(raw)) raw = raw.slice(1);
  return raw;
}

function protocolScheme(value: string): string | null {
  if (/^[a-zA-Z]:[\\/]/.test(value)) return null;
  return /^([a-z][\w+.-]*):/i.exec(value)?.[1]?.toLowerCase() ?? null;
}

function parseFileHref(value: string): ParsedFileRef | null {
  const stripped = stripFileScheme(value);
  const decoded = decodeMaybeUri(stripped);
  const hashLine = /#L?(\d+)/i.exec(decoded)?.[1];
  const clean = decoded.split("#")[0]!.split("?")[0]!;
  const parsed = parseFileRef(clean);
  if (!parsed) return null;
  return { ...parsed, line: parsed.line ?? hashLine };
}

function FilePill({ path, line, matchIndex }: { path: string; line?: string; matchIndex: number }) {
  useLang();
  const ctx = useContext(WorkspaceContext);
  const [done, setDone] = useState<"open" | "copy" | null>(null);
  const display = line ? `${path}:${line}` : path;
  const abs = resolveAgainstWorkspace(path, ctx.dir, ctx.sessionFiles, ctx.currentToolPaths, matchIndex);
  const openInEditor = async () => {
    try {
      await openWithEditor(ctx.editor, abs, firstLine(line));
      setDone("open");
      setTimeout(() => setDone(null), 1200);
    } catch {
      try {
        await navigator.clipboard.writeText(display);
        setDone("copy");
        setTimeout(() => setDone(null), 1200);
      } catch {
        /* ignore */
      }
    }
  };
  const copyOnly = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(display);
      setDone("copy");
      setTimeout(() => setDone(null), 1200);
    } catch {
      /* ignore */
    }
  };
  return (
    <span
      className={`file-pill ${done ? "done" : ""}`}
      role="button"
      tabIndex={0}
      onClick={openInEditor}
      onContextMenu={(e) => {
        e.preventDefault();
        void copyOnly(e);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          void openInEditor();
        }
      }}
      title={t("markdown.filePillTitle")}
    >
      <FileText size={10} className="file-pill-icon" />
      <span className="file-pill-path">{path}</span>
      {line && <span className="file-pill-line">:{line}</span>}
      {done && <Check size={10} className="file-pill-check" />}
    </span>
  );
}

function splitFilePaths(text: string): ReactNode[] | string {
  FILE_PATH_RE.lastIndex = 0;
  const out: ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null = FILE_PATH_RE.exec(text);
  while (m !== null) {
    const prefix = m[1] ?? "";
    const path = m[2]!;
    const line = m[3];
    const pillStart = m.index + prefix.length;
    if (pillStart > last) out.push(text.slice(last, pillStart));
    out.push(<FilePill key={`fp-${pillStart}`} path={path} line={line} matchIndex={_filePillIndex} />);
    _filePillIndex++;
    last = pillStart + path.length + (line ? line.length + 1 : 0);
    m = FILE_PATH_RE.exec(text);
  }
  if (out.length === 0) return text;
  if (last < text.length) out.push(text.slice(last));
  return out;
}

type AnyProps = { children?: ReactNode } & Record<string, unknown>;

function withFilePills(children: ReactNode): ReactNode {
  return Children.map(children, (child) => {
    if (typeof child === "string") return splitFilePaths(child);
    if (isValidElement(child)) {
      if (typeof child.type === "string" && ["a", "code", "pre"].includes(child.type)) {
        return child;
      }
      const props = child.props as AnyProps;
      if (props.children !== undefined) {
        return cloneElement(child, undefined, withFilePills(props.children));
      }
    }
    return child;
  });
}

/**
 * Convert bracket-style math delimiters to dollar-style so they survive
 * the markdown parser (which would otherwise consume the backslash in `\[`
 * as an escape). Handles both display math \[...\] → $$...$$ and inline
 * math \(...\) → $...$.
 *
 * Protects `\\[` sequences (LaTeX line-break spacing like `\\[4pt]`)
 * from being mangled — the regex must not match the `\[` inside `\\[`.
 */
function normalizeMathDelimiters(source: string): string {
  // Protect LaTeX line-break spacing \\[ ... ] — use a sentinel that
  // can't appear in normal text.
  const LB = "\x00LB\x00";
  let result = source.replace(/\\\\\[/g, LB);
  result = result
    // display math: \[ ... \] → $$ ... $$
    .replace(/\\\[/g, "$$$$")
    .replace(/\\\]/g, "$$$$")
    // inline math: \( ... \) → $ ... $
    .replace(/\\\(/g, "$$")
    .replace(/\\\)/g, "$$");
  // Restore protected sequences
  result = result.replace(/\x00LB\x00/g, "\\\\[");

  // Replace | with \vert inside math to prevent GFM table column splitting.
  // \vert renders identically to | in KaTeX — it's the same vertical-bar
  // glyph — but the markdown parser won't mistake it for a table separator.
  result = result.replace(
    /\$\$([\s\S]*?)\$\$/g,
    (_: string, m: string) => `$$${m.replace(/\|/g, "\\vert ")}$$`,
  );
  result = result.replace(
    /\$([^$\n]+)\$/g,
    (_: string, m: string) => `$${m.replace(/\|/g, "\\vert ")}$`,
  );

  return result;
}

let _filePillIndex = 0;

export const Markdown = memo(function Markdown({ source }: { source: string }) {
  _filePillIndex = 0;
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkBreaks, remarkMath]}
        rehypePlugins={[[rehypeKatex, { throwOnError: false }]]}
        components={{
          pre: ({ children }) => {
            // react-markdown v9 nests children unpredictably — flatten all text.
            const rawText = flattenChildText(children).trimEnd();
            return <CodeBlock lang={extractFencedLang(children)} text={rawText} />;
          },
          code: ({ className, children }) => {
            const text = String(children ?? "");
            const parsed = !className ? parseFileRef(text.trim()) : null;
            if (parsed) return <FilePill path={parsed.path} line={parsed.line} matchIndex={_filePillIndex++} />;
            return <code className={className}>{children}</code>;
          },
          a: ({ href, children }) => <SafeLink href={href}>{children}</SafeLink>,
          p: ({ children }) => <p>{withFilePills(children)}</p>,
          li: ({ children }) => <li>{withFilePills(children)}</li>,
          table: ({ children }) => (
            <div className="markdown-table-wrap">
              <table>{children}</table>
            </div>
          ),
          td: ({ children }) => <td>{withFilePills(children)}</td>,
        }}
      >
        {normalizeMathDelimiters(source)}
      </ReactMarkdown>
    </div>
  );
});

function SafeLink({ href, children }: { href?: string; children: ReactNode }) {
  useLang();
  const ctx = useContext(WorkspaceContext);
  const [done, setDone] = useState(false);
  const scheme = href ? protocolScheme(href) : null;
  const isExternal = !!scheme && scheme !== "file";
  const onClick = async (e: React.MouseEvent) => {
    e.preventDefault();
    if (!href) return;
    if (isExternal) {
      try {
        await openUrl(href);
      } catch {
        window.open(href, "_blank", "noopener,noreferrer");
      }
      return;
    }
    try {
      const parsed = parseFileHref(href);
      const target = parsed ?? { path: decodeMaybeUri(stripFileScheme(href)) };
      const abs = resolveAgainstWorkspace(target.path, ctx.dir, ctx.sessionFiles, ctx.currentToolPaths, 0);
      await openWithEditor(ctx.editor, abs, firstLine(target.line));
    } catch {
      try {
        await navigator.clipboard.writeText(href);
        setDone(true);
        setTimeout(() => setDone(false), 1200);
      } catch {
        /* ignore */
      }
    }
  };
  return (
    <a
      href={href ?? "#"}
      onClick={onClick}
      className={`md-link ${isExternal ? "external" : "local"} ${done ? "done" : ""}`}
      title={
        isExternal
          ? t("markdown.externalLinkTitle", { href: href ?? "" })
          : t("markdown.localLinkTitle", { href: href ?? "" })
      }
    >
      {children}
      {isExternal ? (
        <ExternalLink size={10} className="md-link-icon" />
      ) : done ? (
        <Check size={10} className="md-link-icon" />
      ) : null}
    </a>
  );
}

export function extractFencedLang(children: ReactNode): string {
  for (const kid of Children.toArray(children)) {
    if (isValidElement(kid)) {
      const cls = (kid.props as Record<string, unknown>).className;
      if (typeof cls === "string") {
        const m = cls.match(/language-([\w-]+)/);
        if (m) return m[1]!;
      }
    }
  }
  return "text";
}

function flattenChildText(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(flattenChildText).join("");
  if (isValidElement(node))
    return flattenChildText((node.props as { children?: ReactNode }).children);
  return "";
}

function CodeBlock({ lang, text }: { lang: string; text: string }): ReactNode {
  useLang();
  const [copied, setCopied] = useState(false);
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch {
      /* ignore */
    }
  };
  return (
    <div className="codeblock">
      <div className="codeblock-head">
        <span className="codeblock-lang">{lang}</span>
        <span className="codeblock-copy-wrap">
          <button type="button" className={`copy-btn ${copied ? "done" : ""}`} onClick={onCopy}>
            {copied ? <Check size={11} /> : <Copy size={11} />}
          </button>
        </span>
      </div>
      <CodeView text={text} lang={lang} />
    </div>
  );
}
