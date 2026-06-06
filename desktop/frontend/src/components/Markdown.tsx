import { memo, useDeferredValue, useMemo } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import "katex/dist/katex.min.css";
import { CodeBlockOrMath } from "./CodeBlockOrMath";
import { CodeOrMath } from "./CodeOrMath";
import { InlineMath } from "./InlineMath";
import { BlockMath } from "./BlockMath";
import { isLikelyInlineMath } from "./mathClassify";
import { openExternal } from "../lib/bridge";

// ── remark plugin: LLM-LaTeX delimiters ────────────────────────────────────────
// Handles all math delimiters that LLMs emit: $$ / \[ for display, $ / \( for
// inline. $…$ is gated by an LLM-aware classifier to avoid $5, currency, and
// link URLs. Produces custom HAST-passable nodes so react-markdown routes them
// straight to our InlineMath / BlockMath components.

function remarkLatexDelimiters() {
  return (tree: any) => walkTree(tree);

  function walkTree(node: any) {
    if (!node || !Array.isArray(node.children)) return;
    for (let i = 0; i < node.children.length; i++) {
      const child = node.children[i];
      if (child.type === "inlineCode" || child.type === "code") continue;
      if (child.type === "text" && typeof child.value === "string") {
        const replaced = splitText(child);
        if (replaced.length > 1) {
          node.children.splice(i, 1, ...replaced);
          i += replaced.length - 1;
        }
      }
      if (Array.isArray(child.children)) walkTree(child);
    }
  }
}

function inlineMathNode(value: string): any {
  return {
    type: "inlineMath",
    value,
    data: { hName: "inline-math-node", hProperties: { value } },
  };
}

function blockMathNode(value: string): any {
  return {
    type: "math",
    value,
    data: { hName: "block-math-node", hProperties: { value } },
  };
}

function splitText(node: any): any[] {
  const s: string = node.value;
  const parts: any[] = [];
  let i = 0;

  while (i < s.length) {
    // $$ ... $$ → display math (before \[ check so $$ wins for ambiguous input)
    if (s.startsWith("$$", i) && !isMathEscaped(s, i)) {
      const close = findTokClose(s, i + 2, "$$");
      if (close !== -1) {
        parts.push(blockMathNode(s.slice(i + 2, close).trim()));
        i = close + 2;
        continue;
      }
    }

    // \[ ... \] → display math
    if (s.startsWith("\\[", i) && !isMathEscaped(s, i)) {
      const close = findSimpleClose(s, i + 2, "\\]");
      if (close !== -1) {
        parts.push(blockMathNode(s.slice(i + 2, close).trim()));
        i = close + 2;
        continue;
      }
    }

    // \( ... \) → inline math
    if (s.startsWith("\\(", i) && !isMathEscaped(s, i)) {
      const close = findSimpleClose(s, i + 2, "\\)");
      if (close !== -1) {
        parts.push(inlineMathNode(s.slice(i + 2, close).trim()));
        i = close + 2;
        continue;
      }
    }

    // $...$ → inline math (LLM-aware classifier)
    if (s[i] === "$" && !isMathEscaped(s, i)) {
      // Don't eat into a $$ sequence
      if (s.startsWith("$$", i)) {
        parts.push({ type: "text", value: s.slice(i, i + 1) });
        i += 1;
        continue;
      }
      const close = findDollarClose(s, i + 1);
      if (close !== -1) {
        const math = s.slice(i + 1, close).trim();
        if (isLikelyInlineMath(math)) {
          parts.push(inlineMathNode(math));
          i = close + 1;
          continue;
        }
        parts.push({ type: "text", value: s.slice(i, close + 1) });
        i = close + 1;
        continue;
      }
    }

    // Accumulate plain text
    let j = i + 1;
    while (j < s.length) {
      if (s.startsWith("$$", j) && !isMathEscaped(s, j)) break;
      if (s.startsWith("\\[", j) && !isMathEscaped(s, j)) break;
      if (s.startsWith("\\(", j) && !isMathEscaped(s, j)) break;
      if (s[j] === "$" && !isMathEscaped(s, j)) break;
      j += 1;
    }
    parts.push({ type: "text", value: s.slice(i, j) });
    i = j;
  }

  return parts;
}

function findSimpleClose(s: string, start: number, target: string): number {
  for (let i = start; i < s.length; i++) {
    if (target === "\\)" && s[i] === "\n") return -1;
    if (s.startsWith(target, i) && !isMathEscaped(s, i)) return i;
  }
  return -1;
}

function findTokClose(s: string, start: number, tok: string): number {
  for (let i = start; i < s.length; i++) {
    if (s.startsWith(tok, i) && !isMathEscaped(s, i)) return i;
  }
  return -1;
}

function findDollarClose(s: string, start: number): number {
  for (let i = start; i < s.length; i++) {
    if (s[i] === "\n") return -1;
    if (s[i] === "\\") { i += 1; continue; }
    if (s[i] === "$" && !isMathEscaped(s, i)) return i;
  }
  return -1;
}

function isMathEscaped(s: string, i: number): boolean {
  let slashCount = 0;
  for (let j = i - 1; j >= 0 && s[j] === "\\"; j -= 1) slashCount += 1;
  return slashCount % 2 === 1;
}

// ── Components ─────────────────────────────────────────────────────────────────

function makeComponents(): Components & Record<string, any> {
  return {
    pre: ({ children }) => <>{children}</>,
    code: ({ className, children }) => {
      const text = String(children ?? "");
      const match = /language-([\w-]+)/.exec(className ?? "");
      const isBlock = match !== null || text.includes("\n");
      if (isBlock) {
        return <CodeBlockOrMath value={text.replace(/\n$/, "")} language={match?.[1]} />;
      }
      return <CodeOrMath source={text} />;
    },
    "inline-math-node": ({ value }: any) => (value ? <InlineMath source={value} /> : null),
    "block-math-node": ({ value }: any) => (value ? <BlockMath source={value} /> : null),
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
}

export const Markdown = memo(function Markdown({ text }: { text: string }) {
  const components = useMemo(() => makeComponents(), []);
  // useDeferredValue lets React prioritise the plain-text streaming frame over
  // the expensive markdown parse+render. If a new text delta arrives while the
  // markdown tree is still diffing, React can abort the in-progress render and
  // start fresh with the latest text — keeping the UI responsive during the
  // final markdown pass on long responses.
  const deferred = useDeferredValue(text);

  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkLatexDelimiters]}
        components={components}
      >
        {deferred}
      </ReactMarkdown>
    </div>
  );
});
