// Run: tsx src/__tests__/tool-card-error-details.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ToolCard } from "../components/ToolCard";
import { LocaleProvider } from "../lib/i18n";
import type { Item } from "../lib/useController";

type ToolItem = Extract<Item, { kind: "tool" }>;

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

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
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
  globalThis.Event = dom.window.Event;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  dom.window.matchMedia = () => ({
    matches: true,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  });
  return dom;
}

console.log("\ntool card error details");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const error = "evidence 1: verification command \"Select-String -Path @(...long...)\" has no matching successful receipt — cite the command exactly as it ran in the session; commands that ran: [\"unique-command-tail\"] — pick the matching one and retry complete_step";
  const item: ToolItem = {
    kind: "tool",
    id: "complete-step-error",
    name: "complete_step",
    args: "",
    readOnly: true,
    status: "error",
    output: `error: ${error}`,
    error,
    durationMs: 62,
  };

  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null,
        React.createElement(ToolCard, { item }),
      ),
    );
    await flushTimers();
  });

  eq(document.querySelectorAll(".code-block").length, 0, "duplicate error output is not rendered as a code block");
  ok(document.querySelector(".tool__err-summary")?.textContent?.includes("verification command has no matching successful receipt"), "compact error summary is visible");
  ok(!document.querySelector(".tool__err-details"), "full error details are hidden by default");
  ok(!document.body.textContent?.includes("unique-command-tail"), "long receipt list is not visible before expansion");

  const toggle = document.querySelector(".tool__err-toggle") as HTMLButtonElement | null;
  if (!toggle) throw new Error("error details toggle did not render");
  await act(async () => {
    toggle.click();
    await flushTimers();
  });

  ok(document.querySelector(".tool__err-details")?.textContent?.includes("unique-command-tail"), "full error details render after expansion");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
