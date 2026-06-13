import { memo, useDeferredValue, useLayoutEffect, useRef } from "react";
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

const STREAMING_CURSOR_CLASS = "cursor";

/**
 * Find the deepest last-element child of `container` that can hold inline
 * content.  Walks `lastElementChild` recursively, stopping at `<pre>` blocks
 * and void elements.  O(depth) — far cheaper than a full tree walker.
 */
function deepestLastInlineElement(container: HTMLElement): HTMLElement {
  let target: HTMLElement = container;
  while (target.lastElementChild) {
    const last = target.lastElementChild as HTMLElement;
    const tag = last.tagName;
    if (tag === "PRE" || tag === "BR" || tag === "HR" || tag === "IMG") break;
    target = last;
  }
  return target;
}

// Inject a blinking cursor span at the end of the last inline content node
// inside the container, skipping code blocks entirely.  Called from
// useLayoutEffect so the cursor appears synchronously before paint.
//
// Optimisation: during streaming the cursor is usually already in the right
// place (React updates text in-place within the same element), so we check
// position first and skip the DOM mutation entirely when nothing moved.
function injectStreamingCursor(container: HTMLElement): void {
  const target = deepestLastInlineElement(container);

  // Check whether the cursor is already correctly positioned.
  const existing = container.querySelector(`.${STREAMING_CURSOR_CLASS}`);
  if (existing && existing.parentElement === target && target.lastChild === existing) {
    return; // still at the end — nothing to do.
  }

  // Remove stale cursor (if any).
  if (existing) existing.remove();

  // Insert at the new position.
  const cursor = document.createElement("span");
  cursor.className = STREAMING_CURSOR_CLASS;
  cursor.dataset.streamingCursor = "true";
  target.appendChild(cursor);
}

function removeStreamingCursor(container: HTMLElement): void {
  container
    .querySelectorAll(`.${STREAMING_CURSOR_CLASS}`)
    .forEach((el) => el.remove());
}

const components: Components = {
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const text = String(children ?? "");
    const match = /language-([\w-]+)/.exec(className ?? "");
    const isBlock = match !== null || text.includes("\n");
    if (isBlock) {
      return <CodeViewer value={text.replace(/\n$/, "")} language={match?.[1]} maxHeight={360} />;
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
};

export const Markdown = memo(function Markdown({
  text,
  showCursor,
}: {
  text: string;
  showCursor?: boolean;
}) {
  const deferred = useDeferredValue(text);
  const containerRef = useRef<HTMLDivElement>(null);

  // Inject / remove cursor after every React render cycle so the cursor
  // always sits at the tail of the current streaming content — without
  // ever touching the raw Markdown string that ReactMarkdown parses.
  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    if (showCursor) {
      injectStreamingCursor(el);
    } else {
      removeStreamingCursor(el);
    }
  });

  return (
    <div className="md" ref={containerRef}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={components}
      >
        {normalizeMath(deferred)}
      </ReactMarkdown>
    </div>
  );
});
