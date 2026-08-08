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
import type { MarkdownBlock } from "../lib/markdownPipeline";
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

console.log("\nmarkdown streaming → worker final parse");

{
  const parseCalls: string[] = [];
  let respond: (() => void) | null = null;
  const fakeWorker = {
    onmessage: null as ((event: { data: unknown }) => void) | null,
    onerror: null,
    postMessage(request: { id: number; text: string }) {
      parseCalls.push(request.text);
      respond = () => this.onmessage?.({ data: { id: request.id, blocks: WORKER_BLOCKS } });
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
  await flush();
  eq(parseCalls.length, 1, "stream completion requests exactly one final parse");
  eq(parseCalls[0], finalText, "the final parse receives the complete text");
  const tail = rootEl.querySelector(".md--stream-tail");
  eq(tail?.textContent, " final", "the committed view + tail stay on screen until blocks arrive");

  await act(async () => {
    respond?.();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  await flush();
  ok(rootEl.querySelector(".md[data-markdown-blocks]"), "worker-parsed blocks swap in after completion");
  eq(rootEl.textContent, "WORKER-PARSED-FINAL", "the swapped content is the worker render");
  ok(!rootEl.querySelector(".md--stream-tail"), "the streaming tail unmounts after the swap");

  await act(async () => root.unmount());
  workerModule.disposeMarkdownWorkerClient();
  delete (globalThis as { Worker?: unknown }).Worker;
}

await server.close();
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
