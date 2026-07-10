// Run: tsx src/__tests__/history-recovery-copies.test.tsx
//
// Recovery-copy bulk actions in HistoryPanel: the history view sweeps idle
// copies into the trash (skipping current/open ones), the trash view purges
// them, and both flows keep normal sessions untouched.

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import type { SessionMeta } from "../lib/types";

// HistoryPanel transitively imports Welcome's SVG wordmark; tsx has no asset
// loader, so redirect .svg specifiers to an empty-string module stub, the way
// Vite would default-export a URL.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".svg")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
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
  if (JSON.stringify(actual) === JSON.stringify(expected)) ok(true, label);
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
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
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
          {...props}
        />
      </LocaleProvider>,
    );
    await flushTimers(30);
  });
  return root;
}

function findButton(text: string): HTMLButtonElement | undefined {
  return Array.from(document.querySelectorAll("button")).find((b) => b.textContent?.trim() === text) as
    | HTMLButtonElement
    | undefined;
}

async function click(button: HTMLButtonElement) {
  await act(async () => {
    button.click();
    await flushTimers(20);
  });
}

console.log("\nhistory panel recovery-copy bulk actions");

// History view: the sweep button trashes idle recovery copies only.
{
  const dom = installDom();
  const deleted: string[][] = [];
  const purged: string[][] = [];
  const root = await renderPanel({
    kind: "history",
    sessions: [
      session({ path: "/s/normal.jsonl" }),
      session({ path: "/s/continued-recovery-0123456789abcdef.jsonl", title: "continued recovery kept", recovered: true }),
      session({ path: "/s/idle-recovery-0123456789abcdef.jsonl", recovered: true, recoveryCopy: true }),
      session({ path: "/s/open-recovery-0123456789abcdef.jsonl", recovered: true, recoveryCopy: true, open: true }),
      session({ path: "/s/current-recovery-0123456789abcdef.jsonl", recovered: true, recoveryCopy: true, current: true }),
    ],
    onDeleteMany: (paths: string[]) => deleted.push(paths),
    onPurgeAll: (paths: string[]) => purged.push(paths),
    onPurgeRecoveryCopies: (paths: string[]) => purged.push(paths),
  });

  const sweep = findButton("Trash recovery copies");
  ok(Boolean(sweep), "history view shows the trash-recovery-copies button");
  if (sweep) {
    await click(sweep); // arm
    const confirm = findButton("Confirm trash copies");
    ok(Boolean(confirm), "sweep arms into a confirm step");
    if (confirm) await click(confirm);
  }
  eq(deleted, [["/s/idle-recovery-0123456789abcdef.jsonl"]], "sweep trashes only idle recovery copies");
  ok(document.body.textContent?.includes("continued recovery kept"), "continued recovery remains in normal history");
  eq(purged, [], "history sweep never purges");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// History view: no idle copies → no sweep button.
{
  const dom = installDom();
  const root = await renderPanel({
    kind: "history",
    sessions: [
      session({ path: "/s/normal.jsonl" }),
      session({ path: "/s/current-recovery-0123456789abcdef.jsonl", recovered: true, recoveryCopy: true, current: true }),
    ],
    onDeleteMany: () => {},
  });
  ok(!findButton("Trash recovery copies"), "history view hides the sweep button when every copy is live");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Trash view: clear copies purges recovery copies, empty trash stays separate.
{
  const dom = installDom();
  const purged: string[][] = [];
  const emptied: string[][] = [];
  const root = await renderPanel({
    kind: "trash",
    sessions: [
      session({ path: "/t/normal.jsonl", deletedAt: now }),
      session({ path: "/t/continued-recovery-0123456789abcdef.jsonl", deletedAt: now, recovered: true }),
      session({ path: "/t/a-recovery-0123456789abcdef.jsonl", deletedAt: now, recovered: true, recoveryCopy: true }),
      session({ path: "/t/b-recovery-0123456789abcdef.jsonl", deletedAt: now, recovered: true, recoveryCopy: true }),
    ],
    onRestore: () => {},
    onPurge: () => {},
    onPurgeAll: (paths: string[]) => emptied.push(paths),
    onPurgeRecoveryCopies: (paths: string[]) => purged.push(paths),
  });

  const clear = findButton("Clear copies");
  ok(Boolean(clear), "trash view shows the clear-copies button");
  if (clear) {
    await click(clear); // arm
    const confirm = findButton("Confirm clear copies");
    ok(Boolean(confirm), "clear copies arms into a confirm step");
    if (confirm) await click(confirm);
  }
  eq(
    purged,
    [["/t/a-recovery-0123456789abcdef.jsonl", "/t/b-recovery-0123456789abcdef.jsonl"]],
    "clear copies purges every trashed recovery copy and nothing else",
  );
  eq(emptied, [], "clear copies does not invoke the unguarded empty-trash action");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
