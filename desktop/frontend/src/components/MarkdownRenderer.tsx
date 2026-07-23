import { lazy, memo, Suspense, useMemo, useRef } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import "katex/dist/katex.min.css";
import { CodeViewer } from "./CodeViewer";
import { normalizeMath } from "./mathNormalize";
import { openExternal } from "../lib/bridge";
import { markdownImageSource } from "../lib/markdownImage";

const MermaidDiagram = lazy(() => import("./MermaidDiagram"));

// Markdown rendering via react-markdown + remark-gfm (tables, task lists,
// strike, autolinks) and remark-math + rehype-katex for $/$$ KaTeX math.
// Fenced code blocks go through CodeViewer for syntax highlighting; inline
// code is a styled <code>. Links open in the system browser.
//
// The math pre-pass in mathNormalize normalises LLM-native \(…\)/\[…\]
// delimiters to the $/$$ syntax remark-math understands, gates single-$
// pairs through a classifier to avoid false positives on $5, $PATH, etc.,
// and runs KaTeX-specific normalisations (text-mode escapes, |→\vert).

const STATUS_MARKER_RE = /(?:✅|☑|☒|✔️?|✓|\[[xX ]\])/;
const STATUS_MARKER_GLOBAL_RE = /(?:✅|☑|☒|✔️?|✓|\[[xX ]\])/g;
const BULLET_RE = /^[-*•]\s+\S/;
const DIVIDER_RE = /^[\s\-_=─━—]+$/;

function splitStatusLine(line: string): string[] {
  const parts = (line.match(STATUS_MARKER_GLOBAL_RE) ?? []).length > 1
    ? line.split(/(?=(?:✅|☑|☒|✔️?|✓|\[[xX ]\]))/)
    : [line];
  return parts
    .map((part) => part.replace(/^(?:✅|☑|☒|✔️?|✓|\[[xX ]\]|[-*•])\s*/i, "").trim())
    .filter(Boolean)
    .map((part) => part.replace(/\s{2,}/g, " · "));
}

function looksLikeDiagram(text: string): boolean {
  return /[←→↔]|<{1,2}-{2,}|-{2,}>{1,2}|[-_=─━]{6,}/.test(text);
}

function splitPlainBlock(text: string): { preText: string; statusItems: string[] } {
  const items: string[] = [];
  const preLines: string[] = [];
  const lines = text.split(/\r?\n/);
  const bulletLines = lines.filter((line) => BULLET_RE.test(line.trim())).length;
  const collectBulletLines = bulletLines >= 2 && !looksLikeDiagram(text);
  for (const rawLine of lines) {
    const line = rawLine.trim();
    const marked = STATUS_MARKER_RE.test(line) || (collectBulletLines && BULLET_RE.test(line));
    if (marked) {
      items.push(...splitStatusLine(line));
    } else if (DIVIDER_RE.test(line) && items.length > 0 && !looksLikeDiagram(text)) {
      continue;
    } else {
      preLines.push(rawLine);
    }
  }
  while (preLines.length > 0 && preLines[0].trim() === "") preLines.shift();
  while (preLines.length > 0 && preLines[preLines.length - 1].trim() === "") preLines.pop();
  return { preText: preLines.join("\n"), statusItems: items };
}

function PlainMarkdownBlock({ text }: { text: string }) {
  const { preText, statusItems } = splitPlainBlock(text);
  const asList = statusItems.length >= 2;
  return (
    <div className={`md-plain-block${asList ? " md-plain-block--split" : " md-plain-block--pre"}`}>
      <CodeViewer value={text} maxHeight={360} />
      {asList && preText && (
        <div className="md-plain-block__diagram">
          <CodeViewer value={preText} maxHeight={360} />
        </div>
      )}
      {asList && (
        <div className="md-status-list">
          {statusItems.map((item, index) => (
            <div className="md-status-list__item" key={`${index}-${item}`}>
              <span className="md-status-list__dot" aria-hidden="true" />
              <span className="md-status-list__text">{item}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function createComponents(plainStatusBlocks: boolean): Components {
  return {
    pre: ({ children }) => <>{children}</>,
    code: ({ className, children }) => {
      const text = String(children ?? "");
      const match = /language-([\w-]+)/.exec(className ?? "");
      const lang = match?.[1];
      const isBlock = match !== null || text.includes("\n");
      if (isBlock) {
        const value = text.replace(/\n$/, "");
        if (lang === "mermaid") {
          return (
            <Suspense fallback={<CodeViewer value={value} language="mermaid" maxHeight={360} />}>
              <MermaidDiagram definition={value} />
            </Suspense>
          );
        }
        if (!match && plainStatusBlocks) return <PlainMarkdownBlock text={text.replace(/\n$/, "")} />;
        return <CodeViewer value={value} language={lang} maxHeight={360} />;
      }
      return <code className="md-code">{children}</code>;
    },
    a: ({ href, children }) => (
      <a
        href={href}
        onClick={(e) => {
          e.preventDefault();
          if (href) openExternal(href);
        }}
        onAuxClick={(e) => {
          e.preventDefault();
          if (href) openExternal(href);
        }}
        onMouseDown={(e) => {
          if (e.button === 1) e.preventDefault();
        }}
      >
        {children}
      </a>
    ),
    img: ({ src, alt, title }) => (
      <img
        src={markdownImageSource(src)}
        alt={alt ?? ""}
        title={title}
        loading="lazy"
        referrerPolicy="no-referrer"
      />
    ),
  };
}

const MarkdownRenderer = memo(function MarkdownRenderer({
  text,
  plainStatusBlocks = false,
}: {
  text: string;
  plainStatusBlocks?: boolean;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mathContent = useMemo(() => normalizeMath(text), [text]);
  const components = useMemo(() => createComponents(plainStatusBlocks), [plainStatusBlocks]);
  return (
    <div className="md" ref={containerRef}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={components}
      >
        {mathContent}
      </ReactMarkdown>
    </div>
  );
});

export default MarkdownRenderer;
