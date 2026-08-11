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
      if (this.classList.contains("prompt-action__desc")) return 42;
      if (this.classList.contains("prompt-action__label")) return 20;
      return 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollHeight", {
    configurable: true,
    get() {
      if (this.classList.contains("prompt-action__desc")) {
        return this.textContent?.includes("Reuse the archive flow") ? 84 : 42;
      }
      if (this.classList.contains("prompt-action__label")) return 20;
      return 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "clientWidth", {
    configurable: true,
    get() {
      if (this.classList.contains("prompt-action__desc")) return 160;
      if (this.classList.contains("prompt-action__label")) return 160;
      return 0;
    },
  });
  Object.defineProperty(dom.window.HTMLElement.prototype, "scrollWidth", {
    configurable: true,
    get() {
      if (this.classList.contains("prompt-action__desc")) {
        return this.textContent?.includes("Reuse the archive flow") ? 320 : 120;
      }
      if (this.classList.contains("prompt-action__label")) {
        return this.textContent?.includes("Keep every historical migration") ? 480 : 120;
      }
      return 0;
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
  eq(window.getComputedStyle(card).maxHeight, "min(62vh, 560px)", "Ask card stays bounded by the viewport");
  eq(window.getComputedStyle(card).overflow, "hidden", "Ask card delegates overflow to one content scroller");
  eq(window.getComputedStyle(content).overflow, "auto", "Ask title, question, and options share one scroll region");
  eq(content.contains(footer), false, "Ask confirmation footer stays outside the scrolling content");
  const secondary = footer.querySelector(".decision-confirm-bar__secondary") as HTMLButtonElement | null;
  ok(Boolean(secondary?.textContent?.trim()), "Ask skip is a quiet footer action");

  const optionButtons = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLElement[];
  // options + custom; skip is a secondary footer action
  eq(optionButtons.length, 3, "ask renders options plus custom without a skip row");
  ok(
    optionButtons[0]?.textContent?.includes("Reuse the archive flow") === true,
    "option descriptions render inline on each decision row",
  );
  const actions = document.querySelector(".prompt-shelf__actions") as HTMLElement | null;
  const firstOption = optionButtons[0];
  const firstDescription = firstOption?.querySelector(".prompt-action__desc") as HTMLElement | null;
  if (!actions || !firstOption || !firstDescription) throw new Error("ask option layout did not render");

  const actionsStyle = window.getComputedStyle(actions);
  eq(actionsStyle.gridAutoRows, "max-content", "decision row wrappers accommodate optional external details");
  eq(actionsStyle.alignContent, "start", "decision rows stay content-sized at the top of the scroll region");
  eq(actionsStyle.maxHeight, "none", "Ask options do not create a nested scroll region");
  eq(actionsStyle.overflow, "visible", "Ask option overflow belongs to the shared content scroller");

  const optionStyle = window.getComputedStyle(firstOption);
  eq(optionStyle.height, "38px", "desktop decision rows keep a stable compact height");
  eq(optionStyle.minHeight, "38px", "short decision rows retain a compact click target");
  eq(optionStyle.alignItems, "center", "single-line decision copy stays vertically centered with the option key");
  eq(window.getComputedStyle(firstOption.querySelector(".prompt-action__key") as HTMLElement).marginTop, "0px", "decision keys do not carry a top offset");

  ok(
    /\.prompt-shelf--decision \.prompt-shelf__actions \.prompt-action__copy \{[^}]*grid-template-columns:\s*fit-content\(44%\) minmax\(0, 1fr\)/s.test(styles),
    "decision option labels size to content while staying capped at 44% of the row",
  );
  ok(
    !/\.prompt-shelf--decision \.prompt-shelf__actions \.prompt-action__label \{[^}]*max-width:\s*[\d.]/s.test(styles),
    "decision option labels never resolve their width cap against their own content-sized track",
  );

  const descriptionStyle = window.getComputedStyle(firstDescription);
  eq(descriptionStyle.whiteSpace, "nowrap", "long option descriptions stay on one stable summary line");
  eq(descriptionStyle.display, "block", "Ask summaries use ordinary single-line flow");
  eq(descriptionStyle.overflow, "hidden", "collapsed Ask summaries stay inside their row");
  eq(descriptionStyle.getPropertyValue("-webkit-line-clamp"), "", "selection never changes the summary to two lines");
  eq(descriptionStyle.textOverflow, "ellipsis", "long summaries end with a clear ellipsis");
  eq(descriptionStyle.overflowWrap, "normal", "unspaced summaries stay clipped inside the stable row");
  eq(
    firstOption.getAttribute("title"),
    ask.questions[0].options[0].description,
    "normal short labels preserve the existing description tooltip",
  );

  const descriptionToggle = document.querySelector(".prompt-action__description-toggle") as HTMLButtonElement | null;
  if (!descriptionToggle) throw new Error("long Ask description disclosure did not render");
  eq(descriptionToggle.textContent?.trim(), "View full description", "truncated descriptions expose an explicit full-text action");
  eq(descriptionToggle.getAttribute("aria-expanded"), "false", "full description starts collapsed");
  const descriptionDetail = document.getElementById(`${firstDescription.id}-detail`) as HTMLElement | null;
  if (!descriptionDetail) throw new Error("long Ask detail region did not render");
  eq(descriptionToggle.getAttribute("aria-controls"), descriptionDetail.id, "disclosure identifies the separate detail region");
  eq(descriptionDetail.hidden, true, "full detail region starts hidden");

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
  eq(window.getComputedStyle(firstOption).height, "38px", "opening details does not resize the selected row");
  eq(descriptionToggle.getAttribute("aria-expanded"), "true", "expanded state is announced");
  eq(window.getComputedStyle(firstOption).alignItems, "center", "opening details keeps the selected row vertically centered");
  eq(window.getComputedStyle(firstDescription).overflow, "hidden", "the row summary remains clipped after opening details");
  eq(descriptionDetail.hidden, false, "full description opens in a separate region");
  eq(descriptionDetail.getAttribute("data-scrolled-into-view"), "true", "opened detail scrolls above the fixed decision footer");
  eq(
    descriptionDetail.textContent?.includes(ask.questions[0].options[0].description ?? ""),
    true,
    "separate detail region reveals the complete text",
  );

  await act(async () => {
    descriptionToggle.click();
    await flushTimers();
  });
  eq(descriptionDetail.hidden, true, "separate detail region can be collapsed again");
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

// Legacy or malformed Ask payloads may omit description and put the entire
// decision in label. Give those rows the full copy column, then disclose the
// original label only when it still overflows. The answer value stays exact.
{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const answers: QuestionAnswer[][] = [];
  const longLabel = "Keep every historical migration behavior while rebuilding the release validation path";
  const ask: WireAsk = {
    id: "ask-long-label-fallback",
    questions: [{
      id: "legacy-choice",
      prompt: "Choose a compatibility strategy",
      options: [
        { label: longLabel },
        { label: "Minimal repair" },
      ],
    }],
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
    await flushTimers(200);
  });

  const firstOption = document.querySelector(".prompt-shelf__actions .prompt-action") as HTMLButtonElement | null;
  const label = firstOption?.querySelector(".prompt-action__label") as HTMLElement | null;
  const copy = firstOption?.querySelector(".prompt-action__copy") as HTMLElement | null;
  const toggle = document.querySelector(".prompt-action__description-toggle") as HTMLButtonElement | null;
  if (!firstOption || !label || !copy || !toggle) throw new Error("long label fallback did not render");

  eq(window.getComputedStyle(copy).gridTemplateColumns, "minmax(0, 1fr)", "label-only decisions use the full copy width before truncating");
  eq(window.getComputedStyle(label).textOverflow, "ellipsis", "overflowing legacy labels keep the stable compact row");
  eq(firstOption.getAttribute("title"), longLabel, "overflowing labels retain their complete native tooltip");
  eq(toggle.getAttribute("aria-expanded"), "false", "overflowing label detail starts collapsed");

  await act(async () => {
    toggle.click();
    await flushTimers();
  });
  const detail = document.getElementById(toggle.getAttribute("aria-controls") ?? "") as HTMLElement | null;
  if (!detail) throw new Error("long label detail did not render");
  eq(detail.hidden, false, "overflowing label can be expanded outside the stable row");
  eq(detail.getAttribute("data-scrolled-into-view"), "true", "opened label detail is brought into the visible decision scroller");
  eq(detail.textContent?.trim(), longLabel, "label-only detail reveals the original complete decision text once");
  eq(
    window.getComputedStyle(detail.querySelector(".prompt-description-detail__label") as HTMLElement).whiteSpace,
    "normal",
    "full label detail wraps instead of being ellipsized again",
  );
  eq(answers.length, 0, "opening a long label never submits the decision");

  await act(async () => {
    (document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement).click();
    await flushTimers();
  });
  eq(answers.length, 1, "long label decision still submits normally");
  eq(answers[0]?.[0]?.selected?.[0], longLabel, "display fallback does not alter the answer value");

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
