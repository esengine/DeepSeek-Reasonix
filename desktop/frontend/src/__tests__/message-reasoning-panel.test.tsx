// Run: tsx src/__tests__/message-reasoning-panel.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { LocaleProvider } from "../lib/i18n";
import { AssistantMessage } from "../components/Message";

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

console.log("\nmessage reasoning panel");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: { ...dom.window.navigator, language: "en-US" } });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(
    <LocaleProvider>
      <AssistantMessage
        item={{
          kind: "assistant",
          id: "a1",
          text: "",
          reasoning: "line one\nline two",
          streaming: false,
          reasoningComplete: true,
          reasoningDurationMs: 2_600,
        }}
      />
    </LocaleProvider>,
  );
});

const header = document.querySelector<HTMLButtonElement>(".reasoning__head");
ok(Boolean(header), "completed reasoning renders a toggle header");
ok(header?.textContent?.includes("thinking") ?? false, "header keeps the reasoning label");
ok(header?.textContent?.includes("lasted 3s") ?? false, "header shows rounded reasoning duration");
ok(!document.querySelector(".reasoning__body"), "completed reasoning is collapsed by default");

await act(async () => {
  header?.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
});

ok(document.querySelector(".reasoning__body")?.textContent?.includes("line two") ?? false, "clicking the header expands the reasoning body");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
