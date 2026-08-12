import { deepStrictEqual, equal } from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { RecoverySessionDialog } from "../components/RecoverySessionDialog";
import { LocaleProvider } from "../lib/i18n";
import { requestRecoveryResume } from "../lib/recoveryResume";
import type { HistoryPage } from "../lib/types";

const loadedPage: HistoryPage = {
  messages: [{ role: "user", content: "canonical" }],
  startTurn: 0,
  endTurn: 1,
  totalTurns: 1,
  hasOlder: false,
  resolvedPath: "canonical.jsonl",
  redirected: true,
};

let calls = 0;
const loaded = await requestRecoveryResume(
  { tabId: "tab-a", path: "legacy.jsonl", limit: 60 },
  async (tabId, path, limit) => {
    calls += 1;
    deepStrictEqual([tabId, path, limit], ["tab-a", "legacy.jsonl", 60]);
    return loadedPage;
  },
);
equal(calls, 1);
equal(loaded.kind, "loaded");
if (loaded.kind === "loaded") equal(loaded.page.resolvedPath, "canonical.jsonl");

const candidates = [
  { path: "a.jsonl", lastActivityAt: 10, summary: "alpha", turns: 2 },
  { path: "b.jsonl", lastActivityAt: 20, summary: "beta", turns: 3 },
];
const selection = await requestRecoveryResume(
  { tabId: "tab-a", path: "legacy.jsonl", limit: 60 },
  async () => ({
    messages: [], startTurn: 0, endTurn: 0, totalTurns: 0, hasOlder: false,
    selectionRequired: true, recoveryCandidates: candidates,
  }),
);
equal(selection.kind, "selection");
if (selection.kind === "selection") deepStrictEqual(selection.candidates, candidates);

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
globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;

const root = createRoot(document.getElementById("root")!);
const confirmed: string[] = [];
let cancelled = 0;
await act(async () => {
  root.render(
    React.createElement(
      LocaleProvider,
      null,
      React.createElement(RecoverySessionDialog, {
        candidates,
        busy: false,
        onConfirm: (path) => confirmed.push(path),
        onCancel: () => { cancelled += 1; },
      }),
    ),
  );
});

const options = Array.from(document.querySelectorAll<HTMLInputElement>('input[type="radio"]'));
const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>(".recovery-session-dialog button"));
const cancelButton = buttons[0];
const confirmButton = buttons[1];
equal(options.length, 2);
equal(options.some((option) => option.checked), false);
equal(confirmButton?.disabled, true);
equal(document.body.textContent?.includes("alpha"), true);
equal(document.body.textContent?.includes("beta"), true);
equal(document.body.textContent?.includes("2 message rounds"), true);
equal(document.body.textContent?.includes("3 message rounds"), true);
equal(document.body.textContent?.includes("a.jsonl"), false);
equal(document.body.textContent?.includes("b.jsonl"), false);

await act(async () => {
  options[1]?.click();
});
equal(options[1]?.checked, true);
equal(confirmButton?.disabled, false);
await act(async () => {
  confirmButton?.click();
});
deepStrictEqual(confirmed, ["b.jsonl"]);

await act(async () => {
  cancelButton?.click();
});
equal(cancelled, 1);

await act(async () => root.unmount());
dom.window.close();
