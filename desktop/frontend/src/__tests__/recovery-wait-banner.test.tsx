// Run: tsx src/__tests__/recovery-wait-banner.test.tsx
//
// A minute-scale provider-recovery wait renders a prominent banner above the
// composer card (phase, code, countdown, waited-vs-budget, Stop) while the
// small run-strip status line keeps working; short retries render no banner.

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer } from "../components/Composer";
import { RecoveryWaitBanner } from "../components/RecoveryWaitBanner";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import type { CollaborationMode, ToolApprovalMode } from "../lib/types";

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

// The composer loads the banner as a lazy chunk; give the dynamic import a
// few macrotasks to resolve before asserting on it.
async function settle(ready: () => boolean): Promise<void> {
  for (let i = 0; i < 100 && !ready(); i += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10));
    });
  }
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

const waitingRecovery = {
  phase: "headers",
  reason: "rate_limit",
  next_attempt_at: 0,
  waited_ms: 74_000,
  wait_budget_ms: 600_000,
  waiting: true,
};

console.log("\nrecovery wait banner");

// Standalone banner: phase title, code, countdown, waited-vs-budget, Stop.
{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const now = 1_700_000_000_000;
  let stops = 0;
  const paint = async (retry: Parameters<typeof RecoveryWaitBanner>[0]["retry"]) => {
    await act(async () => {
      root.render(
        <LocaleProvider>
          <RecoveryWaitBanner retry={retry} now={now} onStop={() => { stops += 1; }} />
        </LocaleProvider>,
      );
      await flushTimers();
    });
  };

  await paint({ attempt: 4, max: 3, recovery: { ...waitingRecovery, next_attempt_at: now + 42_000 } });
  const banner = document.querySelector(".recovery-wait-banner");
  ok(banner !== null, "waiting recovery renders the banner");
  eq(banner?.getAttribute("data-phase"), "headers", "banner exposes the failure phase");
  eq(document.querySelector(".recovery-wait-banner__title")?.textContent, "Waiting for the provider to recover", "headers phase reads as a provider outage");
  eq(document.querySelector(".recovery-wait-banner__countdown")?.textContent, "Next attempt in 42s", "countdown derives from next_attempt_at");
  eq(document.querySelector(".recovery-wait-banner__progress")?.textContent, "Waited 1 min of 10 min", "waited-so-far is shown against the wait budget");
  eq(document.querySelector(".recovery-wait-banner__code")?.textContent, "rate_limit", "provider error code is shown");
  await act(async () => {
    document.querySelector<HTMLButtonElement>(".recovery-wait-banner__stop")?.click();
    await flushTimers();
  });
  eq(stops, 1, "Stop calls the cancel action");

  await paint({ attempt: 4, max: 3, recovery: { phase: "connect", next_attempt_at: now + 5_000, waited_ms: 14_000, waiting: true } });
  eq(document.querySelector(".recovery-wait-banner__title")?.textContent, "Waiting for the network to recover", "connect phase reads as a network outage");
  eq(document.querySelector(".recovery-wait-banner__progress"), null, "an older kernel without a budget shows no waited-vs-budget line");
  eq(document.querySelector(".recovery-wait-banner__code"), null, "no code chip without a provider code");

  await paint({ attempt: 2, max: 3 });
  eq(document.querySelector(".recovery-wait-banner"), null, "short retries render no banner");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Composer host: the banner sits above the card, the run strip keeps its
// status line, and Stop routes through the composer's cancel path.
{
  const dom = installDom();
  const { root, calls, rerender } = await renderComposer({
    running: true,
    retry: { attempt: 4, max: 3, recovery: { ...waitingRecovery, next_attempt_at: Date.now() + 60_000 } },
  });

  await settle(() => document.querySelector(".recovery-wait-banner") !== null);
  const banner = document.querySelector(".composer-wrap > .recovery-wait-banner");
  ok(banner !== null, "waiting recovery renders the banner inside the composer area");
  ok(banner?.nextElementSibling?.classList.contains("composer-card") ?? false, "banner sits directly above the composer card");
  const strip = document.querySelector(".composer-run-strip__text")?.textContent ?? "";
  ok(strip.startsWith("Waiting for provider (service unavailable)"), `run strip keeps the compact status line: ${JSON.stringify(strip)}`);
  await act(async () => {
    document.querySelector<HTMLButtonElement>(".recovery-wait-banner__stop")?.click();
    await flushTimers();
  });
  eq(calls.cancel, 1, "banner Stop calls onCancel exactly once");

  await rerender({ retry: { attempt: 2, max: 3 } });
  eq(document.querySelector(".recovery-wait-banner"), null, "a short retry hides the banner");
  eq(document.querySelector(".composer-run-strip__text")?.textContent, "retrying (2/3)…", "short retries keep the retrying status line");

  await rerender({ running: false, retry: undefined });
  eq(document.querySelector(".recovery-wait-banner"), null, "idle composer renders no banner");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
