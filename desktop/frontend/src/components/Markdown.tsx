import { memo, useDeferredValue } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import "katex/dist/katex.min.css";
import { CodeViewer } from "./CodeViewer";
import { isLikelyInlineMath } from "./mathClassify";
import { openExternal } from "../lib/bridge";

// Markdown rendering via react-markdown + remark-gfm (tables, task lists,
// strike, autolinks) and remark-math + rehype-katex for $/$$ KaTeX math.
// Fenced code blocks go through CodeViewer for syntax highlighting; inline
// code is a styled <code>. Links open in the system browser.
//
// LLMs emit \(…\) \[…\] delimiters (remark-math only parses $/$$), so we
// normalise them in a pre-pass. Single $…$ is gated by isLikelyInlineMath to
// avoid false positives on $5, $PATH, etc.

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
    >
      {children}
    </a>
  ),
};

// normalizeMath converts LLM-typical \( \) \[ \] delimiters into the $/$$ syntax
// that remark-math expects, protects LaTeX line-break spacing \\[ from the
// rewrite, filters out non-math $…$ (currency, env vars, code tokens), and
// swaps | for \vert inside math so remark-gfm can't read the bar as a table
// column.
function normalizeMath(s: string): string {
  const DM = "\x00DM\x00"; // display-math placeholder
  const IM = "\x00IM\x00"; // inline-math placeholder

  // Protect \\[ line-break spacing in LaTeX
  const lb = "\x00LB\x00";
  let r = s.replace(/\\\\\[/g, lb);

  // Convert LLM-native delimiters to standard $/$$ syntax.
  // Arrow functions are required: "$$" in a JS replace string means a single
  // literal $, not two.
  r = r
    .replace(/\\\[/g, () => "$$")
    .replace(/\\\]/g, () => "$$")
    .replace(/\\\(/g, () => "$")
    .replace(/\\\)/g, () => "$");

  r = r.replace(/\x00LB\x00/g, "\\\\[");

  // Replace | with \vert inside math so remark-gfm table parsing doesn't
  // interfere. We process $$…$$ first and replace the delimiters with
  // placeholders so the single-$ classifier pass won't accidentally match
  // dollars that belong to a display-math block.
  const vert = (m: string) => m.replace(/\|/g, "\\vert ");

  // Step 1: $$…$$ → placeholder-wrapped blocks
  r = r.replace(/\$\$([\s\S]*?)\$\$/g, (_m, m) => `${DM}${vert(m)}${DM}`);

  // Step 2: $…$ → classifier-gated inline math
  r = r.replace(/\$([^$\n]+)\$/g, (_m, m) => {
    if (!isLikelyInlineMath(m.trim())) return `＄${m}＄`;
    return `${IM}${vert(m)}${IM}`;
  });

  // Step 3: restore standard delimiters for remark-math
  return r.replace(/\x00DM\x00/g, () => "$$").replace(/\x00IM\x00/g, () => "$");
}

export const Markdown = memo(function Markdown({ text }: { text: string }) {
  const deferred = useDeferredValue(text);
  return (
    <div className="md">
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
