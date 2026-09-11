import { isValidElement, memo, useEffect, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import { t } from "../i18n";
import { CopyButton } from "./CopyButton";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import { remarkTrimAutolink } from "./autolink";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import { useRevealed } from "./reveal";

// Model output is untrusted markup, so raw HTML is parsed and then cut back to
// an allowlist. These four tags carry meaning no markdown syntax expresses;
// nothing that can navigate, load, or script survives the filter. Subscript and
// superscript ride here rather than on a shorthand plugin: Pandoc's ~2~ collides
// with GFM's ~~strikethrough~~, and one mechanism beats two that fight.
const schema = {
  ...defaultSchema,
  tagNames: [...(defaultSchema.tagNames ?? []), "mark", "sub", "sup", "kbd"],
  attributes: {
    ...defaultSchema.attributes,
    // KaTeX marks its output with classes; sanitizing runs before it, so the
    // math wrappers remark-math emits have to survive to be rendered.
    "*": [...(defaultSchema.attributes?.["*"] ?? []), "className"],
  },
};

const BASE_REMARK = [remarkGfm, remarkMath, remarkTrimAutolink];
// Order is load-bearing: raw parses HTML into nodes, sanitize prunes them, and
// katex renders afterwards so its generated markup is not pruned in turn.
const BASE_REHYPE = [rehypeRaw, [rehypeSanitize, schema]];

// KaTeX and its fonts are about half the bundle, and most coding sessions never
// produce a formula, so it is fetched the first time one appears. No lookbehind
// — older WebKit treats it as a syntax error and takes the whole page down.
const MATH = /\$\$[\s\S]+?\$\$|\$[^\n$]+\$/;
const GEMOJI = /:[a-zA-Z0-9_+-]+:/;

// Models reach for LaTeX's own delimiters as readily as for dollars, and
// remark-math reads only dollars. Rewriting them keeps one parser instead of
// two. Code is split out first: a \[ inside a fence is text, not an equation.
const CODE = /(```[\s\S]*?```|`[^`\n]*`)/g;

function normalizeMath(md: string) {
  return md
    .split(CODE)
    .map((part, i) =>
      i % 2 === 1
        ? part
        : part
            .replace(/\\\[([\s\S]+?)\\\]/g, (_, m) => `\n\n$$${m}$$\n\n`)
            .replace(/\\\(([\s\S]+?)\\\)/g, (_, m) => `$${m}$`),
    )
    .join("");
}

type Plugin = unknown;
let katex: Plugin | null = null;
let katexLoading: Promise<void> | null = null;
let gemoji: Plugin | null = null;
let gemojiLoading: Promise<void> | null = null;

function useKatex(needed: boolean): Plugin | null {
  const [plugin, setPlugin] = useState<Plugin | null>(katex);
  useEffect(() => {
    if (!needed || katex) return;
    let alive = true;
    katexLoading ??= Promise.all([import("rehype-katex"), import("katex/dist/katex.min.css")]).then(
      ([mod]) => {
        // Red source text beats an exception: the message stays readable and
        // the render cannot take the window down with it.
        katex = [mod.default, { throwOnError: false }];
      },
    );
    // A failed chunk leaves the math as source text, which still reads.
    void katexLoading.then(() => alive && setPlugin(() => katex)).catch(() => {});
    return () => {
      alive = false;
    };
  }, [needed]);
  return plugin;
}

function useGemoji(needed: boolean): Plugin | null {
  const [plugin, setPlugin] = useState<Plugin | null>(gemoji);
  useEffect(() => {
    if (!needed || gemoji) return;
    let alive = true;
    gemojiLoading ??= import("remark-gemoji").then((mod) => {
      gemoji = mod.default;
    });
    void gemojiLoading.then(() => alive && setPlugin(() => gemoji)).catch(() => {});
    return () => {
      alive = false;
    };
  }, [needed]);
  return plugin;
}

// Streaming text arrives mid-token, so an unterminated fence would flip the
// whole tail into a code block for as long as it stays open. Close it for the
// render only; the source string is untouched.
function balanceFences(md: string) {
  const fences = md.match(/^```/gm)?.length ?? 0;
  return fences % 2 === 0 ? md : md + "\n```";
}

// A blank line ends a block, but not every one of them is safe to cut at: a
// list, a quote and an indented continuation all carry across one, so slicing
// there would render two lists where the author wrote one.
const CONTINUES = /^(\s|[-*+] |\d+[.)] |>)/;

// Nothing before a safe blank line can change as more text arrives, so each
// span between two of them is parsed once and then held. Splitting only the
// tail off was not enough: re-parsing the whole settled head every time a
// block closed cost 184ms on a long message.
function cutsOf(md: string): number[] {
  const cuts: number[] = [];
  let pos = 0;
  let cand = -1;
  let open = false;
  for (const line of md.split("\n")) {
    const next = pos + line.length + 1;
    const fence = line.startsWith("```");
    if (!open) {
      if (line.trim() === "") cand = next;
      else if (cand >= 0) {
        if (fence || !CONTINUES.test(line)) cuts.push(cand);
        cand = -1;
      }
    }
    if (fence) open = !open;
    pos = next;
  }
  return cuts;
}

// The fence's own info string, as rehype leaves it on the <code>. Absent for an
// indented block, which is why the tag is conditional rather than defaulted to
// a guess at the language.
function langOf(node: ReactNode): string {
  if (!isValidElement(node)) return "";
  const cn = (node.props as { className?: string }).className ?? "";
  return /language-([\w.+#-]+)/.exec(cn)?.[1] ?? "";
}

// What gets copied is the source, so it is read back off the rendered tree
// rather than off the block's markdown — by here the fence markers and the
// info string are gone, and a balanced tail fence was never typed at all.
function textOf(node: ReactNode): string {
  if (typeof node === "string") return node;
  if (typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement(node)) return textOf((node.props as { children?: ReactNode }).children);
  return "";
}

const Block = memo(function Block({ src, math, emoji, tail }: { src: string; math: Plugin | null; emoji: Plugin | null; tail?: boolean }) {
  // Inside the memo, so a settled block normalises once instead of per chunk.
  const body = normalizeMath(tail ? balanceFences(src) : src);
  return (
    <ReactMarkdown
      remarkPlugins={(emoji ? [...BASE_REMARK, emoji] : BASE_REMARK) as never}
      rehypePlugins={(math ? [...BASE_REHYPE, math] : BASE_REHYPE) as never}
      components={{
        // Every link here comes from model output; a webview navigating away
        // would replace the app with the page.
        a: ({ children, href }) => (
          <a href={href} target="_blank" rel="noreferrer noopener">
            {children}
          </a>
        ),
        pre: ({ children }) => (
          <div className="code-wrap" data-lang={langOf(children) || undefined}>
            <pre className="term">{children}</pre>
            <CopyButton text={textOf(children)} className="code-copy" label={t("复制这段代码")} />
          </div>
        ),
        table: ({ children }) => (
          <div className="md-tw">
            <table>{children}</table>
          </div>
        ),
      }}
    >
      {body}
    </ReactMarkdown>
  );
});

export function Markdown({ text, streaming }: { text: string; streaming?: boolean }) {
  const shown = useRevealed(text, streaming);
  const math = useKatex(MATH.test(text));
  const emoji = useGemoji(GEMOJI.test(text));
  // Only a streamed message is split. Once it settles it parses whole again, so
  // nothing left in the transcript stands as a pile of separate documents.
  const cuts = streaming ? cutsOf(shown) : [];
  const parts: string[] = [];
  let at = 0;
  for (const c of cuts) {
    parts.push(shown.slice(at, c));
    at = c;
  }
  return (
    // Same rule the answer's own copy follows: while it is still arriving,
    // handing a listing over would hand over something that was never said.
    <div className="md" data-live={streaming ? "" : undefined}>
      {parts.map((p, i) => (
        <Block key={i} src={p} math={math} emoji={emoji} />
      ))}
      <Block src={shown.slice(at)} math={math} emoji={emoji} tail />
      {streaming && <span className="caret" />}
    </div>
  );
}
