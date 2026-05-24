import { invoke } from "@tauri-apps/api/core";
import { openPath, openUrl } from "@tauri-apps/plugin-opener";
import { Check, Copy, ExternalLink, FileText } from "lucide-react";
import {
  Children,
  cloneElement,
  createContext,
  isValidElement,
  memo,
  type ReactElement,
  type ReactNode,
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
  if (editor && editor.trim()) {
    await invoke("open_in_editor", { command: editor, path: abs, line: line ?? null });
    return;
  }
  await openPath(abs);
}

type WorkspaceCtx = { dir?: string; editor?: string };
const WorkspaceContext = createContext<WorkspaceCtx>({});
export const WorkspaceProvider = WorkspaceContext.Provider;

function resolveAgainstWorkspace(rel: string, ws: string | undefined): string {
  if (!ws) return rel;
  if (/^[a-zA-Z]:[\\/]/.test(rel) || rel.startsWith("/")) return rel;
  const sep = ws.includes("\\") ? "\\" : "/";
  const trimmed = ws.replace(/[\\/]$/, "");
  return `${trimmed}${sep}${rel.replace(/^\.[\\/]/, "")}`;
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
  `(^|[\\s\`'"(\\[])(${FILE_REF_SOURCE})${LINE_REF_SOURCE}(?=[\\s.,;!?\\]\\)'"\`]|$)`,
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

function FilePill({ path, line }: { path: string; line?: string }) {
  useLang();
  const ctx = useContext(WorkspaceContext);
  const [done, setDone] = useState<"open" | "copy" | null>(null);
  const display = line ? `${path}:${line}` : path;
  const openInEditor = async () => {
    try {
      const abs = resolveAgainstWorkspace(path, ctx.dir);
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
    out.push(<FilePill key={`fp-${pillStart}`} path={path} line={line} />);
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

export const Markdown = memo(function Markdown({ source }: { source: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkBreaks, remarkMath]}
        rehypePlugins={[[rehypeKatex, { throwOnError: false }]]}
        components={{
          pre: ({ children }) => {
            // react-markdown v9 nests children unpredictably — flatten all text.
            const rawText = flattenChildText(children).trimEnd();
            const kids = Children.toArray(children);
            const codeEl = kids.find(
              (c): c is ReactElement =>
                isValidElement(c) && typeof c.type === "string" && c.type.toLowerCase() === "code",
            );
            const lang = codeEl
              ? /language-([\w-]+)/.exec(
                  (codeEl.props as Record<string, unknown>).className as string ?? "",
                )?.[1] ?? "text"
              : "text";
            return <CodeBlock lang={lang} text={rawText} />;
          },
          code: ({ className, children }) => {
            const text = String(children ?? "");
            const parsed = !className ? parseFileRef(text.trim()) : null;
            if (parsed) return <FilePill path={parsed.path} line={parsed.line} />;
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
        {source}
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
      const abs = resolveAgainstWorkspace(target.path, ctx.dir);
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

/** Recursively extract plain text from React children (handles nested elements, fragments, etc.). */
function flattenChildText(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(flattenChildText).join("");
  if (isValidElement(node)) return flattenChildText((node.props as { children?: ReactNode }).children);
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
            {copied ? t("markdown.copied") : t("markdown.copy")}
          </button>
        </span>
      </div>
      <CodeView text={text} lang={lang} />
    </div>
  );
}
