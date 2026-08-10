// Run: tsx src/__tests__/approval-animation.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ApprovalModal } from "../components/ApprovalModal";
import { LocaleProvider } from "../lib/i18n";

let passed = 0;
let failed = 0;

type SubmittedAnswer = [allow: boolean, session: boolean, persist: boolean];
type ControllableAnimation = {
  onfinish: (() => void) | null;
  oncancel: (() => void) | null;
};

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
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window);
  Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
  return dom;
}

function mockNativeAnimate(
  dom: JSDOM,
  implementation: (options: KeyframeAnimationOptions) => ControllableAnimation,
) {
  Object.defineProperty(dom.window.Element.prototype, "animate", {
    configurable: true,
    value: (_frames: Keyframe[] | PropertyIndexedKeyframes | null, options: number | KeyframeAnimationOptions) => {
      if (typeof options === "number") throw new TypeError("expected keyframe animation options");
      return implementation(options) as unknown as Animation;
    },
  });
}

async function renderToolApproval(onAnswer: (...answer: SubmittedAnswer) => void) {
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <ApprovalModal
          approval={{ id: "approval-animation", tool: "bash", subject: "echo safe" }}
          onAnswer={onAnswer}
          onStop={() => undefined}
        />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  return root;
}

async function confirmSelectedAction() {
  const confirm = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement | null;
  if (!confirm) throw new Error("approval confirm button did not render");
  await act(async () => {
    confirm.click();
    await flushTimers();
  });
}

async function cleanup(root: Root, dom: JSDOM) {
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log("\napproval shelf animation");

// A real Web Animations implementation validates easing synchronously. Keep
// the business action pending until the visual transition finishes.
{
  const dom = installDom();
  const answers: SubmittedAnswer[] = [];
  const animations: ControllableAnimation[] = [];
  let easing: string | undefined;
  mockNativeAnimate(dom, (options) => {
    easing = options.easing;
    if (typeof easing !== "string" || easing.includes("power")) {
      throw new TypeError(`${String(easing)} is not a valid CSS easing`);
    }
    const animation: ControllableAnimation = { onfinish: null, oncancel: null };
    animations.push(animation);
    return animation;
  });

  const root = await renderToolApproval((...answer) => answers.push(answer));
  await confirmSelectedAction();

  eq(easing, "cubic-bezier(0.8, 0, 0.8, 0.28)", "shelf exit passes a valid CSS easing to Element.animate");
  eq(answers.length, 0, "approval waits for the shelf exit animation");
  eq(animations.length, 1, "approval starts one shelf exit animation");

  await act(async () => {
    animations[0].onfinish?.();
    animations[0].oncancel?.();
    await flushTimers();
  });
  eq(answers.length, 1, "finish and late cancel submit the approval only once");
  eq(JSON.stringify(answers[0]), JSON.stringify([true, false, false]), "finished animation preserves the selected approval");

  await cleanup(root, dom);
}

// Animation cancellation must not discard a decision that the user already
// confirmed.
{
  const dom = installDom();
  const answers: SubmittedAnswer[] = [];
  const animation: ControllableAnimation = { onfinish: null, oncancel: null };
  mockNativeAnimate(dom, () => animation);

  const root = await renderToolApproval((...answer) => answers.push(answer));
  await confirmSelectedAction();
  await act(async () => {
    animation.oncancel?.();
    await flushTimers();
  });

  eq(answers.length, 1, "cancelled shelf animation still submits the approval once");
  await cleanup(root, dom);
}

// The transition is cosmetic. Even a synchronously rejecting WebView must not
// block the underlying approval RPC.
{
  const dom = installDom();
  const answers: SubmittedAnswer[] = [];
  let attempts = 0;
  mockNativeAnimate(dom, () => {
    attempts += 1;
    throw new TypeError("WebView rejected the animation options");
  });

  const root = await renderToolApproval((...answer) => answers.push(answer));
  await confirmSelectedAction();
  await confirmSelectedAction();

  eq(attempts, 1, "a rejected animation is not retried by a second confirm");
  eq(answers.length, 1, "a rejected animation falls back to one approval submission");
  eq(JSON.stringify(answers[0]), JSON.stringify([true, false, false]), "animation fallback preserves the selected approval");

  await cleanup(root, dom);
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
