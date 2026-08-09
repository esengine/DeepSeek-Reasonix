// Run: tsx src/__tests__/markdown-pipeline.test.tsx
//
// Parse-parity goldens for the isomorphic markdown pipeline (Phase E): the
// worker/fallback pipeline must render byte-identical static markup to the
// production react-markdown path for the same document, both unsliced and
// sliced into blocks (footnotes/reference definitions resolve across blocks
// because parsing is whole-document).

import { createElement, Fragment, type ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import { normalizeMath } from "../components/mathNormalize";
import { createComponents } from "../components/markdownComponents";
import { reasonixRehypePlugins, reasonixRemarkPlugins } from "../components/markdownRemarkPlugins";
import { hastBlockToJsx } from "../lib/hastJsx";
import {
  defaultMarkdownUrlTransform,
  estimateHastBytes,
  markdownContentRevision,
  markdownUrlTransform,
  parseMarkdownToBlocks,
  parseMarkdownToHast,
  sliceHastBlocks,
  type MarkdownBlock,
} from "../lib/markdownPipeline";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function renderCurrent(text: string): string {
  return renderToStaticMarkup(
    createElement(ReactMarkdown, {
      remarkPlugins: reasonixRemarkPlugins,
      rehypePlugins: reasonixRehypePlugins,
      components: createComponents(false),
      urlTransform: markdownUrlTransform,
      children: normalizeMath(text),
    }),
  );
}

function renderBlocks(blocks: MarkdownBlock[]): string {
  const components = createComponents(false);
  return renderToStaticMarkup(
    createElement(Fragment, {
      children: blocks.map((block) =>
        createElement(Fragment, { key: block.key, children: hastBlockToJsx(block, components) as ReactNode })),
    }),
  );
}

console.log("\nmarkdown pipeline parity");

const bigCode = "```js\n" + "const value = compute(index); // keep this line long enough\n".repeat(4000) + "```";
ok(bigCode.length > 100_000, "code-fence fixture exceeds 100KB");

const fixtures: Record<string, string> = {
  headingsAndProse: "# Title\n\nHello **world**, this is [a link](https://example.com).\n\nSecond paragraph with `code`.",
  gfmTable: "| name | value |\n| --- | ---: |\n| a | 1 |\n| b | 2 |",
  taskList: "- [x] done\n- [ ] todo\n- [ ] item with **bold** and `code`",
  strikethroughAutolink: "~~gone~~ and https://example.com/auto plus www.example.com",
  mathInline: "Price is $5 and math $x^2$ works. Also \\(y=1\\) and $E=mc^2$.",
  mathBlockRepair: "Before\n\n\\[\n\\int_0^1 x\\,dx\n\\]\n\nAfter\n\n$$\n\\frac{a}{b}\n$$",
  mathInlinePipe: "| formula | note |\n| --- | --- |\n| $|x|$ cell | pipe inside math |",
  footnotes: "Text with a note[^1] and another[^long].\n\n[^1]: first note\n\n[^long]: second note with [ref link][r]\n\n[r]: https://example.com/ref",
  crossBlockRefs: "Use [shared] here.\n\n## Section\n\nMore text.\n\n## Later\n\nAgain [shared] and [other].\n\n[shared]: https://example.com/shared\n[other]: https://example.com/other",
  mermaidFence: "```mermaid\ngraph TD\nA-->B\n```",
  bigCodeFence: bigCode,
  cjk: "中文段落，包含「引号」和路径 D:\\work\\项目\\文件.md。\n\n- 列表项一\n- 列表项二",
  rawHtml: "Before <div class=\"x\">raw</div> after\n\n<script>alert(1)</script>",
  unsafeAndFileLinks: "[bad](javascript:alert(1)) and [file](file:///tmp/a%20b.txt) and D:\\src\\app.ts",
  manyBlocks: Array.from({ length: 40 }, (_, i) => `## Part ${i}\n\nParagraph ${i} with *emphasis*.\n`).join("\n"),
};

for (const [name, text] of Object.entries(fixtures)) {
  const expected = renderCurrent(text);
  const root = parseMarkdownToHast(text);
  const whole = renderBlocks([{ key: "whole", children: root.children }]);
  eq(whole, expected, `${name}: pipeline render matches react-markdown`);
  const blocks = sliceHastBlocks(root);
  const sliced = renderBlocks(blocks);
  eq(sliced, expected, `${name}: sliced blocks render identically (${blocks.length} blocks)`);
}

// Block keys are stable top-level indexes.
{
  const blocks = parseMarkdownToBlocks("one\n\ntwo\n\nthree");
  eq(blocks.map((b) => b.key).join(","), "b0,b1,b2", "block keys are stable indexes");
}

// Footnote definitions survive slicing as a trailing block with working refs.
{
  const text = "first[^a]\n\n## middle\n\nsecond[^b]\n\n[^a]: note a\n[^b]: note b";
  const blocks = parseMarkdownToBlocks(text);
  const last = blocks[blocks.length - 1];
  const lastHtml = renderBlocks([last]);
  ok(lastHtml.includes("data-footnotes"), "footnote section is the trailing block");
  const sliced = renderBlocks(blocks);
  ok(sliced.includes('href="#user-content-fn-a"'), "footnote reference links survive slicing");
  ok(sliced.includes('id="user-content-fn-a"'), "footnote definition anchors survive slicing");
}

// The copied defaultUrlTransform must match react-markdown's across protocols.
{
  const corpus = [
    "https://example.com/a?b=c#d",
    "http://example.com",
    "mailto:user@example.com",
    "javascript:alert(1)",
    "vbscript:x",
    "data:text/html,boom",
    "./relative/path",
    "../up",
    "/absolute",
    "#fragment",
    "query?only",
    "ftp://example.com/file",
    "file:///etc/passwd",
    "HTTPS://EXAMPLE.COM/upper",
    "ircs://irc.example.com/chan",
    "xmpp:user@example.com",
    "a:b:c",
    "",
    "C:\\src\\app.ts",
  ];
  for (const url of corpus) {
    eq(defaultMarkdownUrlTransform(url), defaultUrlTransform(url), `urlTransform parity for ${JSON.stringify(url)}`);
  }
}

// Authority-form UNC links use the same strict local-file allowlist in the
// worker pipeline as canonical file:/// links do.
{
  const unc = "file://nas/share/report.md";
  eq(markdownUrlTransform(unc), unc, "authority-form UNC survives pipeline URL sanitization");
  const root = parseMarkdownToHast(`[report](${unc})`);
  const html = renderBlocks([{ key: "unc", children: root.children }]);
  ok(html.includes(`href="${unc}"`), "authority-form UNC href survives HAST rendering");
}

// Windows device namespaces and alternate data streams must not be restored
// after react-markdown's default URL sanitizer rejects their file: scheme.
for (const unsafe of [
  "file://./PhysicalDrive0",
  "file:////?/C:/Windows",
  "file:///C:/safe.txt:payload",
  "file:///tmp/report.md?download=1",
  "file:///tmp/report.md#section",
]) {
  eq(markdownUrlTransform(unsafe), "", `unsafe local URL is blanked: ${unsafe}`);
}

// Content revision + byte weight.
{
  eq(markdownContentRevision("alpha") === markdownContentRevision("alpha"), true, "content revision is deterministic");
  eq(markdownContentRevision("alpha") !== markdownContentRevision("beta"), true, "content revision distinguishes texts");
  const blocks = parseMarkdownToBlocks("hello **world**");
  const bytes = estimateHastBytes(blocks);
  ok(bytes > 0 && bytes < 100_000, "hast byte estimate is positive and bounded");
  const bigBlocks = parseMarkdownToBlocks(bigCode);
  ok(estimateHastBytes(bigBlocks) > bytes, "hast byte estimate grows with content");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
