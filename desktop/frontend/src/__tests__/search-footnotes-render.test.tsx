// Run: tsx src/__tests__/search-footnotes-render.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { LocaleProvider } from "../lib/i18n";
import { AssistantMessage } from "../components/Message";
import { hydrateReasoningDisplayMode } from "../lib/reasoningDisplayPreference";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nsearch sources panel render");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: { ...dom.window.navigator, language: "en-US" } });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);
hydrateReasoningDisplayMode("hidden", true);

await act(async () => {
  root.render(
    <LocaleProvider>
      <AssistantMessage
        item={{
          kind: "assistant",
          id: "a1",
          text: "answer only",
          reasoning: "",
          streaming: false,
          searchSources: [{ title: "新闻本文", url: "https://example.com/a?utm_source=test#section" }],
        }}
      />
    </LocaleProvider>,
  );
});

const body = document.querySelector(".msg__body");
const panel = document.querySelector(".msg-search-sources");
const toggle = panel?.querySelector<HTMLButtonElement>(".msg-search-sources__toggle");
ok(Boolean(body?.textContent?.includes("answer only")), "answer body keeps the model text");
ok(Boolean(panel), "sources panel renders under the answer");
ok(toggle?.getAttribute("aria-expanded") === "false", "sources panel is collapsed by default");
ok(!panel?.querySelector(".msg-search-sources__body"), "collapsed panel does not expose source details");
await act(async () => {
  toggle?.click();
});
const title = panel?.querySelector(".msg-search-source__title");
const link = panel?.querySelector<HTMLAnchorElement>(".msg-search-source__link");
ok(toggle?.getAttribute("aria-expanded") === "true", "click expands the sources panel");
ok(title?.textContent === "新闻本文", "expanded panel shows the result title");
ok(link?.getAttribute("href") === "https://example.com/a", "source link uses the cleaned URL");
ok(Boolean(panel?.querySelector(".msg-search-source__url")?.textContent?.includes("example.com/a")), "expanded panel shows the hostname and short URL");
ok(!(document.querySelector(".msg__body > .md")?.textContent ?? "").includes("新闻本文"), "answer markdown does not include the search title");

if (failed) {
  process.stdout.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
