// Run: tsx src/__tests__/markdown-streaming-worker.test.tsx
//
// Streaming integration: while an answer streams, rendering stays on the
// incremental-commit path (main thread, StreamingMarkdownTail) and the worker
// is never touched; when the stream COMPLETES, the final full parse routes
// through the markdown worker client, the held committed view stays on screen
// until blocks arrive, and the worker-parsed blocks then swap in.
//
// Loads the component through a middleware-mode vite server (like the
// transcript harness) so the lazy MarkdownRenderer/MarkdownHistory chunks —
// including the katex stylesheet — resolve under tsx.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { createServer } from "vite";
import type { MarkdownBlock, MarkdownParseResult } from "../lib/markdownPipeline";
import type { MarkdownWorkerClient as MarkdownWorkerClientType } from "../lib/markdownWorkerClient";

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

const flush = () => act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 20));
});

// Lazy module resolution is I/O, not a twenty-millisecond rendering contract.
// Drain React/event-loop work until the observable worker handshake completes;
// the deadline is only a failure bound, never the success condition.
async function until(condition: () => boolean) {
  const deadline = Date.now() + 5_000;
  while (!condition()) {
    if (Date.now() >= deadline) throw new Error("Markdown worker handshake did not settle");
    await act(async () => { await new Promise<void>((resolve) => setImmediate(resolve)); });
  }
}

const server = await createServer({
  appType: "custom",
  logLevel: "silent",
  server: { middlewareMode: true },
});
const { Markdown } = await server.ssrLoadModule("/src/components/Markdown.tsx");
// Preload the lazy chunks so runtime React.lazy imports resolve from the
// module-runner cache instead of racing renders against fetchModule.
await server.ssrLoadModule("/src/components/MarkdownRenderer.tsx");
await server.ssrLoadModule("/src/components/MarkdownHistory.tsx");
const workerModule = await server.ssrLoadModule("/src/lib/markdownWorkerClient.ts");

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");

const WORKER_BLOCKS: MarkdownBlock[] = [{
  key: "b0",
  children: [{
    type: "element",
    tagName: "p",
    properties: {},
    children: [{ type: "text", value: "WORKER-PARSED-FINAL" }],
  }],
}];
const WORKER_RESULT: MarkdownParseResult = {
  blocks: WORKER_BLOCKS,
  selectionText: "WORKER-PARSED-FINAL",
  selectionRevision: 1,
};

console.log("\nmarkdown streaming → worker final parse");

{
  const parseCalls: string[] = [];
  let respond: (() => void) | null = null;
  const fakeWorker = {
    onmessage: null as ((event: { data: unknown }) => void) | null,
    onerror: null,
    postMessage(request: { id: number; text: string }) {
      parseCalls.push(request.text);
      respond = () => this.onmessage?.({ data: { id: request.id, result: WORKER_RESULT } });
    },
    terminate() {},
  };
  (globalThis as { Worker?: unknown }).Worker = class {};
  const client: MarkdownWorkerClientType = new workerModule.MarkdownWorkerClient({
    createWorker: () => Promise.resolve(fakeWorker),
  });
  workerModule.setMarkdownWorkerClientForTest(client);

  const streamed = "a".repeat(8_100);
  const finalText = `${streamed} final`;
  const root = createRoot(rootEl);

  await act(async () => {
    root.render(<Markdown text={streamed} streaming />);
  });
  await flush();
  eq(parseCalls.length, 0, "streaming never touches the parse worker");
  ok(rootEl.textContent?.includes(streamed.slice(0, 80)), "the streaming text renders while live");

  await act(async () => {
    root.render(<Markdown text={finalText} streaming={false} />);
  });
  await until(() => parseCalls.length > 0);
  eq(parseCalls.length, 1, "stream completion requests exactly one final parse");
  eq(parseCalls[0], finalText, "the final parse receives the complete text");
  const tail = rootEl.querySelector(".md--stream-tail");
  eq(tail?.textContent, " final", "the committed view + tail stay on screen until blocks arrive");

  await act(async () => {
    respond?.();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  await until(() => rootEl.textContent === "WORKER-PARSED-FINAL");
  ok(rootEl.querySelector(".md[data-markdown-blocks]"), "worker-parsed blocks swap in after completion");
  eq(rootEl.textContent, "WORKER-PARSED-FINAL", "the swapped content is the worker render");
  ok(!rootEl.querySelector(".md--stream-tail"), "the streaming tail unmounts after the swap");

  await act(async () => root.unmount());
  workerModule.disposeMarkdownWorkerClient();
  delete (globalThis as { Worker?: unknown }).Worker;
}

console.log("\nstreaming code fence tail styling");

// An unclosed fence stays in the streaming tail (never committed early) and
// renders with code-block styling until the closing fence arrives (#8843).
{
  const root = createRoot(rootEl);

  await act(async () => {
    root.render(<Markdown text={"intro\n\n"} streaming />);
    await flush();
  });

  await act(async () => {
    root.render(<Markdown text={"intro\n\n```js\nconst a = 1;"} streaming />);
    await flush();
  });
  const tail = rootEl.querySelector(".md--stream-tail");
  ok(tail, "an open code fence keeps the uncommitted tail mounted");
  const pre = tail?.querySelector("pre.code.md--stream-tail-code");
  ok(pre, "an open code fence renders with code styling before the closing fence arrives");
  eq(pre?.getAttribute("data-lang"), "js", "the streaming code block carries the fence language");
  eq(pre?.textContent, "const a = 1;", "the streaming code block shows the fence body without the opener line");

  await act(async () => {
    root.render(<Markdown text={"intro\n\n```js\nconst a = 1;\nconst b = 2;"} streaming />);
    await flush();
  });
  eq(rootEl.querySelector(".md--stream-tail pre code")?.textContent, "const a = 1;\nconst b = 2;", "the streaming code body grows in place");

  await act(async () => {
    root.render(<Markdown text={"intro\n\n```js\nconst a = 1;\nconst b = 2;\n```\ndone"} streaming />);
    await new Promise((resolve) => setTimeout(resolve, 120));
  });
  // The closed fence commits on the next animation frame after the parse
  // budget; let that frame run inside act before asserting.
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 60));
  });
  const settledTail = rootEl.querySelector(".md--stream-tail");
  ok(!settledTail?.querySelector("pre.md--stream-tail-code"), "a closed fence leaves the code styling to the parsed renderer");
  eq(settledTail?.textContent, "done", "text after the closing fence rides the plain tail");

  await act(async () => root.unmount());
}

console.log("\nlive-footer → virtual row remount fallback");

{
  const requests: Array<{ id: number; text: string }> = [];
  const fakeWorker = {
    onmessage: null as ((event: { data: unknown }) => void) | null,
    onerror: null,
    postMessage(request: { id: number; text: string }) {
      requests.push(request);
    },
    terminate() {},
  };
  (globalThis as { Worker?: unknown }).Worker = class {};
  const client: MarkdownWorkerClientType = new workerModule.MarkdownWorkerClient({
    createWorker: () => Promise.resolve(fakeWorker),
  });
  workerModule.setMarkdownWorkerClientForTest(client);

  const text = "# Final title\n\n**formatted**\n\n| A | B |\n|---|---|\n| 1 | 2 |";
  const renderRow = () => (
    <div className="transcript">
      <Markdown text={text} streaming={false} wasStreamed cacheKey="a:handoff:0:answer" />
    </div>
  );
  const first = createRoot(rootEl);
  await act(async () => {
    first.render(renderRow());
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
  ok(rootEl.querySelector("h1")?.textContent === "Final title", "completed live row keeps its formatted heading while worker parsing is pending");
  ok(rootEl.querySelector("strong")?.textContent === "formatted", "completed live row keeps formatted emphasis");
  ok(Boolean(rootEl.querySelector("table")), "completed live row keeps a formatted table");

  await act(async () => first.unmount());
  const second = createRoot(rootEl);
  await act(async () => {
    second.render(renderRow());
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
  const scroller = rootEl.querySelector(".transcript") as HTMLElement;
  Object.defineProperty(scroller, "clientHeight", { configurable: true, value: 200 });
  Object.defineProperty(scroller, "scrollHeight", { configurable: true, value: 1_000 });
  Object.defineProperty(scroller, "scrollTop", { configurable: true, value: 100 });
  ok(requests.length >= 2, "virtual remount requests parsing under the same stable cache key");
  ok(rootEl.querySelector("h1")?.textContent === "Final title", "off-bottom remount never falls back to raw heading source");
  ok(rootEl.querySelector("strong")?.textContent === "formatted", "off-bottom remount preserves emphasis formatting");
  ok(Boolean(rootEl.querySelector("table")), "off-bottom remount preserves table formatting");

  await act(async () => second.unmount());
  workerModule.disposeMarkdownWorkerClient();
  delete (globalThis as { Worker?: unknown }).Worker;
}

await server.close();
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
