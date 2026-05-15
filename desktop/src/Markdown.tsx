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
import remarkGfm from "remark-gfm";
import { CodeView } from "./CodeView";

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
const FILE_PATH_RE = new RegExp(
  `(?:^|(?<=[\\s\`'"(\\[]))((?:[\\w.-]+\\/)+[\\w.-]+\\.(?:${KNOWN_EXTS}))(?::(\\d+(?:-\\d+)?))?(?=[\\s.,;!?\\]\\)'"\`]|$)`,
  "g",
);

function FilePill({ path, line }: { path: string; line?: string }) {
  const ctx = useContext(WorkspaceContext);
  const [done, setDone] = useState<"open" | "copy" | null>(null);
  const display = line ? `${path}:${line}` : path;
  const openInEditor = async () => {
    try {
      const abs = resolveAgainstWorkspace(path, ctx.dir);
      const lineNum = line ? Number.parseInt(line.split("-")[0] ?? line, 10) : undefined;
      await openWithEditor(ctx.editor, abs, Number.isFinite(lineNum) ? lineNum : undefined);
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
      title="click to open · right-click to copy"
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
    if (m.index > last) out.push(text.slice(last, m.index));
    out.push(<FilePill key={`fp-${m.index}`} path={m[1]!} line={m[2]} />);
    last = m.index + m[0].length;
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
        remarkPlugins={[remarkGfm]}
        components={{
          pre: ({ children }) => {
            const codeEl = Children.toArray(children).find(
              (c): c is ReactElement<{ className?: string; children?: ReactNode }> =>
                isValidElement(c) && c.type === "code",
            );
            if (!codeEl) return <pre>{children}</pre>;
            const text = String(codeEl.props.children ?? "").replace(/\n$/, "");
            const lang = /language-([\w-]+)/.exec(codeEl.props.className ?? "")?.[1] ?? "text";
            return <CodeBlock lang={lang} text={text} />;
          },
          code: ({ className, children }) => <code className={className}>{children}</code>,
          a: ({ href, children }) => <SafeLink href={href}>{children}</SafeLink>,
          p: ({ children }) => <p>{withFilePills(children)}</p>,
          li: ({ children }) => <li>{withFilePills(children)}</li>,
          td: ({ children }) => <td>{withFilePills(children)}</td>,
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  );
});

function SafeLink({ href, children }: { href?: string; children: ReactNode }) {
  const ctx = useContext(WorkspaceContext);
  const [done, setDone] = useState(false);
  const isExternal = !!href && /^https?:\/\//i.test(href);
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
      const stripped = href.replace(/^file:\/\//, "");
      const abs = resolveAgainstWorkspace(stripped, ctx.dir);
      await openWithEditor(ctx.editor, abs);
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
      title={isExternal ? `open ${href} in browser` : `open ${href}`}
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

function CodeBlock({ lang, text }: { lang: string; text: string }): ReactNode {
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
        <button type="button" className={`copy-btn ${copied ? "done" : ""}`} onClick={onCopy}>
          {copied ? <Check size={11} /> : <Copy size={11} />}
          {copied ? "copied" : "copy"}
        </button>
      </div>
      <CodeView text={text} lang={lang} />
    </div>
  );
}
