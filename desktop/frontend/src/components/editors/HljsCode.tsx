import { memo, useMemo } from "react";
import type { EditorProps } from "../CodeViewer";
import { highlightToHtml } from "../../lib/highlight";
import { CopyButton } from "../CopyButton";

// HljsCode is the syntax-highlighted default behind the code editor seam. It
// renders highlight.js token markup into a <pre>; token colors live in styles.css
// (.hljs-*). To upgrade to a full editor, point CodeViewer.tsx's lazy import at a
// Monaco/CodeMirror module honoring the same EditorProps.
const HljsCode = memo(function HljsCode({ value, language, maxHeight, showLineNumbers }: EditorProps) {
  const html = useMemo(() => highlightToHtml(value, language), [value, language]);

  if (!showLineNumbers) {
    return (
      <div className="code-block__wrap">
        <pre className="code hljs" data-lang={language} style={maxHeight ? { maxHeight } : undefined}>
          <code dangerouslySetInnerHTML={{ __html: html }} />
        </pre>
        <CopyButton text={value} className="code-block__copy" />
      </div>
    );
  }

  const codeLines = html.split("\n");
  const lineCount = codeLines.length;
  const gutterWidth = lineCount >= 10000 ? "5ch" : lineCount >= 1000 ? "4ch" : "3ch";

  return (
    <div className="code-block__wrap">
      <div
        className="code code--ln hljs"
        data-lang={language}
        role="list"
        style={maxHeight ? { maxHeight } : undefined}
      >
        {codeLines.map((lineHtml, i) => (
          <div key={i} className="code-block__ln-row" role="listitem">
            <span className="code-block__ln-gutter" style={{ minWidth: gutterWidth }}>
              {i + 1}
            </span>
            <code
              className="code-block__ln-code"
              dangerouslySetInnerHTML={{ __html: lineHtml || "\u00A0" }}
            />
          </div>
        ))}
      </div>
      <CopyButton text={value} className="code-block__copy" />
    </div>
  );
});

export default HljsCode;
