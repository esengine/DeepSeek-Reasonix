import { memo, useMemo, useRef } from "react";
import ReactMarkdown from "react-markdown";
import "katex/dist/katex.min.css";
import { normalizeMath } from "./mathNormalize";
import { createComponents } from "./markdownComponents";
import { reasonixRehypePlugins, reasonixRemarkPlugins } from "./markdownRemarkPlugins";
import { markdownImageUrlTransform, markdownUrlTransform } from "../lib/markdownPipeline";

// Markdown rendering via react-markdown + remark-gfm (tables, task lists,
// strike, autolinks) and remark-math + rehype-katex for $/$$ KaTeX math.
// This is the STREAMING path (incremental commits of a live answer); history
// rows render worker-parsed HAST blocks through MarkdownHistory instead, with
// the same plugins, components map, and urlTransform.
//
// The math pre-pass repairs LLM-native delimiters and display structure.
// remarkMathPolicy then classifies parsed inline-math AST nodes using their
// surrounding prose, avoiding false positives on currency and env vars.
//
// file:/// hrefs come from local-path linkification (remarkLocalPathLinks)
// and must survive URL sanitisation; markdownUrlTransform (shared with the
// worker parse pipeline) keeps them while blanking javascript: and friends.

const MarkdownRenderer = memo(function MarkdownRenderer({
  text,
  plainStatusBlocks = false,
  bare = false,
}: {
  text: string;
  plainStatusBlocks?: boolean;
  bare?: boolean;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mathContent = useMemo(() => normalizeMath(text), [text]);
  const components = useMemo(() => createComponents(plainStatusBlocks), [plainStatusBlocks]);
  const content = (
    <ReactMarkdown
      remarkPlugins={reasonixRemarkPlugins}
      rehypePlugins={reasonixRehypePlugins}
      components={components}
      // file:/// anchors (local path linkification) are safe to keep; the
      // default transform would blank them along with javascript: etc.
      urlTransform={(value, key, node) => node.tagName === "img" && key === "src"
        ? markdownImageUrlTransform(value)
        : markdownUrlTransform(value)}
    >
      {mathContent}
    </ReactMarkdown>
  );
  return bare ? content : <div className="md" ref={containerRef}>{content}</div>;
});

export default MarkdownRenderer;
