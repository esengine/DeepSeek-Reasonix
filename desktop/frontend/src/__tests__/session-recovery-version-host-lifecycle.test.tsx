import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { ProjectTreeChangedV2 } from "../lib/sessionCatalogTypes";
import type { SessionRecoveryEvent } from "../lib/types";

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", { pretendToBeVisual: true, url: "http://localhost/" });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: "en-US" });
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;

type RuntimeHandler = (...data: unknown[]) => void;
const handlers = new Map<string, Set<RuntimeHandler>>();
window.runtime = {
  EventsOn: (name: string, callback: RuntimeHandler) => {
    const listeners = handlers.get(name) ?? new Set<RuntimeHandler>();
    listeners.add(callback);
    handlers.set(name, listeners);
    return () => listeners.delete(callback);
  },
  BrowserOpenURL: () => {},
};

const lineageReads: Array<Record<string, unknown>> = [];
window.go = {
  main: {
    App: {
      GetRecoveryLineage: async (key: Record<string, unknown>) => {
        lineageReads.push(key);
        return {
          groupId: "root",
          state: "diverged",
          branchCount: 2,
          unresolved: 1,
          cleanupEligible: 0,
          members: [
            { path: "/sessions/root.jsonl", role: "normal", canonical: true, turns: 1, open: false, running: false },
            { path: "/sessions/fork.jsonl", role: "diverged", canonical: false, turns: 2, open: false, running: false },
          ],
        };
      },
    },
  },
};

const { LocaleProvider, preloadLocale, useI18n } = await import("../lib/i18n");
const { ToastProvider } = await import("../lib/toast");
await preloadLocale("zh");
await import("../lib/sessionRecoveryRuntime");
const { SessionRecoveryVersionsHost } = await import("../components/SessionRecoveryVersionsHost");

let setLocale: ReturnType<typeof useI18n>["setPref"] | undefined;
function LocaleControl() {
  setLocale = useI18n().setPref;
  return null;
}

function emit(name: string, payload: SessionRecoveryEvent | ProjectTreeChangedV2) {
  for (const handler of handlers.get(name) ?? []) handler(payload);
}

async function flushMicrotasks() {
  await Promise.resolve();
  await Promise.resolve();
}

let recovered = 0;
const root = createRoot(document.getElementById("root")!);
await act(async () => {
  root.render(
    <LocaleProvider>
      <ToastProvider>
        <LocaleControl />
        <SessionRecoveryVersionsHost
          sessions={[]}
          onResumeSession={async () => {}}
          onRecoveryCreated={() => { recovered += 1; }}
          onLineageChanged={() => {}}
        />
      </ToastProvider>
    </LocaleProvider>,
  );
  await flushMicrotasks();
});

if ((handlers.get("session:recovered")?.size ?? 0) !== 1 || (handlers.get("project-tree:changed-v2")?.size ?? 0) !== 1) {
  throw new Error("recovery runtime did not install exactly one subscription pair");
}

await act(async () => {
  emit("session:recovered", {
    recoveryPath: "/sessions/fork.jsonl",
    scope: "global",
    topicId: "topic",
    recoveryParentId: "root",
    recoveryReason: "snapshot_conflict",
  });
  await flushMicrotasks();
});
if (recovered !== 1 || lineageReads.length !== 0) throw new Error("recovery event was not held pending for catalog classification");

await act(async () => {
  setLocale?.("zh");
  await flushMicrotasks();
});
if ((handlers.get("session:recovered")?.size ?? 0) !== 1 || (handlers.get("project-tree:changed-v2")?.size ?? 0) !== 1) {
  throw new Error("locale change restarted the recovery runtime and lost pending state");
}

await act(async () => {
  emit("project-tree:changed-v2", { revision: 2, roots: [""], reason: "reconcile" });
  await flushMicrotasks();
});
if (lineageReads.length !== 1 || lineageReads[0].recordClassification !== true) {
  throw new Error(`catalog classification was not persisted by the backend read: ${JSON.stringify(lineageReads)}`);
}
if (document.querySelectorAll(".toast").length !== 1) throw new Error("confirmed divergence did not produce exactly one toast after locale change");

await act(async () => {
  emit("project-tree:changed-v2", { revision: 3, roots: [""], reason: "duplicate" });
  await flushMicrotasks();
});
if (lineageReads.length !== 1 || document.querySelectorAll(".toast").length !== 1) {
  throw new Error("resolved recovery event was processed more than once");
}

await act(async () => {
  (document.querySelector(".toast") as HTMLElement).click();
  root.unmount();
});
dom.window.close();
console.log("  PASS  session recovery host preserves pending classification across locale changes");
