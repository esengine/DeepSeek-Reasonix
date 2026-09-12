import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { createServer } from "vite";
import { parseMarkdown } from "../lib/markdownPipeline";

const dom = new JSDOM('<div id="root"></div>', { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, HTMLElement: dom.window.HTMLElement,
  Node: dom.window.Node, IS_REACT_ACT_ENVIRONMENT: true,
  requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window), cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window) });
const server = await createServer({ server: { middlewareMode: true }, appType: "custom" });
const { default: History } = await server.ssrLoadModule("/src/components/MarkdownHistory.tsx");
const { LocaleProvider } = await server.ssrLoadModule("/src/lib/i18n.tsx");
const worker = await server.ssrLoadModule("/src/lib/markdownWorkerClient.ts");
const calls: Array<{ id: number; text: string }> = [];
const fake = { onmessage: null as null | ((event: { data: unknown }) => void), onerror: null,
  postMessage(message: { id: number; text: string }) { calls.push(message); }, terminate() {} };
Object.assign(globalThis, { Worker: class {} });
worker.setMarkdownWorkerClientForTest(new worker.MarkdownWorkerClient({ createWorker: async () => fake }));
const host = document.getElementById("root")!;
let root = createRoot(host);
async function render(text: string, streaming = false, onError?: () => void) {
  await act(async () => root.render(<LocaleProvider><History text={text} streaming={streaming} cacheKey="natural-markdown" fallback={<div>{text}</div>} onError={onError} /></LocaleProvider>));
}
async function respond(index: number) {
  const call = calls[index]; assert.ok(call);
  await act(async () => fake.onmessage?.({ data: { id: call.id, result: parseMarkdown(call.text) } }));
}
try {
  const prefix = "A **stable** paragraph.\n\n";
  await render(prefix, true); await respond(0);
  const paragraph = host.querySelector("p")!;
  const selection = window.getSelection()!;
  const range = document.createRange(); range.selectNodeContents(paragraph); selection.addRange(range);
  const selected = selection.toString();
  await render(prefix + "A changing tail", true); await respond(1);
  assert.equal(host.querySelector("p"), paragraph, "stream prefix retains its native DOM host");
  await render(prefix + "A changing tail\n\n[ref][id]\n\n[id]: https://example.com"); await respond(2);
  assert.equal(host.querySelector("p"), paragraph, "settlement keeps the same paragraph");
  assert.equal(selection.toString(), selected, "native selection survives settlement");
  assert.equal(host.querySelector("a")?.getAttribute("href"), "https://example.com", "final parse resolves document references");
  const final = calls[2].text;
  await act(async () => root.unmount()); root = createRoot(host);
  await render(final);
  assert.equal(calls.length, 3, "cache avoids parsing unchanged history");
  assert.ok(host.querySelector("strong"));
  await render("obsolete source"); const old = calls.length - 1;
  await render("replacement source"); const current = calls.length - 1;
  await respond(current); await respond(old);
  assert.equal(host.textContent, "replacement source", "late worker result cannot replace the current revision");
  const large = Array.from({ length: 420 }, (_, index) => `Paragraph ${index}.`).join("\n\n");
  await render(large); await respond(calls.length - 1);
  assert.equal(host.querySelectorAll("p").length, 420, "all loaded Markdown blocks remain mounted");
  assert.ok(host.textContent?.includes("Paragraph 0.") && host.textContent.includes("Paragraph 419."));
  assert.equal(host.querySelector("[data-markdown-window-start]"), null);
  console.log("markdown natural flow: worker races, stable DOM/selection, final references, cache and complete blocks passed");
} finally {
  await act(async () => root.unmount()); worker.disposeMarkdownWorkerClient();
  delete (globalThis as { Worker?: unknown }).Worker;
  await server.close(); dom.window.close();
}
