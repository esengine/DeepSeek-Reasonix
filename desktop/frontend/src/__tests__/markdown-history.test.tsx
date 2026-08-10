// Run: tsx src/__tests__/markdown-history.test.tsx
//
// MarkdownHistory (worker-driven history rendering): transcript markdown cache
// hits avoid re-parsing, revision (content) changes re-parse, and huge
// documents mount their blocks progressively via idle callbacks. Uses the
// in-process fallback client with a spy — jsdom has no Worker.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import MarkdownHistory from "../components/MarkdownHistory";
import { parseMarkdownToBlocks, markdownContentRevision } from "../lib/markdownPipeline";
import {
  disposeMarkdownWorkerClient,
  MarkdownWorkerClient,
  setMarkdownWorkerClientForTest,
} from "../lib/markdownWorkerClient";
import { getTranscriptStore } from "../lib/transcriptStore";

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

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

let pendingIdle: Array<() => void> = [];
Object.defineProperty(dom.window, "requestIdleCallback", {
  configurable: true,
  value: (callback: () => void) => {
    pendingIdle.push(callback);
    return pendingIdle.length;
  },
});
Object.defineProperty(dom.window, "cancelIdleCallback", {
  configurable: true,
  value: () => undefined,
});

function runIdle() {
  const callbacks = pendingIdle;
  pendingIdle = [];
  for (const callback of callbacks) callback();
}

const flush = () => act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 0));
});

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

const parseCalls: string[] = [];
// Installed after the deferred-worker section: setMarkdownWorkerClientForTest
// disposes the previous singleton, so each section gets a fresh client.
const newSpyClient = () => new MarkdownWorkerClient({
  parseInProcess: (text) => {
    parseCalls.push(text);
    return parseMarkdownToBlocks(text);
  },
});

console.log("\nmarkdown history rendering");

// ── parse → render → cache; second mount does not re-parse ──────────────────
{
  const text = "# Cached\n\nFirst **render** parses.\n\nSecond mount must not.";
  const entryId = "md-history-cache-1";

  // A deferred fake worker keeps the parse in flight so the fallback can be
  // observed; the global Worker stub routes the client down the worker path.
  (globalThis as { Worker?: unknown }).Worker = class {};
  let resolveParse: ((blocks: ReturnType<typeof parseMarkdownToBlocks>) => void) | null = null;
  const deferred = new MarkdownWorkerClient({
    createWorker: () => Promise.resolve({
      onmessage: null,
      onerror: null,
      postMessage(request) {
        resolveParse = (blocks) => {
          const message = { data: { id: request.id, blocks } };
          (this.onmessage as ((event: unknown) => void) | null)?.(message);
        };
      },
      terminate() {},
    }),
  });
  setMarkdownWorkerClientForTest(deferred);

  await act(async () => {
    root.render(<MarkdownHistory text={text} entryId={entryId} fallback={<div className="md">{text}</div>} />);
  });
  eq(rootEl.textContent, text, "fallback shows the full text while parsing");
  await act(async () => {
    resolveParse?.(parseMarkdownToBlocks(text));
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  ok(rootEl.querySelector('.md[data-markdown-blocks="3"]'), "parsed blocks render after the worker resolves");
  ok(rootEl.querySelector(".md strong"), "rendered blocks carry real markdown structure");
  delete (globalThis as { Worker?: unknown }).Worker;
  setMarkdownWorkerClientForTest(newSpyClient());
  parseCalls.length = 0; // cache assertions count parses from here

  const revision = markdownContentRevision(text);
  const cached = getTranscriptStore().getMarkdown(entryId, revision);
  ok(cached && cached.source === text, "parsed blocks land in the transcript cache");

  await act(async () => root.unmount());
  const root2 = createRoot(rootEl);
  await act(async () => {
    root2.render(<MarkdownHistory text={text} entryId={entryId} fallback={<div className="md">{text}</div>} />);
  });
  await flush();
  eq(parseCalls.length, 0, "second mount of the same entryId+revision does not re-parse");
  ok(rootEl.querySelector('.md[data-markdown-blocks="3"]'), "cache hit renders blocks synchronously");
  await act(async () => root2.unmount());
}

// ── revision change (new text, same entry) re-parses ─────────────────────────
{
  const entryId = "md-history-cache-2";
  const root3 = createRoot(rootEl);
  await act(async () => {
    root3.render(<MarkdownHistory text="version one" entryId={entryId} fallback={null} />);
  });
  await flush();
  eq(parseCalls.length, 1, "first version parses");
  eq(parseCalls[0], "version one", "the parse receives the exact source text");
  await act(async () => {
    root3.render(<MarkdownHistory text="version two" entryId={entryId} fallback={null} />);
  });
  await flush();
  eq(parseCalls.length, 2, "changed content (new revision) re-parses");
  ok(rootEl.textContent?.includes("version two"), "the re-parsed content renders");
  await act(async () => root3.unmount());
}

// ── rows without an entryId skip the cache entirely ──────────────────────────
{
  const root4 = createRoot(rootEl);
  await act(async () => {
    root4.render(<MarkdownHistory text="uncached live text" fallback={null} />);
  });
  await flush();
  eq(parseCalls.length, 3, "live rows parse without a cache key");
  await act(async () => root4.unmount());
}

// ── progressive mounting for huge documents ──────────────────────────────────
{
  const text = Array.from({ length: 60 }, (_, i) => `Paragraph ${i} with some *content*.`).join("\n\n");
  const root5 = createRoot(rootEl);
  await act(async () => {
    root5.render(<MarkdownHistory text={text} entryId="md-history-huge" fallback={null} />);
  });
  await flush();
  const container = rootEl.querySelector(".md[data-markdown-blocks]");
  ok(container, "huge document renders through blocks");
  eq(container?.getAttribute("data-markdown-blocks"), "60", "all 60 blocks are in the render model");
  eq(container?.getAttribute("data-markdown-visible-blocks"), "24", "visible block count exposes the initial idle chunk");
  const initialCount = container?.children.length ?? 0;
  eq(initialCount, 24, "first commit mounts only the initial block chunk");
  for (let i = 0; i < 5 && (rootEl.querySelector(".md[data-markdown-blocks]")?.children.length ?? 0) < 60; i += 1) {
    await act(async () => {
      runIdle();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.children.length, 60, "idle callbacks mount the remaining blocks");
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-visible-blocks"), "60", "visible block count reaches the render model");
  ok(rootEl.textContent?.includes("Paragraph 59"), "the final block's content mounts");
  await act(async () => root5.unmount());
}

// ── worker failure falls back through onError ────────────────────────────────
{
  const failing = new MarkdownWorkerClient({
    parseInProcess: () => {
      throw new Error("kaboom");
    },
  });
  setMarkdownWorkerClientForTest(failing);
  let errors = 0;
  const root6 = createRoot(rootEl);
  await act(async () => {
    root6.render(
      <MarkdownHistory text="broken" fallback={<div className="md">broken</div>} onError={() => { errors += 1; }} />,
    );
  });
  await flush();
  eq(errors, 1, "a parse failure surfaces through onError");
  eq(rootEl.textContent, "broken", "the fallback stays on screen after a failure");
  await act(async () => root6.unmount());
  setMarkdownWorkerClientForTest(newSpyClient());
}

disposeMarkdownWorkerClient();
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
