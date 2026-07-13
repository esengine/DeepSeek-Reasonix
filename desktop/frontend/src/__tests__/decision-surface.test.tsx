// Run: tsx src/__tests__/decision-surface.test.tsx
//
// Unified footer decision surface: select-then-confirm for approvals, ask,
// and clear context; no double submit; composer remains mounted under the host.

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import gsap from "gsap";
import { ApprovalModal } from "../components/ApprovalModal";
import { ClearContextCard } from "../components/ClearContextCard";
import { LocaleProvider } from "../lib/i18n";
import type { WireApproval } from "../lib/types";

let passed = 0;
let failed = 0;

type GsapToOptions = { onComplete?: () => void };
const gsapForTests = (typeof gsap.to === "function" ? gsap : (gsap as unknown as { default?: typeof gsap }).default) as unknown as {
  to?: (target: unknown, vars: GsapToOptions) => unknown;
};
if (typeof gsapForTests.to === "function") {
  gsapForTests.to = (_target: unknown, vars: GsapToOptions) => {
    vars.onComplete?.();
    return {};
  };
}

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

function flushTimers(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function installDom(language = "en-US") {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: language });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  return dom;
}

console.log("\ndecision surface");

// Tool approval: click only selects; confirm submits once; double-confirm ignored.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const answers: Array<[boolean, boolean, boolean]> = [];
  const approval: WireApproval = {
    id: "bash-1",
    tool: "bash",
    subject: "ls -la",
  };

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={approval}
          onAnswer={(a, s, p) => answers.push([a, s, p])}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  eq(actions.length, 4, "ordinary tool approval has four options");
  ok(actions[0].classList.contains("prompt-action--selected"), "default selection is allow once");

  await act(async () => {
    actions[3].click();
    await flushTimers();
  });
  eq(answers.length, 0, "clicking deny only selects");
  ok(actions[3].classList.contains("prompt-action--selected"), "deny becomes selected");

  const confirm = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement;
  await act(async () => {
    confirm.click();
    confirm.click();
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await flushTimers(220);
  });
  eq(answers.length, 1, "double click/enter submits only once");
  eq(JSON.stringify(answers[0]), JSON.stringify([false, false, false]), "deny maps to (false,false,false)");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Clear context: default cancel; clear requires explicit confirm; Escape cancels.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  let cancelled = 0;
  let confirmed = 0;

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ClearContextCard onCancel={() => { cancelled += 1; }} onConfirm={() => { confirmed += 1; }} />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  ok(actions[0].classList.contains("prompt-action--selected"), "clear context defaults to cancel");

  await act(async () => {
    actions[1].click();
    await flushTimers();
  });
  eq(confirmed, 0, "clicking clear only selects");
  ok(actions[1].classList.contains("prompt-action--selected"), "clear option becomes selected");

  await act(async () => {
    (document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement).click();
    await flushTimers();
  });
  eq(confirmed, 1, "confirm runs clear once");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  let cancelled = 0;

  await act(async () => {
    root.render(
      <LocaleProvider>
        <ClearContextCard onCancel={() => { cancelled += 1; }} onConfirm={() => undefined} />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushTimers();
  });
  eq(cancelled, 1, "Escape cancels clear context immediately");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Composer decision host stays in the tree while visually hidden.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <div>
        <div className="composer-decision-host composer-decision-host--hidden" hidden inert aria-hidden="true">
          <textarea id="composer-input" defaultValue="draft text" />
        </div>
      </div>,
    );
    await flushTimers();
  });

  const host = document.querySelector(".composer-decision-host") as HTMLElement;
  const input = document.getElementById("composer-input") as HTMLTextAreaElement;
  ok(host != null, "composer decision host remains mounted");
  ok(host.hasAttribute("hidden"), "host is hidden during decision");
  ok(host.hasAttribute("inert") || (host as HTMLElement & { inert?: boolean }).inert === true, "host is inert during decision");
  eq(input.value, "draft text", "draft value survives while host is hidden");
  eq(host.getAttribute("aria-hidden"), "true", "host is aria-hidden during decision");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// New approval id resets selection and submitting state.
{
  const dom = installDom();
  const root = createRoot(document.getElementById("root")!);
  const answers: Array<[boolean, boolean, boolean]> = [];
  let approval: WireApproval = { id: "a1", tool: "bash", subject: "echo 1" };

  const paint = async (next: WireApproval) => {
    approval = next;
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ApprovalModal
            approval={approval}
            onAnswer={(a, s, p) => answers.push([a, s, p])}
            onStop={() => undefined}
          />
        </LocaleProvider>,
      );
      await flushTimers();
    });
  };

  await paint(approval);
  const actions = () => [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
  await act(async () => {
    actions()[3].click();
    await flushTimers();
  });
  ok(actions()[3].classList.contains("prompt-action--selected"), "deny selected on first prompt");

  await paint({ id: "a2", tool: "bash", subject: "echo 2" });
  ok(actions()[0].classList.contains("prompt-action--selected"), "new prompt id resets selection to allow once");
  eq(answers.length, 0, "selection reset does not submit");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
