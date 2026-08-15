import { memo, useEffect, useMemo, useState } from "react";
import type { EditorProps } from "../CodeViewer";
import {
  escapeHtml,
  highlightToHtml,
  IDLE_HIGHLIGHT_MIN_BYTES,
  shouldHighlightSource,
} from "../../lib/highlight";
import { scheduleIdleTask } from "../../lib/idleTask";
import { CopyButton } from "../CopyButton";

// HljsCode is the syntax-highlighted default behind the code editor seam. It
// renders highlight.js token markup into a <pre>; token colors live in styles.css
// (.hljs-*). To upgrade to a full editor, point CodeViewer.tsx's lazy import at a
// Monaco/CodeMirror module honoring the same EditorProps.
//
// Large blocks (≥ IDLE_HIGHLIGHT_MIN_BYTES, still under the skip caps) mount
// the COMPLETE plain text immediately and swap in highlighted HTML from an
// idle callback; the skip caps (MAX_HIGHLIGHT_BYTES / MAX_HIGHLIGHT_LINES)
// remain the plain-forever policy. Either way nothing is ever truncated.
const HljsCode = memo(function HljsCode({ value, language, scrollMode, maxHeight, sourceSize }: EditorProps) {
  const syntaxHighlight = useMemo(
    () => shouldHighlightSource(value, sourceSize),
    [sourceSize, value],
  );
  const deferToIdle = syntaxHighlight && (sourceSize ?? value.length) >= IDLE_HIGHLIGHT_MIN_BYTES;
  const [idleResult, setIdleResult] = useState<{ value: string; language?: string; html: string } | null>(null);

  useEffect(() => {
    if (!deferToIdle) return;
    let cancelled = false;
    const cancelIdle = scheduleIdleTask(() => {
      if (cancelled) return;
      const html = highlightToHtml(value, language);
      setIdleResult({ value, language, html });
    });
    return () => {
      cancelled = true;
      cancelIdle();
    };
  }, [deferToIdle, language, value]);

  const idleHtml = idleResult && idleResult.value === value && idleResult.language === language
    ? idleResult.html
    : null;
  const html = useMemo(() => {
    if (!deferToIdle) return highlightToHtml(value, syntaxHighlight ? language : undefined);
    return idleHtml ?? escapeHtml(value);
  }, [deferToIdle, idleHtml, language, syntaxHighlight, value]);
  const highlighted = syntaxHighlight && (!deferToIdle || idleHtml !== null);
  const bounded = scrollMode === "bounded" || (scrollMode !== "expand" && maxHeight != null);
  return (
    <div className="code-block__wrap">
      <pre
        className={`code hljs${bounded ? " code--scroll-y" : ""}`}
        data-nested-scroll={bounded ? "" : undefined}
        data-highlight-mode={highlighted ? "syntax" : "plain"}
        data-lang={language}
        style={maxHeight != null ? { maxHeight } : undefined}
      >
        <code dangerouslySetInnerHTML={{ __html: html }} />
      </pre>
      <CopyButton text={value} className="code-block__copy" />
    </div>
  );
});

export default HljsCode;
