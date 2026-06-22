import { memo, useMemo, useRef } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import "katex/dist/katex.min.css";
import { CodeViewer } from "./CodeViewer";
import { normalizeMath } from "./mathNormalize";
import { openExternal } from "../lib/bridge";

// Markdown rendering via react-markdown + remark-gfm (tables, task lists,
// strike, autolinks) and remark-math + rehype-katex for $/$$ KaTeX math.
// Fenced code blocks go through CodeViewer for syntax highlighting; inline
// code is a styled <code>. Links open in the system browser.
//
// The math pre-pass in mathNormalize normalises LLM-native \(…\)/\[…\]
// delimiters to the $/$$ syntax remark-math understands, gates single-$
// pairs through a classifier to avoid false positives on $5, $PATH, etc.,
// and runs KaTeX-specific normalisations (text-mode escapes, |→\vert).
//
// When onFileLinkClick is provided, bare file paths in the text (relative
// paths with common extensions such as src/foo.tsx, docs/sop/README.md) are
// detected and rendered as clickable links. Clicking them calls
// onFileLinkClick(path) instead of opening in the system browser.

const STATUS_MARKER_RE = /(?:✅|☑|☒|✔️?|✓|\[[xX ]\])/;
const STATUS_MARKER_GLOBAL_RE = /(?:✅|☑|☒|✔️?|✓|\[[xX ]\])/g;
const BULLET_RE = /^[-*•]\s+\S/;
const DIVIDER_RE = /^[\s\-_=─━—]+$/;

// Matches relative file paths, absolute file paths, and bare filenames with
// common extensions — used to detect bare file references in text and make them
// clickable. Three forms are supported:
//   - Relative:  src/foo.tsx, ./src/foo.tsx, ../config.yml
//   - Absolute:  /Users/me/project/REASONIX.md
//   - Bare name: README.md, go.mod (when no directory separator is present)
// All must end with a known source/doc extension and must not be preceded
// by a word character or `/` (to avoid matching inside URLs like
// https://github.com/org/repo/blob/main/README.md).
const FILE_PATH_RE = /(?<!\w)(?<!\/)(?<!\.)(?<!@)(?:\/[\w\p{L}.-]+(?:\/[\w\p{L}.\/-]+)*|[\w\p{L}.-]+\/[\w\p{L}.\/-]+|[\w\p{L}.-]+)\.(?:md|tsx?|json|jsx?|go|css|ya?ml|toml|sh|py|rs|mod|sum|s?css|less|html|svelte|vue)/giu;

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

function createComponents(plainStatusBlocks: boolean, onFileLinkClick?: (path: string) => void): Components {
  return {
    pre: ({ children }) => <>{children}</>,
    code: ({ className, children }) => {
      const text = String(children ?? "");
      const match = /language-([\w-]+)/.exec(className ?? "");
      const isBlock = match !== null || text.includes("\n");
      if (isBlock) {
        if (!match && plainStatusBlocks) return <PlainMarkdownBlock text={text.replace(/\n$/, "")} />;
        return <CodeViewer value={text.replace(/\n$/, "")} language={match?.[1]} maxHeight={360} />;
      }
      return <code className="md-code">{children}</code>;
    },
    a: ({ href, children }) => {
      const isFileLink = href != null && !/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(href);
      return (
        <a
          href={href}
          onClick={(e) => {
            e.preventDefault();
            if (href) {
              if (isFileLink && onFileLinkClick) {
                onFileLinkClick(href);
              } else {
                openExternal(href);
              }
            }
          }}
          onAuxClick={(e) => {
            e.preventDefault();
            if (href) {
              if (isFileLink && onFileLinkClick) {
                onFileLinkClick(href);
              } else {
                openExternal(href);
              }
            }
          }}
          onMouseDown={(e) => {
            if (e.button === 1) e.preventDefault();
          }}
        >
          {children}
        </a>
      );
    },
  };
}

/**
 * Detects bare file paths in text and wraps them as markdown links so
 * react-markdown renders them as clickable anchors. Skips content inside
 * fenced code blocks and inline code to avoid false positives.
 */
function linkifyFilePaths(text: string): string {
  // Protect fenced code blocks and inline code spans first
  const fences: string[] = [];
  let idx = 0;
  const protectedText = text
    // Save fenced code blocks (```...``` or ~~~...~~~)
    .replace(/(```[\s\S]*?```|~~~[\s\S]*?~~~)/g, (_m) => {
      const placeholder = `\x00CODEBLOCK${idx}\x00`;
      fences[idx++] = _m;
      return placeholder;
    })
    // Save unclosed fenced code blocks (streaming: closing fence not yet present)
    .replace(/(```[\s\S]*$)/g, (_m) => {
      const placeholder = `\x00CODEBLOCK${idx}\x00`;
      fences[idx++] = _m;
      return placeholder;
    })
    // Save inline code spans
    .replace(/(`[^`]*`)/g, (_m) => {
      const placeholder = `\x00CODEBLOCK${idx}\x00`;
      fences[idx++] = _m;
      return placeholder;
    })
    // Save existing markdown links [text](url)
    .replace(/\[([^\]]*)\]\(([^)]*)\)/g, (_m) => {
      const placeholder = `\x00CODEBLOCK${idx}\x00`;
      fences[idx++] = _m;
      return placeholder;
    });

  // Linkify bare file paths in unprotected text
  const linkified = protectedText.replace(FILE_PATH_RE, (full) => {
    return `[${full}](${full})`;
  });

  // Restore protected blocks
  return linkified.replace(/\x00CODEBLOCK(\d+)\x00/g, (_m, id) => fences[Number(id)] ?? _m);
}

const MarkdownRenderer = memo(function MarkdownRenderer({
  text,
  plainStatusBlocks = false,
  onFileLinkClick,
}: {
  text: string;
  plainStatusBlocks?: boolean;
  onFileLinkClick?: (path: string) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const linkifiedText = useMemo(() => (onFileLinkClick ? linkifyFilePaths(text) : text), [text, onFileLinkClick]);
  const mathContent = useMemo(() => normalizeMath(linkifiedText), [linkifiedText]);
  const components = useMemo(() => createComponents(plainStatusBlocks, onFileLinkClick), [plainStatusBlocks, onFileLinkClick]);
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
