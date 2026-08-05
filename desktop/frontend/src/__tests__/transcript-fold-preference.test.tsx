// Run: tsx src/__tests__/transcript-fold-preference.test.tsx
//
// Pins that switching the work-process-fold preference applies to folds
// already on screen through the live event bus — not only to folds mounted
// afterwards.

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";

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

console.log("\ntranscript fold preference sync");

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window);
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(dom.window, "matchMedia", {
  configurable: true,
  value: () => ({
    matches: true,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  }),
});
const storage = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => void storage.set(key, value),
    removeItem: (key: string) => void storage.delete(key),
    clear: () => storage.clear(),
    key: () => null,
    length: 0,
  },
});

const { createServer } = await import("vite");
// GSAP's CSS plugin cannot run against jsdom; the assertions here are about
// state-driven class names, so the animation hooks are stubbed out.
const server = await createServer({
  appType: "custom",
  logLevel: "silent",
  server: { middlewareMode: true },
  plugins: [
    {
      name: "stub-animation-hooks",
      enforce: "pre",
      load(id) {
        if (id.endsWith("/src/lib/useGSAPCollapse.ts")) {
          return "export function useGSAPCollapse() {}";
        }
        if (id.endsWith("/src/lib/useEntranceAnimation.ts")) {
          return "export function useEntranceAnimation() { return { current: null }; }";
        }
        return undefined;
      },
    },
  ],
});
const { Transcript } = await server.ssrLoadModule("/src/components/Transcript.tsx");
const { LocaleProvider } = await server.ssrLoadModule("/src/lib/i18n.tsx");
const { setProcessFoldPreference } = await server.ssrLoadModule("/src/lib/processFoldPreference.ts");

const items = [
  { kind: "user", id: "u1", text: "ask" },
  { kind: "assistant", id: "a1", text: "answered", reasoning: "quick thought", streaming: false, workDurationMs: 3_000 },
  {
    kind: "notice",
    id: "decision-1",
    level: "info",
    text: "Decision recorded: answered",
    decisionReceipt: {
      id: "ask-1",
      kind: "ask",
      subject: "Choose a model: DeepSeek V4",
      outcome: "answered",
    },
  },
];
const streamingItems = [
  { kind: "user", id: "u1", text: "stream" },
  { kind: "assistant", id: "a1", text: "partial answer", reasoning: "working", streaming: true, reasoningComplete: false },
];
const processOnlyItems = [
  { kind: "user", id: "u-only", text: "cancelled" },
  { kind: "assistant", id: "a-only", text: "", reasoning: "got cut off", streaming: false, reasoningComplete: true },
];
const processOnlyStreamingItems = processOnlyItems.map((item) => item.id === "a-only"
  ? { ...item, streaming: true, reasoningComplete: false }
  : item);

const container = dom.window.document.getElementById("root")!;
const root = createRoot(container);
try {
  await act(async () => {
    root.render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(Transcript, { items: streamingItems, onPrompt: () => {}, questionNavigator: false, running: true }),
      ),
    );
  });
  ok(container.querySelector(".turn-collapse--open"), "auto preference opens a fold while the turn is streaming");

  await act(async () => {
    setProcessFoldPreference("collapsed");
  });
  ok(!container.querySelector(".turn-collapse--open"), "live collapsed preference closes an active fold");

  await act(async () => {
    setProcessFoldPreference("expanded");
  });
  ok(container.querySelector(".turn-collapse--open"), "live expanded preference opens an active fold");

  await act(async () => {
    setProcessFoldPreference("auto");
  });
  ok(container.querySelector(".turn-collapse--open"), "auto preference keeps an active fold open");

  await act(async () => {
    root.render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(Transcript, { items, onPrompt: () => {}, questionNavigator: false, running: false }),
      ),
    );
  });
  ok(container.querySelector(".turn-collapse"), "completed turn renders its work fold");
  ok(!container.querySelector(".turn-collapse--open"), "auto preference collapses the completed fold with an answer outside");
  ok(container.textContent?.includes("Question answered"), "Ask receipt keeps its completed title");
  ok(!container.querySelector(".notice-line__decision-outcome"), "Ask receipt does not repeat the answered outcome");

  await act(async () => {
    setProcessFoldPreference("expanded");
  });
  ok(container.querySelector(".turn-collapse--open"), "live expanded preference reopens the completed fold");

  await act(async () => {
    setProcessFoldPreference("auto");
  });
  ok(!container.querySelector(".turn-collapse--open"), "live auto preference collapses the completed answered fold");

  await act(async () => {
    setProcessFoldPreference("collapsed");
    root.render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(Transcript, { items: processOnlyStreamingItems, onPrompt: () => {}, questionNavigator: false, running: true }),
      ),
    );
  });
  ok(!container.querySelector(".turn-collapse--open"), "collapsed preference keeps a process-only streaming fold closed");

  await act(async () => {
    root.render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(Transcript, { items: processOnlyItems, onPrompt: () => {}, questionNavigator: false, running: false }),
      ),
    );
  });
  ok(container.querySelector(".turn-collapse--open"), "completed process-only fold reopens so the response is not apparently empty");

  const processOnlyHead = container.querySelector<HTMLButtonElement>(".turn-collapse > .reasoning__head");
  await act(async () => {
    processOnlyHead?.click();
  });
  ok(container.querySelector(".turn-collapse--open"), "content-only completed fold cannot be manually hidden");

  await act(async () => {
    setProcessFoldPreference("expanded");
  });
  ok(container.querySelector(".turn-collapse--open"), "switching to keep-expanded opens folds already on screen");

  await act(async () => {
    setProcessFoldPreference("auto");
  });
  ok(container.querySelector(".turn-collapse--open"), "auto preference still keeps a content-only completed fold visible");
} finally {
  await act(async () => {
    root.unmount();
  });
  await server.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
