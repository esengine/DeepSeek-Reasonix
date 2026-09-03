// markdownComponents — the shared components map for both Markdown render
// paths: react-markdown (streaming) and the worker-parsed block renderer
// (history). Kept in its own CSS-free module so the worker-path renderer and
// plain-tsx tests can import it without pulling the katex stylesheet.
//
// Fenced code blocks go through CodeViewer for syntax highlighting; inline
// code is a styled <code>. Mermaid fences lazy-load the diagram renderer.
// Links open in the system browser via RichMarkdownLink. Oversized tables
// virtualize their body rows.

import { lazy, Suspense } from "react";
import type { Components } from "react-markdown";
import { CodeViewer } from "./CodeViewer";
import { RichMarkdownLink } from "./githubLink";
import { MarkdownTable } from "./MarkdownTable";
import { MarkdownImage } from "./MarkdownImage";

const MermaidDiagram = lazy(() => import("./MermaidDiagram"));

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
  if (text.trim() === "") return null;
  const { preText, statusItems } = splitPlainBlock(text);
  const asList = statusItems.length >= 2;
  return (
    <div className={`md-plain-block${asList ? " md-plain-block--split" : " md-plain-block--pre"}`}>
      <CodeViewer value={text} scrollMode="bounded" maxHeight="min(60vh, 28rem)" />
      {asList && preText && (
        <div className="md-plain-block__diagram">
          <CodeViewer value={preText} scrollMode="bounded" maxHeight="min(60vh, 28rem)" />
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

// The components map is shared by the main-thread react-markdown renderer
// (streaming path) and the worker-parsed block renderer (history path), so
// both produce byte-identical DOM for the same document.
export function createComponents(plainStatusBlocks: boolean): Components {
  return {
    pre: ({ children }) => <>{children}</>,
    table: ({ children }) => <MarkdownTable>{children}</MarkdownTable>,
    code: ({ className, children }) => {
      const text = String(children ?? "");
      const match = /language-([\w-]+)/.exec(className ?? "");
      const lang = match?.[1];
      const isBlock = match !== null || text.includes("\n");
      if (isBlock) {
        const value = text.replace(/\n$/, "");
        // An empty fence is a formatting placeholder; a bordered one-line
        // CodeViewer would otherwise become a phantom row in every surface.
        if (value.trim() === "") return null;
        if (lang === "mermaid") {
          return (
            <Suspense fallback={<CodeViewer value={value} language="mermaid" scrollMode="bounded" maxHeight="min(60vh, 28rem)" />}>
              <MermaidDiagram definition={value} />
            </Suspense>
          );
        }
        if (!match && plainStatusBlocks) return <PlainMarkdownBlock text={text.replace(/\n$/, "")} />;
        return <CodeViewer value={value} language={lang} scrollMode="bounded" maxHeight="min(60vh, 28rem)" />;
      }
      return <code className="md-code">{children}</code>;
    },
    a: ({ href, children }) => <RichMarkdownLink href={href}>{children}</RichMarkdownLink>,
    img: ({ src, alt, title }) => <MarkdownImage src={src} alt={alt} title={title} />,
  };
}
