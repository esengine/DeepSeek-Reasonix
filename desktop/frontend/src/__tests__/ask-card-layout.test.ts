// Run: tsx src/__tests__/ask-card-layout.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { AskCard } from "../components/AskCard";
import { LocaleProvider } from "../lib/i18n";
import type { QuestionAnswer, WireAsk } from "../lib/types";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8");

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

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
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
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

  const style = document.createElement("style");
  style.textContent = styles;
  document.head.appendChild(style);
  return dom;
}

console.log("\nask card layout");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const answers: QuestionAnswer[][] = [];
  const ask: WireAsk = {
    id: "ask-superpowers-decision",
    questions: [
      {
        id: "decision",
        header: "Review",
        prompt: "baoguanPutArchive needs a user-owned decision: fully align archive logic, or only repair the current compiler error?",
        options: [
          { label: "Full alignment", description: "Reuse the archive flow and keep behavior consistent." },
          { label: "Minimal repair", description: "Touch only the failing path and keep the patch smaller." },
        ],
      },
    ],
  };

  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null,
        React.createElement(AskCard, {
          ask,
          onAnswer: (_id: string, next: QuestionAnswer[]) => answers.push(next),
          onDismiss: () => undefined,
          onStop: () => undefined,
        }),
      ),
    );
    await flushTimers();
  });

  const card = document.querySelector(".prompt-shelf__card") as HTMLElement | null;
  const meta = document.querySelector(".prompt-shelf__meta") as HTMLElement | null;
  if (!card || !meta) throw new Error("ask prompt shelf did not render");

  eq(meta.textContent, ask.questions[0].prompt, "ask question text remains complete in the prompt shelf");

  const computed = window.getComputedStyle(meta);
  eq(computed.whiteSpace, "normal", "ask question can wrap instead of staying on one line");
  eq(computed.overflow, "visible", "ask question is not clipped by the prompt shelf");
  eq(computed.textOverflow, "clip", "ask question does not render as an ellipsis-only preview");
  eq(computed.overflowWrap, "anywhere", "long unspaced ask questions can break within the shelf");
  ok(card.getAttribute("role") === "dialog", "ask prompt shelf keeps dialog semantics");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
