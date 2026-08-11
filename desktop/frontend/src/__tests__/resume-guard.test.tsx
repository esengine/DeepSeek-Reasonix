// Run: tsx src/__tests__/resume-guard.test.tsx
// The resume guard estimates a session from the read-only PreviewSession and
// asks before the mutating ResumeSessionPageForTab call: confirming continues
// the hydrate, cancelling leaves the tab untouched (the resume call never
// happens) and surfaces a cancelled hydrate error.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { useController } from "../lib/useController";
import type { AppBindings } from "../lib/bridge";
import type { BalanceInfo, CheckpointMeta, ContextInfo, EffortInfo, HistoryMessage, JobView, Meta, TabMeta } from "../lib/types";

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

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

function tabMeta(): TabMeta {
  return {
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    topicId: "topic-a",
    topicTitle: "General",
    label: "model",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
  };
}

function meta(): Meta {
  return { label: "model", ready: true, cwd: "/repo", workspaceRoot: "/repo", topicId: "topic-a" };
}

const context: ContextInfo = { used: 10, window: 1_000_000, sessionTokens: 0, compactRatio: 0.2 };
const balance: BalanceInfo = { available: false, display: "" };
const jobs: JobView[] = [];
const checkpoints: CheckpointMeta[] = [];
const effort: EffortInfo = { supported: true, current: "auto", default: "auto", levels: ["auto"] };

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id='root'></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Node = dom.window.Node;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.ResizeObserver = TestResizeObserver;
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: () => ({
      matches: true,
      media: "(prefers-reduced-motion: reduce)",
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
  return dom;
}

type Controller = ReturnType<typeof useController>;
let controller: Controller | undefined;
let resumeCalls = 0;
let channelCalls = 0;

// estimatedTokens ≈ content length × 0.25; 820_000 chars → ~205_000, above
// the 200_000 threshold (window 1_000_000 × compactRatio 0.2).
const bigPreview: HistoryMessage[] = [{ role: "user", content: "x".repeat(820_000), level: "user" }];
const smallPreview: HistoryMessage[] = [{ role: "user", content: "small", level: "user" }];

async function setup(preview: HistoryMessage[]) {
  resumeCalls = 0;
  channelCalls = 0;
  const dom = installDom();
  window.runtime = {
    EventsOn: () => () => {},
    BrowserOpenURL: () => {},
  };
  window.go = {
    main: {
      App: {
        ListTabs: async () => [tabMeta()],
        MetaForTab: async () => meta(),
        ContextUsageForTab: async () => context,
        EffortForTab: async () => effort,
        BalanceForTab: async () => balance,
        JobsForTab: async () => jobs,
        CheckpointsForTab: async () => checkpoints,
        HistoryForTab: async () => [],
        HistoryPageForTab: async () => ({ messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false }),
        HistoryCheckpointTurnsForTab: async () => [],
        ReplayPendingPrompts: async () => {},
        PreviewSession: async () => preview,
        ResumeSessionPageForTab: async () => {
          resumeCalls += 1;
          return {
            messages: [
              { role: "user", content: "restore", level: "user" } as HistoryMessage,
              { role: "assistant", content: "done", level: "assistant" } as HistoryMessage,
            ],
            startTurn: 0,
            endTurn: 1,
            totalTurns: 1,
            hasOlder: false,
          };
        },
        ResumeSessionForTab: async () => [],
        OpenChannelSessionPageForTab: async () => {
          channelCalls += 1;
          return { messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false };
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };

  function Probe() {
    controller = useController();
    return <>{controller?.resumeGuardDialog}</>;
  }

  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(<Probe />);
    await flushPromises();
  });
  await waitFor("active tab", () => controller?.activeTabId === "tab-a");
  return { dom, root };
}

console.log("\nresume guard");

{
  // Over-threshold resume asks first; confirming continues the hydrate.
  const { dom, root } = await setup(bigPreview);

  let resume: Promise<void>;
  await act(async () => {
    resume = controller?.resumeSession("/repo/session.jsonl", "tab-a") ?? Promise.resolve();
    await flushPromises();
  });
  void resume;

  const dialog = document.querySelector(".reasonix-confirm-dialog");
  ok(dialog !== null, "over-threshold resume opens the confirm dialog");
  const message = document.querySelector(".reasonix-confirm-dialog__message")?.textContent ?? "";
  ok(
    message.includes("205,000") && message.includes("200,000"),
    `dialog names the estimated size and threshold, got ${message}`,
  );
  ok(resumeCalls === 0, "the mutating resume call has not happened while the dialog is open");

  const confirmButton = [...document.querySelectorAll(".reasonix-confirm-dialog button")]
    .find((b) => b.textContent?.includes("Resume"));
  ok(confirmButton !== undefined, "confirm button labelled for resuming");
  await act(async () => {
    (confirmButton as HTMLButtonElement).click();
    await flushPromises();
  });

  await waitFor("hydrate completes after confirm", () => document.querySelector(".reasonix-confirm-dialog") === null);
  await waitFor("hydrate history loaded", () => (controller?.state.items?.length ?? 0) > 0);
  ok(
    !controller?.state.hydrateError && (controller?.state.items?.length ?? 0) > 0,
    "confirmed resume hydrates the restored conversation",
  );
  ok(resumeCalls === 1, "confirmed resume performs exactly one mutating call");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // Cancelling never performs the mutating resume call and surfaces a notice.
  const { dom, root } = await setup(bigPreview);

  let resume: Promise<void>;
  await act(async () => {
    resume = controller?.resumeSession("/repo/session.jsonl", "tab-a") ?? Promise.resolve();
    await flushPromises();
  });
  void resume;
  const dialog = document.querySelector(".reasonix-confirm-dialog");
  ok(dialog !== null, "over-threshold resume re-opens the confirm dialog");

  const cancelButton = [...document.querySelectorAll(".reasonix-confirm-dialog button")]
    .find((b) => b.textContent === "Cancel");
  await act(async () => {
    (cancelButton as HTMLButtonElement).click();
    await flushPromises();
  });
  await waitFor("dialog closes on cancel", () => document.querySelector(".reasonix-confirm-dialog") === null);
  ok(
    controller?.state.hydrateError != null && controller?.state.hydrateError.includes("unchanged"),
    `cancelled resume surfaces a hydrate error, got ${controller?.state.hydrateError}`,
  );
  ok(resumeCalls === 0, "cancelled resume never performs the mutating call");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // A session below the threshold resumes without any confirmation.
  const { dom, root } = await setup(smallPreview);

  let resume: Promise<void>;
  await act(async () => {
    resume = controller?.resumeSession("/repo/session.jsonl", "tab-a") ?? Promise.resolve();
    await flushPromises();
  });
  void resume;
  await waitFor("below-threshold hydrate completes", () => (controller?.state.items?.length ?? 0) > 0);
  ok(
    document.querySelector(".reasonix-confirm-dialog") === null,
    "below-threshold resume skips the confirm dialog",
  );
  ok(resumeCalls === 1, "below-threshold resume performs the mutating call once");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // The channel-open path gets the same guard: cancelling never performs the
  // mutating OpenChannelSessionPageForTab call.
  const { dom, root } = await setup(bigPreview);

  let channel: Promise<void>;
  await act(async () => {
    channel = controller?.openChannelSession("/repo/session.jsonl", "tab-a") ?? Promise.resolve();
    await flushPromises();
  });
  void channel;
  ok(
    document.querySelector(".reasonix-confirm-dialog") !== null,
    "over-threshold channel open asks for confirmation",
  );
  ok(channelCalls === 0, "the mutating channel call has not happened while the dialog is open");
  const cancelButton = [...document.querySelectorAll(".reasonix-confirm-dialog button")]
    .find((b) => b.textContent === "Cancel");
  await act(async () => {
    (cancelButton as HTMLButtonElement).click();
    await flushPromises();
  });
  await waitFor("dialog closes on cancel", () => document.querySelector(".reasonix-confirm-dialog") === null);
  ok(channelCalls === 0, "cancelled channel open never performs the mutating call");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
