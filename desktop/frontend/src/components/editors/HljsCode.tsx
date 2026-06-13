import type { EditorProps } from "../CodeViewer";
import { highlightToHtml } from "../../lib/highlight";
import { CopyButton } from "../CopyButton";

// HljsCode is the syntax-highlighted default behind the code editor seam. It
// renders highlight.js token markup into a <pre>; token colors live in styles.css
// (.hljs-*). To upgrade to a full editor, point CodeViewer.tsx's lazy import at a
// Monaco/CodeMirror module honoring the same EditorProps.
export default function HljsCode({ value, language, maxHeight }: EditorProps) {
  const html = highlightToHtml(value, language);
  return (
    <pre className="code hljs" data-lang={language} style={maxHeight ? { maxHeight } : undefined}>
      <code dangerouslySetInnerHTML={{ __html: html }} />
      <CopyButton text={value} className="code-block__copy" />
    </pre>
  );
}
