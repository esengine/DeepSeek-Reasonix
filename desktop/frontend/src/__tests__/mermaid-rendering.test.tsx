// Run: tsx src/__tests__/mermaid-rendering.test.tsx

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import MermaidDiagram from "../components/MermaidDiagram";
import {
  __setMermaidPanZoomFactoryForTest,
  __setMermaidRenderAdapterForTest,
  isOpenableMermaidHref,
  isSafeMermaidHref,
  safelyRunPanZoom,
  safelySyncPanZoom,
  sanitizeMermaidSvg,
} from "../components/MermaidDiagram";
import { LocaleProvider } from "../lib/i18n";
import { REMOTE_MARKDOWN_IMAGE_PATH } from "../lib/markdownImage";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const markdownRendererSource = readFileSync(resolve(testDir, "../components/MarkdownRenderer.tsx"), "utf8");

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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let i = 0; i < 30; i += 1) {
    if (predicate()) return;
    await act(async () => {
      await flushTimers();
    });
  }
  ok(false, label);
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><head></head><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });

  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.SVGElement = dom.window.SVGElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.DOMParser = dom.window.DOMParser;
  globalThis.XMLSerializer = dom.window.XMLSerializer;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", {
    configurable: true,
    value: () => ({ x: 0, y: 0, width: 640, height: 360, top: 0, right: 640, bottom: 360, left: 0, toJSON: () => ({}) }),
  });
  Object.defineProperty(dom.window, "matchMedia", {
    configurable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    }),
  });
  globalThis.matchMedia = dom.window.matchMedia;

  const style = document.createElement("style");
  style.textContent = styles;
  document.head.appendChild(style);

  return dom;
}

{
  const dom = installDom();
  const container = document.createElement("div");
  const throwing = {
    destroy: () => {},
    resize: () => { throw new DOMException("matrix is not invertible", "InvalidStateError"); },
    fit: () => { throw new DOMException("matrix is not invertible", "InvalidStateError"); },
    center: () => {},
    zoomIn: () => { throw new DOMException("matrix is not invertible", "InvalidStateError"); },
    zoomOut: () => {},
    reset: () => {},
  };
  eq(safelySyncPanZoom(throwing, container), false, "matrix failures are contained during pan/zoom layout sync");
  eq(safelyRunPanZoom(throwing, () => throwing.zoomIn()), false, "matrix failures are contained during toolbar actions");
  Object.defineProperty(container, "getBoundingClientRect", {
    value: () => ({ x: 0, y: 0, width: 0, height: 0, top: 0, right: 0, bottom: 0, left: 0, toJSON: () => ({}) }),
  });
  eq(safelySyncPanZoom(throwing, container), false, "zero-sized layouts are deferred before SVG matrix work");
  dom.window.close();
}

function parseSvg(svg: string): Document {
  return new DOMParser().parseFromString(svg, "image/svg+xml");
}

console.log("\nmermaid rendering");

{
  ok(
    markdownRendererSource.includes('lazy(() => import("./MermaidDiagram"))'),
    "MarkdownRenderer lazy-loads the Mermaid renderer",
  );
  ok(
    markdownRendererSource.includes('lang === "mermaid"'),
    "MarkdownRenderer routes mermaid fenced code blocks to the Mermaid renderer",
  );
}

{
  const dom = installDom();
  Object.defineProperty(dom.window, "runtime", { configurable: true, value: {} });
  const dirtySvg = `
    <svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" onload="steal()">
      <script>alert(1)</script>
      <a id="safe" href="https://example.com/diagram"><text>safe</text></a>
      <a id="unsafe" href="javascript:alert(1)"><text>bad</text></a>
      <a id="unsafe-xlink" xlink:href="data:text/html,boom"><text>bad</text></a>
      <image id="remote-image" href="https://images.example.com/diagram.png" />
      <use id="external-use" href="https://images.example.com/icons.svg#node" />
      <g onclick="steal()"><text>node</text></g>
    </svg>`;
  const sanitized = sanitizeMermaidSvg(dirtySvg);
  const doc = parseSvg(sanitized);

  ok(!doc.documentElement.hasAttribute("onload"), "sanitizer strips event attributes from the root SVG");
  ok(!doc.querySelector("script"), "sanitizer removes script nodes");
  ok(doc.querySelector("#safe")?.getAttribute("href") === "https://example.com/diagram", "sanitizer keeps safe external links");
  ok(!doc.querySelector("#unsafe")?.hasAttribute("href"), "sanitizer removes javascript links");
  ok(!doc.querySelector("#unsafe-xlink")?.hasAttribute("xlink:href"), "sanitizer removes data xlink links");
  ok(
    doc.querySelector("#remote-image")?.getAttribute("href")?.startsWith(`${REMOTE_MARKDOWN_IMAGE_PATH}?url=`) === true,
    "sanitizer routes Mermaid image resources through the backend proxy",
  );
  ok(!doc.querySelector("#external-use")?.hasAttribute("href"), "sanitizer removes external SVG use resources");
  ok(!doc.querySelector("g")?.hasAttribute("onclick"), "sanitizer strips event attributes from child nodes");
  ok(isSafeMermaidHref("https://example.com/a"), "https Mermaid links are safe");
  ok(isSafeMermaidHref("mailto:hello@example.com"), "mailto Mermaid links are safe");
  ok(!isSafeMermaidHref("file:///tmp/private"), "file Mermaid links are not safe");
  ok(!isOpenableMermaidHref("#internal"), "fragment Mermaid links are not opened externally");

  dom.window.close();
}

{
  const dom = installDom();
  const openedUrls: string[] = [];
  dom.window.open = ((url: string | URL | undefined) => {
    if (url) openedUrls.push(String(url));
    return null;
  }) as Window["open"];

  const renders: Array<{ definition: string; theme: string }> = [];
  const panZoomCalls: string[] = [];

  __setMermaidRenderAdapterForTest(async (_svgId, definition, theme, signal) => {
    if (signal.aborted) throw new DOMException("Aborted", "AbortError");
    renders.push({ definition, theme });
    return `
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 80" onload="steal()">
        <script>alert(1)</script>
        <a id="safe-link" href="https://example.com/diagram"><text>Open</text></a>
        <a id="unsafe-link" href="vbscript:msgbox(1)"><text>Blocked</text></a>
        <g class="node"><text>Rendered Mermaid</text></g>
      </svg>`;
  });

  __setMermaidPanZoomFactoryForTest(() => ({
    destroy: () => { panZoomCalls.push("destroy"); },
    resize: () => { panZoomCalls.push("resize"); },
    fit: () => { panZoomCalls.push("fit"); },
    center: () => { panZoomCalls.push("center"); },
    zoomIn: () => { panZoomCalls.push("zoomIn"); },
    zoomOut: () => { panZoomCalls.push("zoomOut"); },
    reset: () => { panZoomCalls.push("reset"); },
  }));

  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);

  await act(async () => {
    root.render(
      <LocaleProvider>
        <div className="chat-pane">
          <MermaidDiagram definition={"graph TD\nA-->B"} />
        </div>
      </LocaleProvider>,
    );
    await flushTimers();
  });

  await waitFor("Mermaid preview SVG rendered in DOM", () => Boolean(document.querySelector(".mermaid-diagram__preview svg")));
  ok(document.querySelector(".mermaid-diagram__toolbar"), "Mermaid renderer shows its toolbar");
  eq(renders.length, 1, "Mermaid renderer calls the render adapter once");
  eq(renders[0]?.definition, "graph TD\nA-->B", "Mermaid renderer passes the diagram definition to Mermaid");
  ok(document.querySelector("#safe-link"), "safe SVG link remains in the rendered DOM");
  ok(!document.querySelector("#unsafe-link")?.hasAttribute("href"), "unsafe SVG link href is stripped in the rendered DOM");
  ok(!document.querySelector(".mermaid-diagram__preview svg")?.hasAttribute("onload"), "rendered SVG root event handler is stripped");
  ok(!document.querySelector(".mermaid-diagram__preview script"), "rendered SVG script nodes are removed");

  await waitFor("pan zoom instance initialized", () => panZoomCalls.includes("fit") && panZoomCalls.includes("center"));

  const zoomIn = document.querySelector<HTMLButtonElement>('button[aria-label="Zoom in"]');
  const zoomOut = document.querySelector<HTMLButtonElement>('button[aria-label="Zoom out"]');
  const reset = document.querySelector<HTMLButtonElement>('button[aria-label="Reset zoom"]');
  zoomIn?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  zoomOut?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  reset?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  ok(panZoomCalls.includes("zoomIn"), "zoom in button calls the pan zoom instance");
  ok(panZoomCalls.includes("zoomOut"), "zoom out button calls the pan zoom instance");
  ok(panZoomCalls.includes("reset"), "reset zoom button calls the pan zoom instance");

  document.querySelector("#safe-link text")?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  eq(openedUrls[0], "https://example.com/diagram", "SVG links open through the external browser bridge");
  document.querySelector("#unsafe-link text")?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  eq(openedUrls.length, 1, "unsafe SVG links do not open externally");

  await act(async () => {
    document.querySelector<HTMLButtonElement>('button[aria-label="Show diagram source"]')
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await flushTimers();
  });
  ok(document.querySelector(".mermaid-diagram__code")?.textContent?.includes("graph TD"), "source tab shows the Mermaid definition");

  await act(async () => {
    document.querySelector<HTMLButtonElement>('button[aria-label="Open fullscreen"]')
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await flushTimers();
  });
  ok(document.querySelector(".chat-pane > .mermaid-diagram--fullscreen"), "fullscreen diagram portals into the chat pane");

  await act(async () => {
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushTimers();
  });
  ok(!document.querySelector(".chat-pane > .mermaid-diagram--fullscreen"), "Escape closes the Mermaid fullscreen portal");

  await act(async () => {
    root.unmount();
  });
  __setMermaidRenderAdapterForTest(null);
  __setMermaidPanZoomFactoryForTest(undefined);
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
