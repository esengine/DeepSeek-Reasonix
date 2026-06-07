import {
  Children,
  cloneElement,
  isValidElement,
  memo,
  useDeferredValue,
  type ReactNode,
} from "react";
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

// Inline private-use sentinel that never appears in real LLM output. We
// append it to the text while streaming, then replace it with the actual
// cursor span inside the markdown renderers. That way the cursor lives
// *inside* the rendered markdown content (e.g. as the last inline token of
// the current paragraph/list item/table cell) instead of as a sibling of
// the whole <Markdown /> block — which is what the PR2 review required.
const CURSOR_SENTINEL = "\uE000";

function CursorSpan(): ReactNode {
  return <span className="cursor" data-streaming-cursor="true" />;
}

// Walk a children tree (string + element) and replace the CURSOR_SENTINEL
// with <CursorSpan />. Used by inline-aware custom renderers (p / li / td /
// th / inline code) so the cursor lands at the end of the current
// streaming text fragment.
function renderCursorChildren(children: ReactNode): ReactNode {
  return Children.map(children, (child) => {
    if (typeof child === "string") {
      const idx = child.indexOf(CURSOR_SENTINEL);
      if (idx < 0) return child;
      const before = child.slice(0, idx);
      const after = child.slice(idx + CURSOR_SENTINEL.length);
      // Recurse into `after` in case the LLM somehow re-emits the sentinel.
      return (
        <>
          {before}
          <CursorSpan />
          {renderCursorChildren(after)}
        </>
      );
    }
    if (isValidElement(child)) {
      // Drop the sentinel element itself if it ever leaks through.
      const props = child.props as { children?: ReactNode };
      if ((child.type as unknown) === CURSOR_SENTINEL) return null;
      // Preserve the wrapper element (<strong>, <em>, etc.) while recursing
      // into its children to find and replace the sentinel.
      return cloneElement(child, { children: renderCursorChildren(props.children) });
    }
    return child;
  });
}

// For block-level contexts where we don't want to render an extra cursor
// span (e.g. code blocks must stay pure text), just strip the sentinel.
function stripSentinel(children: ReactNode): ReactNode {
  return Children.map(children, (child) => {
    if (typeof child === "string") {
      const idx = child.indexOf(CURSOR_SENTINEL);
      if (idx < 0) return child;
      return child.slice(0, idx) + child.slice(idx + CURSOR_SENTINEL.length);
    }
    if (isValidElement(child)) {
      const props = child.props as { children?: ReactNode };
      if ((child.type as unknown) === CURSOR_SENTINEL) return null;
      return cloneElement(child, { children: stripSentinel(props.children) });
    }
    return child;
  });
}

const components: Components = {
  p: ({ children }) => <p>{renderCursorChildren(children)}</p>,
  li: ({ children }) => <li>{renderCursorChildren(children)}</li>,
  td: ({ children }) => <td>{renderCursorChildren(children)}</td>,
  th: ({ children }) => <th>{renderCursorChildren(children)}</th>,
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const text = String(stripSentinel(children ?? ""));
    const match = /language-([\w-]+)/.exec(className ?? "");
    const isBlock = match !== null || text.includes("\n");
    if (isBlock) {
      return <CodeViewer value={text.replace(/\n$/, "")} language={match?.[1]} maxHeight={360} />;
    }
    return <code className="md-code">{text}</code>;
  },
  a: ({ href, children }) => (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        if (href) openExternal(href);
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
  const withCursor = showCursor ? deferred + CURSOR_SENTINEL : deferred;
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={components}
      >
        {normalizeMath(withCursor)}
      </ReactMarkdown>
    </div>
  );
});
