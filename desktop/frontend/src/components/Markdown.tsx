import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import "katex/dist/katex.min.css";
import { CodeViewer } from "./CodeViewer";
import { openExternal } from "../lib/bridge";

// Markdown rendering via react-markdown + remark-gfm (tables, task lists, strike,
// autolinks) and remark-math + rehype-katex for $inline$/$$block$$ KaTeX math.
// Fenced code blocks are routed through the editor seam (CodeViewer)
// so syntax highlighting stays owned by one place; inline code is a styled <code>.
// Links open in the system browser rather than navigating the webview.

const components: Components = {
  // Passthrough <pre> so our code renderer fully owns block rendering (no nested
  // <pre><pre>).
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

export function Markdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown remarkPlugins={[remarkGfm, remarkMath]} rehypePlugins={[rehypeKatex]} components={components}>
        {text}
      </ReactMarkdown>
    </div>
  );
}
