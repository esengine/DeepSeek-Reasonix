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

function flushTimers(delay = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, delay));
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
  Object.defineProperty(dom.window.HTMLElement.prototype, "clientHeight", {
    configurable: true,
    get() {
      return this.classList.contains("prompt-action__desc") ? 42 : 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollHeight", {
    configurable: true,
    get() {
      if (!this.classList.contains("prompt-action__desc")) return 0;
      return this.textContent?.includes("Reuse the archive flow") ? 84 : 42;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    value() {
      this.setAttribute("data-scrolled-into-view", "true");
    },
  });

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
          {
            label: "Full alignment",
            description: "Reuse the archive flow and keep behavior consistent across every constructor, dynamically computed path, runtime boundary, and release validation step.",
          },
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
  const content = document.querySelector(".prompt-shelf__content") as HTMLElement | null;
  const meta = document.querySelector(".prompt-shelf__meta") as HTMLElement | null;
  const footer = document.querySelector(".prompt-shelf__footer") as HTMLElement | null;
  if (!card || !content || !meta || !footer) throw new Error("ask prompt shelf did not render");

  eq(meta.textContent, ask.questions[0].prompt, "ask question text remains complete in the prompt shelf");

  const computed = window.getComputedStyle(meta);
  eq(computed.whiteSpace, "normal", "ask question can wrap instead of staying on one line");
  eq(computed.overflow, "visible", "ask question is not clipped by the prompt shelf");
  eq(computed.textOverflow, "clip", "ask question does not render as an ellipsis-only preview");
  eq(computed.overflowWrap, "anywhere", "long unspaced ask questions can break within the shelf");
  ok(card.getAttribute("role") === "dialog", "ask prompt shelf keeps dialog semantics");
  ok(document.querySelector(".prompt-shelf--decision") != null, "ask uses the unified decision surface layout");
  eq(window.getComputedStyle(card).maxHeight, "min(82vh, 720px)", "Ask card stays bounded by the viewport");
  eq(window.getComputedStyle(card).overflow, "hidden", "Ask card delegates overflow to one content scroller");
  eq(window.getComputedStyle(content).overflow, "auto", "Ask title, question, and options share one scroll region");
  eq(content.contains(footer), false, "Ask confirmation footer stays outside the scrolling content");

  const optionButtons = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLElement[];
  // options + custom + skip
  eq(optionButtons.length, 4, "ask renders options plus custom and skip rows");
  ok(
    optionButtons[0]?.textContent?.includes("Reuse the archive flow") === true,
    "option descriptions render inline on each decision row",
  );
  const actions = document.querySelector(".prompt-shelf__actions") as HTMLElement | null;
  const firstOption = optionButtons[0];
  const firstDescription = firstOption?.querySelector(".prompt-action__desc") as HTMLElement | null;
  if (!actions || !firstOption || !firstDescription) throw new Error("ask option layout did not render");

  const actionsStyle = window.getComputedStyle(actions);
  eq(actionsStyle.gridAutoRows, "max-content", "decision rows use their full content height before scrolling");
  eq(actionsStyle.alignContent, "start", "decision rows stay content-sized at the top of the scroll region");
  eq(actionsStyle.maxHeight, "none", "Ask options do not create a nested scroll region");
  eq(actionsStyle.overflow, "visible", "Ask option overflow belongs to the shared content scroller");

  const optionStyle = window.getComputedStyle(firstOption);
  eq(optionStyle.height, "auto", "long decision rows are not fixed-height");
  eq(optionStyle.minHeight, "40px", "short decision rows retain the compact minimum height");
  eq(optionStyle.alignItems, "flex-start", "multi-line decision copy aligns with the option key");

  const descriptionStyle = window.getComputedStyle(firstDescription);
  eq(descriptionStyle.whiteSpace, "normal", "long option descriptions can wrap");
  eq(descriptionStyle.display, "-webkit-box", "long Ask descriptions use a multi-line clamp box");
  eq(descriptionStyle.overflow, "hidden", "collapsed Ask descriptions stay within three lines");
  eq(descriptionStyle.getPropertyValue("-webkit-line-clamp"), "3", "collapsed Ask descriptions show three summary lines");
  eq(descriptionStyle.textOverflow, "clip", "wrapped descriptions do not use a single-line ellipsis");
  eq(descriptionStyle.overflowWrap, "anywhere", "unspaced option descriptions can wrap without overflowing");
  eq(firstOption.getAttribute("title"), ask.questions[0].options[0].description, "collapsed descriptions keep the full native tooltip");

  const descriptionToggle = document.querySelector(".prompt-action__description-toggle") as HTMLButtonElement | null;
  if (!descriptionToggle) throw new Error("long Ask description disclosure did not render");
  eq(descriptionToggle.textContent?.trim(), "View full description", "truncated descriptions expose an explicit full-text action");
  eq(descriptionToggle.getAttribute("aria-expanded"), "false", "full description starts collapsed");
  eq(descriptionToggle.getAttribute("aria-controls"), firstDescription.id, "disclosure identifies the controlled description");

  let disclosureEnterDefaultPrevented = true;
  await act(async () => {
    const event = new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    descriptionToggle.dispatchEvent(event);
    disclosureEnterDefaultPrevented = event.defaultPrevented;
    await flushTimers();
  });
  eq(answers.length, 0, "Enter on Ask disclosure never confirms the selected answer");
  eq(disclosureEnterDefaultPrevented, true, "Ask disclosure owns keyboard activation before global shortcuts");
  eq(descriptionToggle.getAttribute("aria-expanded"), "true", "Enter expands the Ask description");

  await act(async () => {
    descriptionToggle.dispatchEvent(new window.KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    }));
    await flushTimers();
  });
  eq(descriptionToggle.getAttribute("aria-expanded"), "false", "Enter collapses the Ask description again");

  await act(async () => {
    descriptionToggle.click();
    await flushTimers();
  });
  eq(firstOption.classList.contains("prompt-action--description-expanded"), true, "full-text action expands the selected card");
  eq(descriptionToggle.getAttribute("aria-expanded"), "true", "expanded state is announced");
  const expandedDescriptionStyle = window.getComputedStyle(firstDescription);
  eq(expandedDescriptionStyle.display, "block", "expanded description restores normal block flow");
  eq(expandedDescriptionStyle.overflow, "visible", "expanded description reveals the complete text");

  await act(async () => {
    descriptionToggle.click();
    await flushTimers();
  });
  eq(firstOption.classList.contains("prompt-action--description-expanded"), false, "full description can be collapsed again");
  eq(descriptionToggle.getAttribute("aria-expanded"), "false", "collapsed state is announced");

  await act(async () => {
    optionButtons[1].click();
    await flushTimers(200);
  });
  eq(document.querySelector(".prompt-action__description-toggle"), null, "short selected descriptions do not show a redundant disclosure");
  eq(answers.length, 0, "single-select click only selects and does not auto-advance/submit");

  await act(async () => {
    (document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement).click();
    await flushTimers();
  });
  eq(answers.length, 1, "confirm submits the selected single-select answer");
  eq(answers[0]?.[0]?.selected?.[0], "Minimal repair", "submitted answer matches the selected option");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Multi-select requires at least one choice before confirm advances.
{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const answers: QuestionAnswer[][] = [];
  const ask: WireAsk = {
    id: "ask-multi",
    questions: [
      {
        id: "picks",
        prompt: "Pick at least one",
        multi: true,
        options: [
          { label: "A", description: "Option A" },
          { label: "B", description: "Option B" },
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

  const confirm = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement;
  eq(confirm.disabled, true, "multi-select confirm stays disabled until an option is chosen");

  const optionButtons = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLElement[];
  await act(async () => {
    optionButtons[0].click();
    await flushTimers();
  });
  eq(confirm.disabled, false, "multi-select confirm enables after selecting one option");
  eq(answers.length, 0, "multi-select click does not submit");

  await act(async () => {
    confirm.click();
    await flushTimers();
  });
  eq(answers.length, 1, "multi-select confirm submits once");
  eq(JSON.stringify(answers[0]?.[0]?.selected), JSON.stringify(["A"]), "multi-select keeps chosen labels");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Single-select: keyboard cursor is confirmable without a prior click.
{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const answers: QuestionAnswer[][] = [];
  const ask: WireAsk = {
    id: "ask-keyboard-single",
    questions: [
      {
        id: "choice",
        prompt: "Pick one with the keyboard",
        options: [
          { label: "First", description: "Option one" },
          { label: "Second", description: "Option two" },
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

  const optionButtons = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLElement[];
  const confirm = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement;
  eq(optionButtons[0]?.getAttribute("aria-selected"), "true", "initial keyboard cursor marks the first option");
  eq(confirm.disabled, false, "initial option cursor enables confirm without a click");

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(optionButtons[0]?.getAttribute("aria-selected"), "false", "ArrowDown moves the single-select cursor off the first row");
  eq(optionButtons[1]?.getAttribute("aria-selected"), "true", "ArrowDown selects the second option visually");
  eq(optionButtons[1]?.getAttribute("data-scrolled-into-view"), "true", "keyboard selection stays inside the visible option viewport");
  eq(confirm.disabled, false, "ArrowDown keeps confirm enabled for the highlighted option");

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(answers.length, 1, "ArrowDown+Enter submits the highlighted single-select option");
  eq(answers[0]?.[0]?.selected?.[0], "Second", "submitted answer matches the keyboard cursor");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Single-select: initial Enter confirms the default-highlighted first option.
{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const answers: QuestionAnswer[][] = [];
  const ask: WireAsk = {
    id: "ask-keyboard-initial-enter",
    questions: [
      {
        id: "choice",
        prompt: "Confirm the first option with Enter",
        options: [
          { label: "Alpha", description: "A" },
          { label: "Beta", description: "B" },
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

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(answers.length, 1, "initial Enter submits without a prior click");
  eq(answers[0]?.[0]?.selected?.[0], "Alpha", "initial Enter uses the first highlighted option");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Multi-select: keyboard cursor must not look like a checked answer.
{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const ask: WireAsk = {
    id: "ask-keyboard-multi",
    questions: [
      {
        id: "picks",
        prompt: "Cursor is not a check",
        multi: true,
        options: [
          { label: "A", description: "Option A" },
          { label: "B", description: "Option B" },
        ],
      },
    ],
  };

  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null,
        React.createElement(AskCard, {
          ask,
          onAnswer: () => undefined,
          onDismiss: () => undefined,
          onStop: () => undefined,
        }),
      ),
    );
    await flushTimers();
  });

  const optionButtons = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLElement[];
  const confirm = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement;
  eq(optionButtons[0]?.getAttribute("aria-selected"), "false", "multi-select cursor alone is not aria-selected");
  eq(optionButtons[0]?.getAttribute("data-active"), "true", "multi-select marks the keyboard cursor with data-active");
  eq(confirm.disabled, true, "multi-select confirm stays disabled until an option is checked");

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(optionButtons[0]?.getAttribute("data-active"), null, "ArrowDown clears the previous multi-select cursor");
  eq(optionButtons[1]?.getAttribute("data-active"), "true", "ArrowDown moves the multi-select cursor");
  eq(optionButtons[1]?.getAttribute("aria-selected"), "false", "ArrowDown does not check the multi-select option");
  eq(confirm.disabled, true, "ArrowDown alone does not enable multi-select confirm");

  await act(async () => {
    optionButtons[1].click();
    await flushTimers();
  });
  eq(optionButtons[1]?.getAttribute("aria-selected"), "true", "click checks the multi-select option");
  eq(confirm.disabled, false, "multi-select confirm enables after a real check");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
