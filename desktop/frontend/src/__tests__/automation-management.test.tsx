import assert from "node:assert/strict";
import { managementDom } from "../test-support/managementDom";
import type { HeartbeatTask } from "../custom/features/heartbeat/heartbeat.types";
const dom = managementDom();
const { default: React, act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { LocaleProvider } = await import("../lib/i18n");
const { HeartbeatView } = await import("../custom/features/heartbeat/HeartbeatPanel");
const { useAutomationDraftStore: store } = await import("../store/automationDrafts");
let tasks: HeartbeatTask[] = [
  { id: "a", title: "Task A", prompt: "Saved", interval: "1h", enabled: true, createdAt: 1 },
  { id: "b", title: "Task B", prompt: "Saved B", interval: "2h", enabled: false, createdAt: 2 },
];
let release: (() => void) | undefined;
let failSave = false;
Object.assign(window, { go: { main: { App: {
  async HeartbeatReloadConfig() { return { revision: 1, etag: "a", tasks }; },
  async HeartbeatSaveConfig(value: { tasks: HeartbeatTask[] }) {
    if (failSave) throw new Error("failed");
    await new Promise<void>((resolve) => { release = resolve; });
    tasks = value.tasks; return { revision: 2, etag: "b", tasks };
  },
  async ListWorkspaces() { return []; }, async HeartbeatGenerateID() { return "draft-new"; },
} } } });
const root = createRoot(document.getElementById("root")!);
const render = (active = true) => <LocaleProvider><HeartbeatView active={active} /></LocaleProvider>;
const button = (text: string) => Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((node) => node.textContent?.trim() === text)!;
await act(async () => root.render(render()));
await act(async () => button("Task A").click());
await act(async () => store.getState().edit("a", (draft) => ({ ...draft, title: "Edited A", prompt: "Keep this draft" })));
await act(async () => button("Task B").click());
await act(async () => button("Task A · Unsaved").click());
assert.equal(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value, "Edited A");
await act(async () => root.render(render(false)));
await act(async () => root.render(render()));
assert.equal(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value, "Edited A");
await act(async () => button("Paused").click());
assert.equal(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value, "Edited A", "filters do not discard the active editor");
await act(async () => button("All").click());
await act(async () => button("Save").click());
assert.equal(store.getState().entries.a.busy, true);
await act(async () => button("Task B").click());
await act(async () => release?.());
assert.equal(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value, "Task B", "save completion must not steal selection");
assert.equal(store.getState().entries.a.draft.title, "Edited A");
assert.equal(store.getState().entries.a.busy, false);
await act(async () => button("Edited A").click());
assert.equal(button("Save").disabled, true, "saved baseline immediately clears dirty footer state");
assert.ok(document.querySelector(".automation-save-status")?.textContent?.includes("Saved"));
await act(async () => store.getState().edit("a", (draft) => ({ ...draft, prompt: "Survives failed save" })));
failSave = true;
await act(async () => button("Save").click());
assert.equal(store.getState().entries.a.draft.prompt, "Survives failed save");
assert.equal(store.getState().entries.a.error, true);
await act(async () => root.unmount());
assert.equal(store.getState().entries.a.draft.prompt, "Survives failed save", "component unmount does not own draft lifetime");
dom.window.close();
console.log("PASS automation page/filter retention, save-switch race, failure and unmount draft retention");
