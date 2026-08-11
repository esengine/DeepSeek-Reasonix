// markdownPipeline — the isomorphic Markdown→HAST parse pipeline (Phase E of
// the history refactor). It runs the EXACT processor chain react-markdown uses
// in MarkdownRenderer, factored into a side-effect-free function that runs both
// on the main thread (jsdom/tests, Worker-less fallback) and inside
// markdown.worker.ts:
//
//   normalizeMath pre-pass
//   → remark-parse + remarkGfm + remarkMath + remarkMathPolicy + remarkLocalPathLinks
//   → remark-rehype (allowDangerousHtml, same as react-markdown's default)
//   → rehypeReasonixKatex
//   → react-markdown's post-transform (raw → text, urlTransform on URL attrs)
//
// The document is parsed WHOLE so definitions, footnotes, and reference links
// resolve globally; sliceHastBlocks then cuts the resulting HAST top-level
// children into renderable blocks (the remark-rehype footnote section stays its
// own trailing block). Concatenating the rendered blocks is byte-identical to
// rendering the unsliced root.
//
// This module must stay free of DOM/React imports so the inline worker bundle
// contains no component code.

import { unified } from "unified";
import { VFile } from "vfile";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { urlAttributes } from "html-url-attributes";
import { visit } from "unist-util-visit";
import type { Element as HastElement, Root as HastRoot, RootContent as HastRootContent } from "hast";
import { normalizeMath } from "../components/mathNormalize";
import { reasonixRemarkPlugins } from "../components/markdownRemarkPlugins";
import { reasonixRehypePlugins } from "../components/rehypeReasonixKatex";
import {
  extractLargePlainMarkdownTables,
  type VirtualMarkdownTableData,
} from "./largeMarkdownTable";
import { isLocalFileHref } from "./localFileUrl";
import { contentRevision } from "./contentRevision";
import { markdownSelectionTextFromBlocks } from "./markdownSelectionProjection";
export { estimateHastBytes } from "./markdownByteEstimate";

export type { HastRoot, HastRootContent };

export interface MarkdownBlock {
  /** Stable within one parse: the block's top-level index. */
  key: string;
  children: HastRootContent[];
  /** Lightweight representation for a large table with plain-text cells. */
  virtualTable?: VirtualMarkdownTableData;
}

export interface MarkdownParseResult {
  blocks: MarkdownBlock[];
  selectionText: string;
  selectionRevision: number;
}

const SAFE_PROTOCOL_RE = /^(https?|ircs?|mailto|xmpp)$/i;

// Mirror of react-markdown's exported defaultUrlTransform (which itself mirrors
// micromark-util-sanitize-uri without the encode pass). Copied so the worker
// bundle does not pull React in through react-markdown; the pipeline parity
// test pins this copy against the original over a URL corpus.
export function defaultMarkdownUrlTransform(value: string): string {
  const colon = value.indexOf(":");
  const questionMark = value.indexOf("?");
  const numberSign = value.indexOf("#");
  const slash = value.indexOf("/");

  if (
    // If there is no protocol, it’s relative.
    colon === -1
    // If the first colon is after a `?`, `#`, or `/`, it’s not a protocol.
    || (slash !== -1 && colon > slash)
    || (questionMark !== -1 && colon > questionMark)
    || (numberSign !== -1 && colon > numberSign)
    // It is a protocol, it should be allowed.
    || SAFE_PROTOCOL_RE.test(value.slice(0, colon))
  ) {
    return value;
  }

  return "";
}

// Local file hrefs come from local-path linkification (remarkLocalPathLinks)
// or explicit Markdown links and must survive URL sanitisation, which would
// otherwise blank them along with javascript: and friends.
export function markdownUrlTransform(value: string): string {
  return isLocalFileHref(value) ? value : defaultMarkdownUrlTransform(value);
}

// The same transform react-markdown applies between the rehype plugins and
// hast-util-to-jsx-runtime (its internal `post()` visitor): raw HTML nodes
// become text (skipHtml is false), and every URL attribute passes through
// urlTransform. applied here so worker output arrives render-ready.
function applyReactMarkdownTransforms(tree: HastRoot): void {
  visit(tree, (node, index, parent) => {
    if ((node.type as string) === "raw" && parent && typeof index === "number") {
      parent.children[index] = { type: "text", value: (node as unknown as { value: string }).value };
      return index;
    }
    if (node.type === "element") {
      const element = node as HastElement;
      for (const key in urlAttributes) {
        const hasAttr = Object.prototype.hasOwnProperty.call(urlAttributes, key)
          && Object.prototype.hasOwnProperty.call(element.properties, key);
        if (!hasAttr) continue;
        const value = element.properties[key];
        const test = urlAttributes[key as keyof typeof urlAttributes];
        if (test === null || (test as readonly string[]).includes(element.tagName)) {
          element.properties[key] = markdownUrlTransform(String(value || ""));
        }
      }
    }
    return undefined;
  });
}

/**
 * Parse markdown text into a render-ready HAST root using the exact chain the
 * main-thread renderer uses. Synchronous (unified runSync), DOM-free, safe to
 * run inside a Web Worker.
 */
export function parseMarkdownToHast(text: string): HastRoot {
  return parseNormalizedMarkdownToHast(normalizeMath(text));
}

function parseNormalizedMarkdownToHast(normalized: string): HastRoot {
  const processor = unified()
    .use(remarkParse)
    .use(reasonixRemarkPlugins)
    .use(remarkRehype, { allowDangerousHtml: true })
    .use(reasonixRehypePlugins);
  // The same VFile must flow through parse and runSync: remarkMathPolicy
  // slices original math sources out of file.value by node position.
  const file = new VFile({ value: normalized });
  const tree = processor.runSync(processor.parse(file), file) as unknown as HastRoot;
  applyReactMarkdownTransforms(tree);
  return tree;
}

const VIRTUAL_TABLE_TAG = "reasonix-virtual-table";

function injectVirtualTablePlaceholders(
  root: HastRoot,
  markerPrefix: string,
  tableCount: number,
): void {
  const walk = (node: HastRoot | HastElement): void => {
    for (let index = 0; index < node.children.length; index += 1) {
      const child = node.children[index];
      if (child.type === "element") {
        const paragraph = child as HastElement;
        const only = paragraph.tagName === "p" && paragraph.children.length === 1
          ? paragraph.children[0]
          : undefined;
        const value = only?.type === "text" ? only.value : "";
        if (value.startsWith(markerPrefix)) {
          const tableIndex = Number(value.slice(markerPrefix.length));
          if (Number.isInteger(tableIndex) && tableIndex >= 0 && tableIndex < tableCount) {
            node.children[index] = {
              type: "element",
              tagName: VIRTUAL_TABLE_TAG,
              properties: { dataReasonixTableIndex: tableIndex },
              children: [],
            };
            continue;
          }
        }
        walk(paragraph);
      }
    }
  };
  walk(root);
}

/**
 * Slice a parsed HAST root into top-level blocks. Each element child starts a
 * new block; interstitial whitespace text attaches to the FOLLOWING block so
 * concatenating the rendered blocks reproduces the unsliced render byte for
 * byte. Whole-document parsing has already resolved footnote/reference
 * definitions, and remark-rehype's footnote section arrives as a trailing
 * top-level element, so it becomes its own trailing block.
 */
export function sliceHastBlocks(root: HastRoot): MarkdownBlock[] {
  const blocks: MarkdownBlock[] = [];
  let pending: HastRootContent[] = [];
  for (const child of root.children) {
    if (child.type === "element") {
      blocks.push({ key: `b${blocks.length}`, children: [...pending, child] });
      pending = [];
    } else if (blocks.length === 0) {
      pending.push(child);
    } else {
      blocks[blocks.length - 1].children.push(child);
    }
  }
  if (pending.length > 0) {
    if (blocks.length === 0) blocks.push({ key: "b0", children: pending });
    else blocks[blocks.length - 1].children.push(...pending);
  }
  return blocks;
}

/** Parse + slice in one call (the worker entry point). */
export function parseMarkdownToBlocks(text: string): MarkdownBlock[] {
  const normalized = normalizeMath(text);
  const extracted = extractLargePlainMarkdownTables(normalized);
  if (extracted.tables.length === 0) return sliceHastBlocks(parseNormalizedMarkdownToHast(normalized));

  const processor = unified()
    .use(remarkParse)
    .use(reasonixRemarkPlugins)
    .use(remarkRehype, { allowDangerousHtml: true })
    .use(reasonixRehypePlugins);
  const file = new VFile({ value: extracted.text });
  const tree = processor.runSync(processor.parse(file), file) as unknown as HastRoot;
  applyReactMarkdownTransforms(tree);
  injectVirtualTablePlaceholders(tree, extracted.markerPrefix, extracted.tables.length);

  return sliceHastBlocks(tree).map((block) => {
    const placeholder = block.children.find(
      (child): child is HastElement => child.type === "element" && child.tagName === VIRTUAL_TABLE_TAG,
    );
    if (!placeholder) return block;
    const index = Number(placeholder.properties.dataReasonixTableIndex);
    return {
      ...block,
      children: block.children.filter((child) => child !== placeholder),
      virtualTable: extracted.tables[index],
    };
  });
}

/** Parse once and derive both the render tree and copy projection. */
export function parseMarkdown(text: string): MarkdownParseResult {
  const blocks = parseMarkdownToBlocks(text);
  const selectionText = markdownSelectionTextFromBlocks(blocks);
  return {
    blocks,
    selectionText,
    selectionRevision: contentRevision(selectionText),
  };
}

/**
 * Content-derived cache revision for the transcript markdown cache: an FNV-1a
 * fingerprint of the source text. Cache entries also store the source itself,
 * so a (practically impossible) hash collision is caught by comparison.
 */
export function markdownContentRevision(text: string): number {
  return contentRevision(text);
}
