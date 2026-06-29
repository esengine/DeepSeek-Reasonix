// Run: tsx src/__tests__/approval-modal-file-reference.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ApprovalModal } from "../components/ApprovalModal";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { WireApproval } from "../lib/types";

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

function flushTimers(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitFor(label: string, predicate: () => boolean, timeoutMs = 1000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (predicate()) return;
    await act(async () => {
      await flushTimers(20);
    });
  }
  ok(false, label);
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
  globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.InputEvent = dom.window.InputEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window);
  Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
  return dom;
}

function mockApp(methods: Partial<AppBindings>) {
  window.go = {
    main: {
      App: {
        ...methods,
      } as Partial<AppBindings> as AppBindings,
    },
  };
}

async function renderApproval(props: Partial<Parameters<typeof ApprovalModal>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const revisions: string[] = [];
  const activeStates: boolean[] = [];
  const approval: WireApproval = {
    id: "plan-approval",
    tool: "exit_plan_mode",
    subject: "Plan ready",
  };
  let currentProps: Parameters<typeof ApprovalModal>[0] = {
    approval,
    cwd: "/repo",
    onAnswer: () => undefined,
    onRevisePlan: (text) => revisions.push(text),
    onExitPlan: () => undefined,
    onStop: () => undefined,
    onRevisionActiveChange: (active) => activeStates.push(active),
    ...props,
  };
  const paint = async (nextProps: Partial<Parameters<typeof ApprovalModal>[0]> = {}) => {
    currentProps = { ...currentProps, ...nextProps };
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ApprovalModal {...currentProps} />
        </LocaleProvider>,
      );
      await flushTimers();
    });
  };
  await paint();
  return { root, revisions, activeStates, rerender: paint };
}

console.log("\napproval modal file references");

{
  const dom = installDom();
  mockApp({
    ListDir: async () => [{ name: "src", isDir: true }, { name: "README.md", isDir: false }],
    SearchFileRefs: async () => [],
  });
  const { root, revisions, rerender } = await renderApproval();

  const reviseButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("Revise plan")) as HTMLButtonElement | undefined;
  if (!reviseButton) throw new Error("revise button did not render");

  await act(async () => {
    reviseButton.click();
    await flushTimers();
  });

  const textarea = document.querySelector(".plan-revision__input") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("plan revision textarea did not render");

  await rerender({ insertRequest: { id: 1, text: "please inspect @" } });
  await waitFor("plan revision @ text opens file suggestions", () => document.body.textContent?.includes("README.md") === true);

  ok(document.body.textContent?.includes("README.md") === true, "plan revision @ text opens file suggestions");

  const readmeButton = Array.from(document.querySelectorAll(".slashmenu__item")).find((button) => button.textContent?.includes("README.md")) as HTMLButtonElement | undefined;
  if (!readmeButton) throw new Error("README file suggestion did not render");

  await act(async () => {
    readmeButton.dispatchEvent(new window.MouseEvent("mousedown", { bubbles: true, cancelable: true }));
    await flushTimers();
  });

  eq(textarea.value, "please inspect @README.md ", "file suggestion completes inline in the plan revision");

  const sendButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("Send update")) as HTMLButtonElement | undefined;
  if (!sendButton) throw new Error("send revision button did not render");

  await act(async () => {
    sendButton.click();
    await flushTimers(220);
  });

  eq(revisions.join(","), "please inspect @README.md", "submitted plan revision keeps the selected file reference");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const { root, activeStates, rerender } = await renderApproval();

  const reviseButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("Revise plan")) as HTMLButtonElement | undefined;
  if (!reviseButton) throw new Error("revise button did not render");

  await act(async () => {
    reviseButton.click();
    await flushTimers();
  });

  const textarea = document.querySelector(".plan-revision__input") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("plan revision textarea did not render");

  await rerender({ insertRequest: { id: 2, text: "@src/main.go" } });

  eq(textarea.value, "@src/main.go", "workspace add-reference insert request targets the plan revision input");
  ok(activeStates.includes(true), "plan revision reports itself as the active workspace insertion target");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
