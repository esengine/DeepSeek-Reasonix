import { memo, useMemo } from "react";
import type { EditorProps } from "../CodeViewer";
import { highlightToHtml, shouldHighlightSource } from "../../lib/highlight";
import { CopyButton } from "../CopyButton";

// HljsCode is the syntax-highlighted default behind the code editor seam. It
// renders highlight.js token markup into a <pre>; token colors live in styles.css
// (.hljs-*). To upgrade to a full editor, point CodeViewer.tsx's lazy import at a
// Monaco/CodeMirror module honoring the same EditorProps.
const HljsCode = memo(function HljsCode({ value, language, maxHeight, sourceSize }: EditorProps) {
  const syntaxHighlight = useMemo(
    () => shouldHighlightSource(value, sourceSize),
    [sourceSize, value],
  );
  const html = useMemo(
    () => highlightToHtml(value, syntaxHighlight ? language : undefined),
    [language, syntaxHighlight, value],
  );
  return (
    <div className="code-block__wrap">
      <pre
        className="code hljs"
        data-highlight-mode={syntaxHighlight ? "syntax" : "plain"}
        data-lang={language}
        style={maxHeight ? { maxHeight } : undefined}
      >
        <code dangerouslySetInnerHTML={{ __html: html }} />
      </pre>
      <CopyButton text={value} className="code-block__copy" />
    </div>
  );
});

export default HljsCode;
