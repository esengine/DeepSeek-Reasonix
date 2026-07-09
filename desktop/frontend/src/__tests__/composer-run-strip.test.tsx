// Run: tsx src/__tests__/composer-run-strip.test.tsx
//
// The run state lives inside the composer card (no floating pill, no layout
// jump), stop has a fixed home next to send, and a pending approval/ask shifts
// the strip into a waiting state instead of a ticking "working" spinner.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer } from "../components/Composer";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { CollaborationMode, ToolApprovalMode, TokenMode } from "../lib/types";

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

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
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
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.InputEvent = dom.window.InputEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.File = dom.window.File;
  globalThis.FileReader = dom.window.FileReader;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.ResizeObserver = TestResizeObserver;
  Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(window, "matchMedia", {
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
  return dom;
}

async function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const calls = { cancel: 0 };
  let currentProps: Parameters<typeof Composer>[0] = {
    running: false,
    collaborationMode: "normal" as CollaborationMode,
    toolApprovalMode: "ask" as ToolApprovalMode,
    tokenMode: "full" as TokenMode,
    goal: "",
    cwd: "/repo",
    modelLabel: "DeepSeek-R1",
    onSend: () => {},
    onCancel: () => {
      calls.cancel += 1;
      return undefined;
    },
    onCycleMode: () => {},
    onSetMode: () => {},
    onSetCollaborationMode: () => {},
    onSetToolApprovalMode: () => {},
    onToggleYoloApprovalMode: () => {},
    onClearGoal: () => {},
    onSwitchModel: () => {},
    onSetEffort: () => {},
    onSetTokenMode: () => {},
    ready: true,
    ...props,
  };
  const paint = async (nextProps: Partial<Parameters<typeof Composer>[0]> = {}) => {
    currentProps = { ...currentProps, ...nextProps };
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <Composer {...currentProps} />
          </ToastProvider>
        </LocaleProvider>,
      );
      await flushTimers();
    });
  };
  await paint();
  return { root, calls, rerender: paint };
}

console.log("\ncomposer run strip");

// Idle: no strip, no stop button, plain send arrow.
{
  const dom = installDom();
  const { root } = await renderComposer();

  eq(document.querySelector(".composer-run-strip"), null, "idle composer renders no run strip");
  eq(document.querySelector(".composer__btn--stop"), null, "idle composer renders no stop button");
  ok(document.querySelector(".composer__btn--send") !== null, "idle composer keeps the send button");
  eq(document.querySelector(".composer-toolbar--status-only"), null, "floating status pill is gone");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Running: strip lives inside the card, ticker is aria-hidden, stop cancels.
{
  const dom = installDom();
  const { root, calls } = await renderComposer({ running: true, turnStartAt: Date.now() });

  const strip = document.querySelector(".composer-card .composer-run-strip");
  ok(strip !== null, "running strip renders inside the composer card");
  const ticker = strip?.querySelector(".composer-run-strip__text");
  eq(ticker?.getAttribute("aria-hidden"), "true", "ticking spinner text stays out of the accessibility tree");
  const live = strip?.querySelector(".sr-only[role=\"status\"]");
  eq(live?.textContent, "Reasonix is working", "live region announces the stable state text only");
  ok(document.querySelector(".composer-card--running") !== null, "running card keeps its running modifier");

  const stop = document.querySelector(".composer__btn--stop") as HTMLButtonElement | null;
  if (!stop) throw new Error("running composer stop button did not render");
  await act(async () => {
    stop.click();
    await flushTimers();
  });
  eq(calls.cancel, 1, "stop button next to send cancels the turn");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Waiting on approval: strip switches to the waiting state and stops ticking.
{
  const dom = installDom();
  const { root, rerender } = await renderComposer({ running: true, turnStartAt: Date.now() });

  await rerender({ pendingApprovalLabel: "Run command", disabled: true });

  const strip = document.querySelector(".composer-run-strip");
  ok(strip?.classList.contains("composer-run-strip--waiting") === true, "pending approval shifts the strip into waiting");
  const text = strip?.querySelector(".composer-run-strip__text");
  eq(text?.textContent, "Waiting for your approval — Run command", "waiting strip names the tool awaiting approval");
  eq(text?.getAttribute("aria-hidden"), null, "waiting text is static and stays accessible");
  eq(document.querySelector(".composer-card--running"), null, "waiting card hands the running accent off to the prompt card");
  ok(document.querySelector(".composer-card--waiting") !== null, "waiting card takes the waiting modifier");

  const modeButtons = [...document.querySelectorAll(".composer-modebar--approval .composer-modebar__item")] as HTMLButtonElement[];
  ok(modeButtons.length === 3 && modeButtons.every((b) => !b.disabled), "approval bar stays usable while its own prompt disables the composer");

  await rerender({ pendingApprovalLabel: null, pendingAsk: true });
  eq(
    document.querySelector(".composer-run-strip__text")?.textContent,
    "Waiting for your answer",
    "pending ask question shows the ask waiting state",
  );
  ok(
    modeButtons.every((b) => b.disabled),
    "approval bar stays disabled for non-approval reasons",
  );

  await rerender({ pendingAsk: false, disabled: false });
  ok(
    document.querySelector(".composer-run-strip__text")?.getAttribute("aria-hidden") === "true",
    "resolving the prompt returns the strip to the ticking spinner",
  );

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Cancel restores queued guidance: stop means "stop acting", never "discard
// what I typed".
{
  const dom = installDom();
  const { root, calls } = await renderComposer({
    running: true,
    turnStartAt: Date.now(),
    guidanceQueuePreviewItems: ["数到一半改用英文", "最后给出一句总结"],
  });

  ok(document.querySelector(".composer-guidance-shelf") !== null, "queued guidance renders in the shelf");

  const stop = document.querySelector(".composer__btn--stop") as HTMLButtonElement | null;
  if (!stop) throw new Error("stop button did not render");
  await act(async () => {
    stop.click();
    await flushTimers();
  });

  eq(calls.cancel, 1, "stop cancels the running turn");
  const ta = document.querySelector("textarea") as HTMLTextAreaElement;
  eq(ta.value, "数到一半改用英文\n最后给出一句总结", "stop folds unconsumed queued guidance back into the draft");
  eq(document.querySelector(".composer-guidance-shelf"), null, "restored queue clears the shelf");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Waiting on the user pauses the ticker clock: elapsed time means model time.
{
  const dom = installDom();
  const start = Date.now() - 30000;
  const { root, rerender } = await renderComposer({ running: true, turnStartAt: start });

  await rerender({ pendingApprovalLabel: "Run command", disabled: true });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 2400));
  });
  await rerender({ pendingApprovalLabel: null, disabled: false });

  const ticker = document.querySelector(".composer-run-strip__text")?.textContent ?? "";
  ok(/ 30s| 31s/.test(ticker), `ticker excludes the time spent waiting for approval (got "${ticker}")`);
  ok(!/ 32s| 33s/.test(ticker), "ticker does not count the ~2.4s approval wait as model time");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Resize consistency: --composer-height always carries the logical height in
// every writer (React render, live drag, keyboard), with the run strip's
// reservation isolated in a CSS calc — so dragging a resized composer during a
// running turn cannot flash-shrink the card.
{
  const stylesSource = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../styles.css"), "utf8");
  ok(
    stylesSource.includes("calc(var(--composer-height) + var(--composer-run-strip-reserved, 0px))"),
    "resized card height combines logical height and strip reservation in CSS",
  );

  const dom = installDom();
  const { root, rerender } = await renderComposer({ running: true, turnStartAt: Date.now() });

  const handle = document.querySelector(".composer-resize-handle") as HTMLButtonElement;
  await act(async () => {
    handle.focus();
    handle.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Home", bubbles: true }));
    await flushTimers();
  });

  const card = document.querySelector(".composer-card") as HTMLElement;
  eq(card.style.getPropertyValue("--composer-height"), "104px", "render path writes the logical height, not a compensated one");
  eq(card.style.getPropertyValue("--composer-run-strip-reserved"), "30px", "running card reserves the strip height via its own variable");

  // Drag while running: the live writer stays in logical-height space.
  await act(async () => {
    handle.dispatchEvent(new window.MouseEvent("pointerdown", { bubbles: true, clientY: 300 }));
    await flushTimers();
  });
  eq(card.style.getPropertyValue("--composer-height"), "104px", "drag start does not flash-shrink the running card");

  await act(async () => {
    document.dispatchEvent(new window.MouseEvent("pointermove", { bubbles: true, clientY: 280 }));
    document.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, clientY: 280 }));
    await flushTimers();
  });
  eq(card.style.getPropertyValue("--composer-height"), "124px", "drag release keeps the same logical-height space as the render path");
  eq(card.style.getPropertyValue("--composer-run-strip-reserved"), "30px", "strip reservation survives the drag");
  eq(handle.getAttribute("aria-valuenow"), "124", "separator reports the logical height");

  await rerender({ running: false, turnStartAt: undefined });
  eq(card.style.getPropertyValue("--composer-run-strip-reserved"), "0px", "idle card releases the strip reservation");
  eq(card.style.getPropertyValue("--composer-height"), "124px", "idle card keeps the user's logical height");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
