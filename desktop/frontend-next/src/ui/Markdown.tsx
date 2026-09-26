import { isValidElement, memo, useEffect, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import { CodeBlock } from "./CodeBlock";
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

// A single tilde is how "500~1000" writes a range; read as strikethrough, two
// ranges on one line strike out everything between them. Only ~~ strikes.
const BASE_REMARK = [[remarkGfm, { singleTilde: false }], remarkMath, remarkTrimAutolink];
// Order is load-bearing: raw parses HTML into nodes, sanitize prunes them, and
// katex renders afterwards so its generated markup is not pruned in turn.
const BASE_REHYPE = [rehypeRaw, [rehypeSanitize, schema]];

// KaTeX and its fonts are about half the bundle, and most coding sessions never
// produce a formula, so it is fetched the first time one appears. No lookbehind
// — older WebKit treats it as a syntax error and takes the whole page down.
const MATH = /\$\$[\s\S]+?\$\$|\$[^\n$]+\$/;
const EMOJI = /:[a-zA-Z0-9_+-]+:/;
// Only a fence that named its language. rehype-highlight's own detection stays
// off: guessing a grammar paints prose as code.
const FENCED = /^```[a-zA-Z]/m;

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
type Slot = { plugin: Plugin | null; loading: Promise<void> | null; load: () => Promise<Plugin> };

const KATEX: Slot = {
  plugin: null,
  loading: null,
  // Red source text beats an exception: the message stays readable and the
  // render cannot take the window down with it.
  load: () =>
    Promise.all([import("rehype-katex"), import("katex/dist/katex.min.css")]).then(
      ([mod]) => [mod.default, { throwOnError: false }] as Plugin,
    ),
};
/** Classes, not inline colours: a highlighter writing its own hex values would
 *  leave the token system and stop following the theme. */
const HLJS: Slot = { plugin: null, loading: null, load: () => import("rehype-highlight").then((m) => m.default) };
const EMOJIS: Slot = { plugin: null, loading: null, load: () => import("remark-gemoji").then((m) => m.default) };

/** Fetched the first time the text calls for one. Every read of a slot goes
 *  through a thunk because a plugin is a function: useState takes one as a lazy
 *  initializer, and what it would store is the attacher's transformer — which
 *  unified then calls as an attacher, handing it no tree at all. */
function usePlugin(needed: boolean, slot: Slot): Plugin | null {
  const [plugin, setPlugin] = useState<Plugin | null>(() => slot.plugin);
  useEffect(() => {
    if (!needed || slot.plugin) return;
    let alive = true;
    // A failed chunk leaves the source text, which still reads.
    slot.loading ??= slot.load().then((p) => {
      slot.plugin = p;
    });
    void slot.loading.then(() => alive && setPlugin(() => slot.plugin)).catch(() => {});
    return () => {
      alive = false;
    };
  }, [needed, slot]);
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

/** How a document shown from the workspace reads its own references: a link to
 *  another file there, and an image stored beside it. Absent for a reply. */
export interface LocalRefs {
  /** An action for href, or null when it is not a file in the workspace. */
  link(href: string): (() => void) | null;
  /** Where to load src from, or null to leave it as written. */
  image(src: string): string | null;
}

// live is the tail of a message still arriving; tail alone is only the last
// block, which a settled message has too.
const Block = memo(function Block({ src, math, emoji, code, tail, live, local }: { src: string; math: Plugin | null; emoji: Plugin | null; code: Plugin | null; tail?: boolean; live?: boolean; local?: LocalRefs }) {
  // Inside the memo, so a settled block normalises once instead of per chunk.
  const body = normalizeMath(tail ? balanceFences(src) : src);
  return (
    <ReactMarkdown
      remarkPlugins={(emoji ? [...BASE_REMARK, emoji] : BASE_REMARK) as never}
      rehypePlugins={([...BASE_REHYPE, math, code].filter(Boolean)) as never}
      components={{
        // Every link here comes from model output; a webview navigating away
        // would replace the app with the page.
        a: ({ children, href }) => {
          const open = local && href ? local.link(href) : null;
          if (open) {
            return (
              <a
                href={href}
                onClick={(e) => {
                  e.preventDefault();
                  open();
                }}
              >
                {children}
              </a>
            );
          }
          // An anchor alone points into a document this view does not number
          // headings for; followed as a link it would reload the window.
          if (local && href?.startsWith("#")) return <a>{children}</a>;
          return (
            <a href={href} target="_blank" rel="noreferrer noopener">
              {children}
            </a>
          );
        },
        img: ({ src, alt, title }) => (
          <img src={(local && typeof src === "string" && local.image(src)) || (src as string | undefined)} alt={alt} title={title} />
        ),
        pre: ({ children }) => (
          <CodeBlock lang={langOf(children)} source={textOf(children)} live={live}>{children}</CodeBlock>
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

export function Markdown({ text, streaming, local }: { text: string; streaming?: boolean; local?: LocalRefs }) {
  const shown = useRevealed(text, streaming);
  const math = usePlugin(MATH.test(text), KATEX);
  const emoji = usePlugin(EMOJI.test(text), EMOJIS);
  const code = usePlugin(FENCED.test(text), HLJS);
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
        <Block key={i} src={p} math={math} emoji={emoji} code={code} local={local} />
      ))}
      <Block src={shown.slice(at)} math={math} emoji={emoji} code={code} local={local} tail live={streaming} />
      {streaming && <span className="caret" />}
    </div>
  );
}
