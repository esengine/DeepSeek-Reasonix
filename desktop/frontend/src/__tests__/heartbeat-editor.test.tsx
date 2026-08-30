// Run: node --import ./scripts/css-stub-register.mjs --import tsx src/__tests__/heartbeat-editor.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { HeartbeatView, TaskEditor } from "../custom/features/heartbeat/HeartbeatPanel";
import type { HeartbeatTask } from "../custom/features/heartbeat/heartbeat.types";
import { LocaleProvider } from "../lib/i18n";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function button(label: string): HTMLButtonElement | undefined {
  return Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((item) => item.textContent?.trim() === label);
}

class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

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
globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.ResizeObserver = NoopResizeObserver as unknown as typeof ResizeObserver;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

let nextID = 0;
let savedUpdate: { tasks?: HeartbeatTask[] } | null = null;
let backendTasks: HeartbeatTask[] = [];
let saveShouldFail = false;
Object.assign(window, {
  go: {
    main: {
      App: {
        async HeartbeatReloadConfig() { return { revision: 1, etag: "test", tasks: backendTasks }; },
        async HeartbeatSaveConfig(update: { tasks?: HeartbeatTask[] }) {
          if (saveShouldFail) throw new Error("conflict");
          savedUpdate = update;
          backendTasks = update.tasks ?? [];
          return { revision: 2, etag: "saved", tasks: backendTasks };
        },
        async HeartbeatTriggerNow() {},
        async HeartbeatGenerateID() { nextID += 1; return `draft-${nextID}`; },
        async ListWorkspaces() { return [{ name: "Project One", path: "/project-one", current: true }]; },
      },
    },
  },
});

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
const noopDelete = async () => true;

function renderEditor(task: HeartbeatTask, onSave: (task: HeartbeatTask) => Promise<boolean>, key: string) {
  root.render(
    <LocaleProvider>
      <TaskEditor
        key={key}
        task={task}
        onSave={onSave}
        onDelete={noopDelete}
        onCloseDetail={() => {}}
      />
    </LocaleProvider>,
  );
}

console.log("\nheartbeat editor state ownership");

const originalTask: HeartbeatTask = {
  id: "existing",
  title: "Saved title",
  prompt: "Saved prompt",
  interval: "30m",
  enabled: true,
  createdAt: 1,
  topicId: "old-topic",
  lastRunAt: 100,
};
let submitted: HeartbeatTask | null = null;
await act(async () => {
  renderEditor(originalTask, async (task) => { submitted = task; return true; }, "run-state");
  await flush();
});
await act(async () => {
  button("Daily")?.click();
  await flush();
});
await act(async () => {
  renderEditor({
    ...originalTask,
    topicId: "fresh-topic",
    lastRunAt: 200,
    runHistory: [{ at: 200, topicId: "fresh-topic" }],
  }, async (task) => { submitted = task; return true; }, "run-state");
  await flush();
});
await act(async () => {
  button("Save")?.click();
  await flush();
});
ok(submitted?.interval.startsWith("24h|daily") === true, "trigger completion preserves the user's edited schedule");
ok(submitted?.topicId === "fresh-topic" && submitted.lastRunAt === 200, "save carries the latest engine-owned run state");
ok(submitted?.runHistory?.[0]?.topicId === "fresh-topic", "save carries run history added while the editor was open");

console.log("\nheartbeat editor failed save");

let resolveSave: ((saved: boolean) => void) | undefined;
const rejectedSave = () => new Promise<boolean>((resolve) => { resolveSave = resolve; });
await act(async () => {
  renderEditor({ ...originalTask, id: "conflict" }, rejectedSave, "conflict");
  await flush();
});
await act(async () => {
  button("Weekly")?.click();
  await flush();
});
await act(async () => {
  button("Save")?.click();
  await flush();
});
ok(button("Save")?.disabled === true, "save stays pending until persistence resolves");
await act(async () => {
  resolveSave?.(false);
  await flush();
});
ok(button("Weekly")?.classList.contains("set-seg__btn--on") === true, "failed save preserves the local draft");
ok(document.querySelector('[role="alert"]')?.textContent?.includes("Your draft is still here") === true, "failed save reports an actionable error");
ok(button("Save") != null, "failed save remains dirty and retryable");

console.log("\nheartbeat editor frequency conversion");

await act(async () => {
  renderEditor({ ...originalTask, id: "weekly-cron", interval: "0 9 * * 1" }, async () => true, "weekly-cron");
  await flush();
});
await act(async () => {
  button("Interval")?.click();
  await flush();
});
ok(button("Custom")?.classList.contains("set-seg__btn--on") === true, "lossy cron conversion keeps the Custom frequency selected");
ok(document.querySelector<HTMLInputElement>('.heartbeat-editor__freq-input--cron')?.value === "0 9 * * 1", "lossy conversion keeps the original cron expression");
ok(document.querySelector('.heartbeat-editor__inline-error')?.textContent?.includes("cannot be converted") === true, "lossy conversion explains why the editor did not switch");

console.log("\nheartbeat recommendation draft");

await act(async () => {
  root.render(<LocaleProvider><HeartbeatView /></LocaleProvider>);
  await flush();
  await flush();
});
await act(async () => {
  document.querySelector<HTMLButtonElement>(".heartbeat-suggestion")?.click();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value === "Daily review", "recommendation keeps its prefilled editor open");
ok(document.body.textContent?.includes("Select a task to view details") !== true, "recommendation is not cleared by the missing-task cleanup effect");
ok(button("Ask")?.classList.contains("set-seg__btn--on") === true, "recommendation defaults to ask approval");
ok(button("Global") != null, "recommendation remains a new draft with editable scope");
await act(async () => {
  button("Save")?.click();
  await flush();
});
ok(savedUpdate?.tasks?.[0]?.enabled === false, "recommendation stays disabled until the user explicitly enables it");
ok(savedUpdate?.tasks?.[0]?.approvalMode === "ask", "recommendation persists the safe ask approval default");

saveShouldFail = true;
await act(async () => {
  const productSuggestion = Array.from(document.querySelectorAll<HTMLButtonElement>(".heartbeat-suggestion"))
    .find((item) => item.textContent?.includes("Product update"));
  productSuggestion?.click();
  await flush();
});
await act(async () => {
  button("Save")?.click();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value === "Product update digest", "parent save conflict keeps the unsaved recommendation draft open");
ok(document.querySelector('.heartbeat-editor__save-error')?.textContent?.includes("Your draft is still here") === true, "parent save conflict is reported instead of marking the draft clean");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
