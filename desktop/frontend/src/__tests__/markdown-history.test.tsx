// Run: tsx src/__tests__/markdown-history.test.tsx
//
// MarkdownHistory (worker-driven history rendering): transcript markdown cache
// hits avoid re-parsing, revision (content) changes re-parse, and huge
// documents keep a viewport-driven tail window. Uses the in-process fallback
// client with a spy — jsdom has no Worker.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import MarkdownHistory from "../components/MarkdownHistory";
import { TranscriptScrollWriteProvider } from "../components/TranscriptLayoutIntentContext";
import { parseMarkdown, markdownContentRevision } from "../lib/markdownPipeline";
import {
  nativeTranscriptDistanceFromBottom,
  observeNativeTranscriptTailClamp,
} from "../lib/transcriptScrollGeometry";
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

const intersectionCallbacks = new Map<Element, IntersectionObserverCallback>();
class TestIntersectionObserver {
  readonly callback: IntersectionObserverCallback;
  readonly targets = new Set<Element>();
  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback;
  }
  observe(target: Element) {
    this.targets.add(target);
    intersectionCallbacks.set(target, this.callback);
  }
  unobserve(target: Element) {
    this.targets.delete(target);
    intersectionCallbacks.delete(target);
  }
  disconnect() {
    for (const target of this.targets) intersectionCallbacks.delete(target);
    this.targets.clear();
  }
  takeRecords(): IntersectionObserverEntry[] { return []; }
  readonly root = null;
  readonly rootMargin = "0px";
  readonly thresholds = [0];
}
globalThis.IntersectionObserver = TestIntersectionObserver as unknown as typeof IntersectionObserver;
dom.window.IntersectionObserver = TestIntersectionObserver as unknown as typeof IntersectionObserver;

async function intersectSentinel(selector: string, isIntersecting: boolean) {
  const sentinel = rootEl?.querySelector(selector);
  if (!sentinel) throw new Error(`missing sentinel ${selector}`);
  await act(async () => {
    intersectionCallbacks.get(sentinel)?.([{ isIntersecting, target: sentinel } as IntersectionObserverEntry], {} as IntersectionObserver);
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

async function intersectSentinels(selectors: string[], isIntersecting: boolean) {
  const sentinels = selectors.map((selector) => {
    const sentinel = rootEl?.querySelector(selector);
    if (!sentinel) throw new Error(`missing sentinel ${selector}`);
    return sentinel;
  });
  await act(async () => {
    for (const sentinel of sentinels) {
      intersectionCallbacks.get(sentinel)?.([{ isIntersecting, target: sentinel } as IntersectionObserverEntry], {} as IntersectionObserver);
    }
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
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
    return parseMarkdown(text);
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
  let resolveParse: ((result: ReturnType<typeof parseMarkdown>) => void) | null = null;
  const deferred = new MarkdownWorkerClient({
    createWorker: () => Promise.resolve({
      onmessage: null,
      onerror: null,
      postMessage(request) {
        resolveParse = (result) => {
          const message = { data: { id: request.id, result } };
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
    resolveParse?.(parseMarkdown(text));
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

// ── a worker result cannot replace the reader's mid-document fallback ────────
{
  rootEl.className = "transcript";
  rootEl.scrollTop = 400;
  Object.defineProperty(rootEl, "scrollHeight", { configurable: true, value: 1_000 });
  Object.defineProperty(rootEl, "clientHeight", { configurable: true, value: 300 });
  // The pending Virtuoso row sits inside the transcript viewport: the long answer
  // keeps the stable fallback because the tail-window swap would remove the
  // blocks being read (#9570 keeps that protection; short answers and
  // off-screen rows commit immediately now).
  const originalRectMidRead = dom.window.HTMLElement.prototype.getBoundingClientRect;
  dom.window.HTMLElement.prototype.getBoundingClientRect = function getBoundingClientRect() {
    if (this.classList.contains("transcript")) {
      return { top: 50, bottom: 350, left: 0, right: 800, width: 800, height: 300, x: 0, y: 50, toJSON: () => ({}) };
    }
    if (this.classList.contains("transcript__row")) {
      return { top: 100, bottom: 400, left: 0, right: 0, width: 0, height: 300, x: 0, y: 100, toJSON: () => ({}) };
    }
    return { top: 0, bottom: 0, left: 0, right: 0, width: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) };
  };
  (globalThis as { Worker?: unknown }).Worker = class {};
  let resolveParse: ((result: ReturnType<typeof parseMarkdown>) => void) | null = null;
  const deferred = new MarkdownWorkerClient({
    createWorker: () => Promise.resolve({
      onmessage: null,
      onerror: null,
      postMessage(request) {
        resolveParse = (result) => {
          const message = { data: { id: request.id, result } };
          (this.onmessage as ((event: unknown) => void) | null)?.(message);
        };
      },
      terminate() {},
    }),
  });
  setMarkdownWorkerClientForTest(deferred);
  const root5 = createRoot(rootEl);
  const text = Array.from({ length: 60 }, (_, index) => `# Anchor ${index + 1}\n\nBody ${index + 1}.`).join("\n\n");
  await act(async () => {
    root5.render(
      <div className="transcript__row">
        <MarkdownHistory text={text} entryId="md-history-mid-read" fallback={<div className="md">{text}</div>} />
      </div>,
    );
  });
  await act(async () => {
    resolveParse?.(parseMarkdown(text));
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  ok(!rootEl.querySelector(".md[data-markdown-blocks]"), "worker completion keeps the full fallback while the reader is away from bottom");
  ok(rootEl.textContent?.includes("Anchor 1"), "the reader's prefix remains mounted during the deferred handoff");

  rootEl.scrollTop = 692;
  ok(!observeNativeTranscriptTailClamp(rootEl, 692), "the first native no-op tail write remains pending");
  ok(observeNativeTranscriptTailClamp(rootEl, 692), "a repeated native no-op tail write confirms the reachable bottom");
  eq(nativeTranscriptDistanceFromBottom(rootEl), 0, "the shared transcript geometry treats the native clamp as bottom");
  await act(async () => {
    rootEl.dispatchEvent(new dom.window.Event("scroll"));
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  ok(rootEl.querySelector('.md[data-markdown-blocks="120"]'), "returning to bottom commits the cached parsed blocks");
  ok(rootEl.textContent?.includes("Anchor 60"), "the parsed tail is visible after the safe handoff");
  await act(async () => root5.unmount());
  dom.window.HTMLElement.prototype.getBoundingClientRect = originalRectMidRead;
  rootEl.className = "";
  delete (globalThis as { Worker?: unknown }).Worker;
  setMarkdownWorkerClientForTest(newSpyClient());
}

// ── a short visible answer renders even when the reader is not at bottom ──
{
  rootEl.className = "transcript";
  rootEl.scrollTop = 400;
  Object.defineProperty(rootEl, "scrollHeight", { configurable: true, value: 1_000 });
  Object.defineProperty(rootEl, "clientHeight", { configurable: true, value: 300 });
  const root5a = createRoot(rootEl);
  const text = "# Short answer\n\nThis **must render** without a trip to the bottom.";
  await act(async () => {
    root5a.render(
      <div className="transcript__row">
        <MarkdownHistory text={text} entryId="md-history-short-visible" fallback={<div className="md">{text}</div>} />
      </div>,
    );
  });
  await flush();
  ok(rootEl.querySelector('.md[data-markdown-blocks="2"]'), "a short visible answer commits while the reader remains above the bottom");
  ok(rootEl.querySelector(".md strong"), "the short answer exposes rendered markdown instead of the raw fallback");
  await act(async () => root5a.unmount());
  rootEl.className = "";
}

// ── an off-screen long answer commits without waiting for the reader ─────────
{
  rootEl.className = "transcript";
  rootEl.scrollTop = 400;
  Object.defineProperty(rootEl, "scrollHeight", { configurable: true, value: 50_000 });
  Object.defineProperty(rootEl, "clientHeight", { configurable: true, value: 300 });
  const originalRectOffscreen = dom.window.HTMLElement.prototype.getBoundingClientRect;
  dom.window.HTMLElement.prototype.getBoundingClientRect = function getBoundingClientRect() {
    if (this.classList.contains("transcript")) {
      return { top: 50, bottom: 350, left: 0, right: 800, width: 800, height: 300, x: 0, y: 50, toJSON: () => ({}) };
    }
    if (this.classList.contains("transcript__row")) {
      return { top: 500, bottom: 800, left: 0, right: 800, width: 800, height: 300, x: 0, y: 500, toJSON: () => ({}) };
    }
    return { top: 0, bottom: 0, left: 0, right: 0, width: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) };
  };
  // A mounted overscan row outside the transcript viewport must render instead
  // of waiting for a bottom scroll that may never come (#9570).
  const root5b = createRoot(rootEl);
  const text = Array.from({ length: 90 }, (_, index) => `# Away ${index + 1}\n\nBody ${index + 1}.`).join("\n\n");
  await act(async () => {
    root5b.render(
      <div className="transcript__row">
        <MarkdownHistory text={text} entryId="md-history-offscreen" fallback={<div className="md">{text}</div>} />
      </div>,
    );
  });
  await flush();
  ok(rootEl.querySelector('.md[data-markdown-blocks="180"]'), "the off-screen long answer renders immediately");
  ok(!rootEl.textContent?.includes("Away 1\n\nBody 1."), "the raw fallback is replaced by the bounded tail window");
  await act(async () => root5b.unmount());
  dom.window.HTMLElement.prototype.getBoundingClientRect = originalRectOffscreen;
  rootEl.className = "";
  delete (globalThis as { Worker?: unknown }).Worker;
  setMarkdownWorkerClientForTest(newSpyClient());
}

// ── progressive mounting for huge documents ──────────────────────────────────
{
  rootEl.className = "transcript";
  rootEl.scrollTop = 1_000;
  Object.defineProperty(rootEl, "scrollHeight", { configurable: true, value: 1_300 });
  Object.defineProperty(rootEl, "clientHeight", { configurable: true, value: 300 });
  const originalRect = dom.window.HTMLElement.prototype.getBoundingClientRect;
  dom.window.HTMLElement.prototype.getBoundingClientRect = function getBoundingClientRect() {
    const top = this.hasAttribute("data-markdown-older-sentinel")
      ? 100
      : this.hasAttribute("data-markdown-newer-sentinel")
        ? 700
        : this.hasAttribute("data-markdown-scroll-anchor")
          ? Number(this.getAttribute("data-markdown-scroll-anchor")) < 300 ? 500 : 300
          : 0;
    const height = this.hasAttribute("data-markdown-older-sentinel") ? 1 : 0;
    return { top, bottom: top + height, left: 0, right: 0, width: 0, height, x: 0, y: top, toJSON: () => ({}) };
  };
  const text = Array.from({ length: 420 }, (_, i) => `Paragraph ${i} with some *content*.`).join("\n\n");
  const root6 = createRoot(rootEl);
  const writeTranscriptOffset = (_owner: "block-window-prepend", top: number) => {
    rootEl.scrollTop = top;
    return true;
  };
  await act(async () => {
    root6.render(
      <TranscriptScrollWriteProvider value={writeTranscriptOffset}>
        <MarkdownHistory text={text} entryId="md-history-huge" fallback={null} />
      </TranscriptScrollWriteProvider>,
    );
  });
  await flush();
  const container = rootEl.querySelector(".md[data-markdown-blocks]");
  ok(container, "huge document renders through blocks");
  eq(container?.getAttribute("data-markdown-blocks"), "420", "all 420 blocks are in the render model");
  eq(container?.getAttribute("data-markdown-visible-blocks"), "24", "visible block count exposes the initial tail window");
  const initialCount = container?.children.length ?? 0;
  eq(initialCount, 25, "first commit mounts the tail plus one inert viewport sentinel");
  ok(!rootEl.textContent?.includes("Paragraph 0"), "the cold prefix stays out of the DOM");
  ok(rootEl.textContent?.includes("Paragraph 419"), "the newest block mounts immediately");

  await intersectSentinel("[data-markdown-older-sentinel]", true);
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-visible-blocks"), "120", "entering the older edge prepends one bounded page");
  eq(rootEl.scrollTop, 1_199, "prepending compensates for the old leading block boundary's measured movement");
  await intersectSentinel("[data-markdown-older-sentinel]", true);
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-visible-blocks"), "120", "a stationary sentinel cannot start a render loop");
  await intersectSentinel("[data-markdown-older-sentinel]", false);
  await intersectSentinel("[data-markdown-older-sentinel]", true);
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-visible-blocks"), "216", "leaving and re-entering requests the next page");
  await intersectSentinel("[data-markdown-older-sentinel]", false);
  await intersectSentinel("[data-markdown-older-sentinel]", true);
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-visible-blocks"), "216", "the resident block window stays bounded while paging toward the start");
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-window-start"), "108", "older paging advances the bounded window start");
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-window-end"), "324", "older paging trims cold blocks from the newer edge");
  ok(rootEl.textContent?.includes("Paragraph 108"), "the requested older page is mounted");
  ok(!rootEl.textContent?.includes("Paragraph 419"), "the distant tail is evicted after the window reaches its cap");

  await intersectSentinel("[data-markdown-older-sentinel]", false);
  await intersectSentinels(["[data-markdown-older-sentinel]", "[data-markdown-newer-sentinel]"], true);
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-window-start"), "12", "simultaneous edge callbacks apply only the first window move");
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-window-end"), "228", "an opposite-edge callback cannot overwrite the in-flight window move");

  await intersectSentinel("[data-markdown-newer-sentinel]", false);
  const beforeNewerPage = rootEl.scrollTop;
  await intersectSentinel("[data-markdown-newer-sentinel]", true);
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-window-start"), "108", "newer paging trims cold blocks from the older edge");
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-window-end"), "324", "newer paging advances toward the document tail");
  eq(rootEl.scrollTop, beforeNewerPage - 200, "trimming above compensates for the old trailing boundary's measured movement");
  await intersectSentinel("[data-markdown-newer-sentinel]", false);
  await intersectSentinel("[data-markdown-newer-sentinel]", true);
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-window-start"), "204", "a second newer page keeps the resident window bounded");
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-window-end"), "420", "newer paging can return to the document tail");
  ok(rootEl.textContent?.includes("Paragraph 419"), "newer paging restores the document tail");

  const replacement = Array.from({ length: 420 }, (_, i) => `Replacement ${i} with some *content*.`).join("\n\n");
  await act(async () => {
    root6.render(
      <TranscriptScrollWriteProvider value={writeTranscriptOffset}>
        <MarkdownHistory text={replacement} entryId="md-history-huge-replacement" fallback={null} />
      </TranscriptScrollWriteProvider>,
    );
  });
  await flush();
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-visible-blocks"), "24", "an equal-sized replacement document resets to its own tail");
  ok(!rootEl.textContent?.includes("Replacement 0"), "the replacement cannot inherit the previous document's expanded prefix");
  ok(rootEl.textContent?.includes("Replacement 419"), "the replacement's newest block mounts immediately");
  await intersectSentinel("[data-markdown-older-sentinel]", true);
  eq(rootEl.querySelector(".md[data-markdown-blocks]")?.getAttribute("data-markdown-visible-blocks"), "120", "a replacement document re-arms viewport paging");
  await act(async () => root6.unmount());
  dom.window.HTMLElement.prototype.getBoundingClientRect = originalRect;
  rootEl.className = "";
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
