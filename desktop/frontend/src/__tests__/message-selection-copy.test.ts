// Run: tsx src/__tests__/message-selection-copy.test.ts

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import { JSDOM } from "jsdom";

import { reasonixRehypePlugins, reasonixRemarkPlugins } from "../components/markdownRemarkPlugins";
import { normalizeMath } from "../components/mathNormalize";
import {
  installMessageSelectionCopy,
  messageSelectionContextText,
  messageSelectionCopyText,
} from "../lib/messageSelectionCopy";
import { transcriptSelectionStore, type TranscriptSelectableRow } from "../lib/transcriptSelectionStore";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nmessage selection copy");

eq(
  messageSelectionCopyText({
    text: "selected assistant reply",
    isCollapsed: false,
    targetIsEditable: false,
    intersectsMessage: true,
    canWriteClipboard: true,
  }),
  "selected assistant reply",
  "copies selected message text through the fallback handler",
);

eq(
  messageSelectionCopyText({
    text: "draft text",
    isCollapsed: false,
    targetIsEditable: true,
    intersectsMessage: true,
    canWriteClipboard: true,
  }),
  null,
  "does not override native copy inside editable fields",
);

eq(
  messageSelectionCopyText({
    text: "   \n\t",
    isCollapsed: false,
    targetIsEditable: false,
    intersectsMessage: true,
    canWriteClipboard: true,
  }),
  null,
  "ignores whitespace-only selections",
);

eq(
  messageSelectionCopyText({
    text: "settings panel text",
    isCollapsed: false,
    targetIsEditable: false,
    intersectsMessage: false,
    canWriteClipboard: true,
  }),
  null,
  "leaves non-message selections to the browser",
);

eq(
  messageSelectionCopyText({
    text: "selected assistant reply",
    isCollapsed: true,
    targetIsEditable: false,
    intersectsMessage: true,
    canWriteClipboard: true,
  }),
  null,
  "ignores collapsed selections",
);

eq(
  messageSelectionCopyText({
    text: "selected assistant reply",
    isCollapsed: false,
    targetIsEditable: false,
    intersectsMessage: true,
    canWriteClipboard: false,
  }),
  null,
  "does not claim copy events without writable clipboard data",
);

function installDOMGlobals(dom: JSDOM) {
  Object.assign(globalThis, {
    Node: dom.window.Node,
    Element: dom.window.Element,
    HTMLElement: dom.window.HTMLElement,
  });
}

function inlineKatex(source: string, rendered: string): string {
  return `<span class="katex"><span class="katex-mathml"><math><semantics>` +
    `<annotation encoding="application/x-tex">${source}</annotation>` +
    `</semantics></math></span><span class="katex-html">${rendered}</span></span>`;
}

{
  const dom = new JSDOM(
    `<div class="msg__body" id="message">before ${inlineKatex("\\alpha", "α")} after</div>`,
  );
  installDOMGlobals(dom);
  const doc = dom.window.document;
  const message = doc.getElementById("message")!;
  const range = doc.createRange();
  range.selectNodeContents(message);
  const selection = dom.window.getSelection()!;
  selection.addRange(range);
  eq(
    messageSelectionContextText(doc, message),
    "before $\\alpha$ after",
    "restores one inline formula without dropping surrounding prose",
  );
}

{
  const dom = new JSDOM(
    `<div class="msg__body" id="message">${inlineKatex("\\frac{1}{2}", "½")}</div>`,
  );
  installDOMGlobals(dom);
  const doc = dom.window.document;
  const message = doc.getElementById("message")!;
  const rendered = message.querySelector(".katex-html")!.firstChild!;
  const range = doc.createRange();
  range.setStart(rendered, 0);
  range.setEnd(rendered, 1);
  const selection = dom.window.getSelection()!;
  selection.addRange(range);
  eq(
    messageSelectionContextText(doc, message),
    "$\\frac{1}{2}$",
    "selecting part of a formula copies the complete source",
  );
}

{
  const dom = new JSDOM(
    `<div class="msg__body" id="message">x and ${inlineKatex("x", "x")} plus x</div>`,
  );
  installDOMGlobals(dom);
  const doc = dom.window.document;
  const message = doc.getElementById("message")!;
  const range = doc.createRange();
  range.selectNodeContents(message);
  const selection = dom.window.getSelection()!;
  selection.addRange(range);
  eq(
    messageSelectionContextText(doc, message),
    "x and $x$ plus x",
    "does not replace identical prose around a formula",
  );
}

{
  const dom = new JSDOM(
    `<div class="msg__body" id="message">before <span class="katex-display">` +
      `${inlineKatex("x^2", "x²")}</span> after</div>`,
  );
  installDOMGlobals(dom);
  const doc = dom.window.document;
  const message = doc.getElementById("message")!;
  const range = doc.createRange();
  range.selectNodeContents(message);
  const selection = dom.window.getSelection()!;
  selection.addRange(range);
  eq(
    messageSelectionContextText(doc, message),
    "before $$\nx^2\n$$ after",
    "restores display math delimiters",
  );
}

{
  const rendered = renderToStaticMarkup(
    createElement(ReactMarkdown, {
      remarkPlugins: reasonixRemarkPlugins,
      rehypePlugins: reasonixRehypePlugins,
      children: normalizeMath("before $|x|$ then $x = 50%$ after"),
    }),
  );
  const dom = new JSDOM(`<div class="msg__body" id="message">${rendered}</div>`);
  installDOMGlobals(dom);
  const doc = dom.window.document;
  const message = doc.getElementById("message")!;
  const range = doc.createRange();
  range.selectNodeContents(message);
  const selection = dom.window.getSelection()!;
  selection.addRange(range);
  eq(
    messageSelectionContextText(doc, message),
    "before $|x|$ then $x = 50%$ after",
    "real Markdown rendering copies original TeX instead of normalized KaTeX input",
  );
}

{
  const rendered = renderToStaticMarkup(
    createElement(ReactMarkdown, {
      remarkPlugins: reasonixRemarkPlugins,
      rehypePlugins: reasonixRehypePlugins,
      children: normalizeMath("before $\\alpha $ then $  x  $ after"),
    }),
  );
  const dom = new JSDOM(`<div class="msg__body" id="message">${rendered}</div>`);
  installDOMGlobals(dom);
  const doc = dom.window.document;
  const message = doc.getElementById("message")!;
  const range = doc.createRange();
  range.selectNodeContents(message);
  const selection = dom.window.getSelection()!;
  selection.addRange(range);
  eq(
    messageSelectionContextText(doc, message),
    "before $\\alpha $ then $  x  $ after",
    "preserves authored delimiter padding through real Markdown rendering",
  );
}

{
  const rendered = renderToStaticMarkup(
    createElement(ReactMarkdown, {
      remarkPlugins: reasonixRemarkPlugins,
      rehypePlugins: reasonixRehypePlugins,
      children: normalizeMath("before $V=\\yng(2,1) | x$ then $$\\young(ab,c)$$ after"),
    }),
  );
  const dom = new JSDOM(`<div class="msg__body" id="message">${rendered}</div>`);
  installDOMGlobals(dom);
  const doc = dom.window.document;
  const message = doc.getElementById("message")!;
  const range = doc.createRange();
  range.selectNodeContents(message);
  const selection = dom.window.getSelection()!;
  selection.addRange(range);
  eq(
    messageSelectionContextText(doc, message),
    "before $V=\\yng(2,1) | x$ then\n$$\n\\young(ab,c)\n$$\nafter",
    "copies authored Young macros through nested pipe protection",
  );
}

{
  const dom = new JSDOM("<!doctype html><body><div class=\"msg__body\">logical</div></body>");
  installDOMGlobals(dom);
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  const writes: string[] = [];
  Object.defineProperty(dom.window.navigator, "clipboard", {
    configurable: true,
    value: { writeText: async (text: string) => { writes.push(text); } },
  });
  let finish!: (text: string) => void;
  const delayed = new Promise<string>((resolve) => { finish = resolve; });
  const rows: TranscriptSelectableRow[] = [{
    rowKey: "a",
    sourceText: "fallback",
    contentRevision: 1,
    resolveText: () => delayed,
  }];
  transcriptSelectionStore.clear("copy-test-reset");
  transcriptSelectionStore.beginNative("tab-copy");
  transcriptSelectionStore.promoteToLogical(
    "tab-copy",
    { rowKey: "a", textOffset: 0, affinity: "forward" },
    { rowKey: "a", textOffset: 8, affinity: "forward" },
    rows,
  );
  transcriptSelectionStore.settleLogical();
  const uninstall = installMessageSelectionCopy(document);
  const staleCopy = new window.Event("copy", { bubbles: true, cancelable: true });
  document.dispatchEvent(staleCopy);
  eq(staleCopy.defaultPrevented, true, "logical copy claims the event before async projection resolves");
  transcriptSelectionStore.clear("tab-switch");
  finish("resolved");
  await new Promise((resolve) => setTimeout(resolve, 0));
  eq(writes.length, 0, "late logical copy result cannot write after its snapshot is cleared");

  transcriptSelectionStore.beginNative("tab-copy");
  transcriptSelectionStore.promoteToLogical(
    "tab-copy",
    { rowKey: "a", textOffset: 0, affinity: "forward" },
    { rowKey: "a", textOffset: 8, affinity: "forward" },
    [{ ...rows[0], resolveText: async () => "resolved" }],
  );
  transcriptSelectionStore.settleLogical();
  document.dispatchEvent(new window.Event("copy", { bubbles: true, cancelable: true }));
  await new Promise((resolve) => setTimeout(resolve, 0));
  eq(writes[0], "resolved", "logical keyboard copy writes the frozen text projection");
  eq(transcriptSelectionStore.getSnapshot().mode, "none", "successful logical keyboard copy clears the snapshot");
  uninstall();
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
