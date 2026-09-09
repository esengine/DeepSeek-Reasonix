// Run: tsx src/__tests__/history-recovery-copies.test.tsx

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { SessionMeta } from "../lib/types";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".svg")) return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    return nextResolve(specifier, context);
  },
});

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
  ok(JSON.stringify(actual) === JSON.stringify(expected), `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", { pretendToBeVisual: true, url: "http://localhost/" });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: "en-US" });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window);
  return dom;
}

const now = 1_750_000_000_000;

function session(overrides: Partial<SessionMeta> & { path: string }): SessionMeta {
  return {
    preview: "session preview",
    turns: 3,
    createdAt: now - 3_600_000,
    lastActivityAt: now,
    modTime: now,
    current: false,
    open: false,
    ...overrides,
  };
}

async function renderPanel(props: Record<string, unknown>) {
  const { HistoryPanel } = await import("../components/HistoryPanel");
  const { LocaleProvider } = await import("../lib/i18n");
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <HistoryPanel
          running={false}
          onResume={() => {}}
          onPreview={async () => []}
          onDelete={() => {}}
          onRename={() => {}}
          onClose={() => {}}
          sessions={[]}
          {...props}
        />
      </LocaleProvider>,
    );
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
  return root;
}

function findButton(text: string): HTMLButtonElement | undefined {
  return Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.trim() === text) as HTMLButtonElement | undefined;
}

async function click(button: HTMLButtonElement) {
  await act(async () => {
    button.click();
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
}

console.log("\nhistory recovery data visibility");

{
  const dom = installDom();
  const root = await renderPanel({
    kind: "history",
    sessions: [
      session({ path: "/s/normal.jsonl", title: "normal session" }),
      session({ path: "/s/continued.jsonl", title: "continued session", recovered: true }),
      session({ path: "/s/covered.jsonl", title: "covered copy", recovered: true, recoveryCopy: true }),
    ],
  });

  ok(document.body.textContent?.includes("normal session") === true, "ordinary history keeps normal sessions");
  ok(document.body.textContent?.includes("continued session") === true, "unique recovered content remains ordinary history");
  ok(document.body.textContent?.includes("covered copy") === false, "covered copies are hidden from ordinary history");
  ok(!document.body.textContent?.includes("Recovery copies"), "ordinary history exposes no recovery-copy group");
  ok(!findButton("Trash recovery copies"), "ordinary history exposes no copy cleanup action");
  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  const emptied: string[][] = [];
  const restored: string[] = [];
  const purged: string[] = [];
  const root = await renderPanel({
    kind: "trash",
    sessions: [
      session({ path: "/t/normal.jsonl", title: "deleted session", deletedAt: now }),
      session({ path: "/t/covered.jsonl", title: "system copy", deletedAt: now, recovered: true, recoveryCopy: true }),
    ],
    onRestore: async (path: string) => { restored.push(path); },
    onPurge: async (path: string) => { purged.push(path); },
    onPurgeAll: async (paths: string[]) => { emptied.push(paths); },
  });

  ok(document.body.textContent?.includes("system copy") === false, "trash collapses system recovery data by default");
  const disclosure = document.querySelector<HTMLButtonElement>(".history-system-recovery__toggle") ?? undefined;
  ok(Boolean(disclosure), "trash provides a collapsed advanced recovery-data disclosure");
  if (disclosure) await click(disclosure);
  ok(document.body.textContent?.includes("system copy") === true, "advanced disclosure keeps recovery data restorable");
  const systemCopy = Array.from(document.querySelectorAll<HTMLButtonElement>(".hist-item__main"))
    .find((button) => button.textContent?.includes("system copy"));
  if (systemCopy) await click(systemCopy);
  const restore = findButton("Restore");
  ok(Boolean(restore) && !restore?.disabled, "system recovery data retains its restore action");
  if (restore) await click(restore);
  eq(restored, ["/t/covered.jsonl"], "restore targets the selected system recovery item");
  const purge = findButton("Delete permanently");
  ok(Boolean(purge) && !purge?.disabled, "system recovery data retains permanent delete control");
  if (purge) {
    await click(purge);
    const confirmPurge = findButton("Confirm permanent delete");
    if (confirmPurge) await click(confirmPurge);
  }
  eq(purged, ["/t/covered.jsonl"], "permanent delete targets the selected system recovery item");

  const clear = findButton("Empty trash");
  ok(Boolean(clear), "ordinary trash still exposes its normal empty action");
  if (clear) {
    await click(clear);
    const confirm = findButton("Confirm empty trash");
    if (confirm) await click(confirm);
  }
  eq(emptied, [["/t/normal.jsonl"]], "empty trash excludes protected system recovery data");
  await act(async () => root.unmount());
  dom.window.close();
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
