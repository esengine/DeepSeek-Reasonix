import assert from "node:assert/strict";
import { act } from "react";
import { installDom, installBridgeApp, renderComposer } from "./composerInboxHarness";
import type { EffortInfo } from "../lib/types";

const flush = () => new Promise<void>(resolve => setTimeout(resolve, 0));
async function waitFor(check: () => boolean, label: string) {
  for (let i = 0; i < 60 && !check(); i++) await act(async () => { await flush(); });
  assert.ok(check(), label);
}
async function click(selector: string) {
  const target = document.querySelector<HTMLButtonElement>(selector);
  assert.ok(target, selector);
  await act(async () => { target.click(); await flush(); });
}

const dom = installDom();
Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", { configurable: true, value: () => {} });
const emptyInbox = { revision: 1, paused: false, recovered: false, items: [], itemsCount: 0, bytes: 0, maxItems: 64, maxBytes: 65536 };
const selected: string[] = [];
const submitted: string[] = [];
let releaseImage!: (path: string) => void;
let savingImage = false;
installBridgeApp({
  InboxSnapshot: async () => emptyInbox,
  ListSessions: async () => [{ path: "/sessions/reference.jsonl", title: "Reference session", current: false }],
  PreviewSession: async () => [{ role: "user", content: "Earlier requirements" }],
  SavePastedFile: async (name: string) => ".reasonix/attachments/" + name,
  SavePastedImage: async () => {
    savingImage = true;
    return new Promise<string>(resolve => { releaseImage = resolve; });
  },
  AttachmentDataURL: async () => "data:image/png;base64,aGVsbG8=",
  EnqueueInboxSteer: async (_tab: string, _display: string, submit: string) => {
    submitted.push(submit);
    return { itemId: "queued-" + submitted.length, disposition: "queued_followup", position: 1, paused: false };
  },
});
const effort: EffortInfo = { supported: true, current: "auto", default: "high", levels: ["auto", "high", "max"], canDefer: true };
const { root, rerender } = await renderComposer({ running: true, effort, onSetEffort: level => selected.push(level) });
try {
  const plus = document.querySelector<HTMLButtonElement>(".composer-content-trigger")!;
  assert.equal(plus.disabled, false);
  await click(".composer-content-trigger");
  assert.ok(document.querySelector(".composer-content-menu"));
  const taskModes = [...document.querySelectorAll<HTMLButtonElement>(".composer-intent-menu__item")];
  assert.ok(taskModes.length >= 2 && taskModes.every(item => item.disabled), "task modes retain their own busy guard");

  await click(".composer-content-menu__item");
  const fileInput = document.querySelector<HTMLInputElement>(".composer-content-file-input")!;
  assert.equal(fileInput.disabled, false);
  await act(async () => {
    Object.defineProperty(fileInput, "files", { configurable: true, value: [new File(["notes"], "notes.txt", { type: "text/plain" })] });
    fileInput.dispatchEvent(new Event("change", { bubbles: true }));
    await flush();
  });
  await waitFor(() => document.body.textContent?.includes("notes.txt") === true, "file picker adds a file while running");

  await click(".composer-content-trigger");
  const referenceItem = document.querySelectorAll<HTMLButtonElement>(".composer-content-menu__item")[2];
  await act(async () => { referenceItem.click(); await flush(); });
  await waitFor(() => document.querySelector(".slashmenu__search") !== null, "history picker opens while running");
  const reference = [...document.querySelectorAll<HTMLButtonElement>(".slashmenu button")].find(item => item.textContent?.includes("Reference session"));
  assert.ok(reference);
  await act(async () => {
    reference.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true }));
    await flush();
  });
  assert.ok(document.querySelector(".composer-context__item--session"), "history reference is attached");

  // Image save remains asynchronous: a fast Enter/button click cannot enqueue
  // an incomplete reference. Releasing the save makes the same draft sendable.
  const textarea = document.querySelector<HTMLTextAreaElement>("textarea")!;
  await act(async () => {
    const paste = new Event("paste", { bubbles: true, cancelable: true });
    Object.defineProperty(paste, "clipboardData", { value: {
      files: [new File(["image"], "screen.png", { type: "image/png" })],
      items: [], types: ["Files"], getData: () => "",
    } });
    textarea.dispatchEvent(paste);
    await flush();
  });
  await waitFor(() => savingImage, "pasted image starts saving during a run");
  assert.equal(document.querySelector<HTMLButtonElement>(".composer__btn--send")?.disabled, true);
  await act(async () => {
    textarea.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    document.querySelector<HTMLButtonElement>(".composer__btn--send")?.click();
  });
  assert.equal(submitted.length, 0, "unfinished attachment is never submitted");
  await act(async () => { releaseImage(".reasonix/attachments/screen.png"); await flush(); });
  await waitFor(() => document.querySelector<HTMLButtonElement>(".composer__btn--send")?.disabled === false, "save completion enables enqueue");
  await click(".composer__btn--send");
  await waitFor(() => submitted.length === 1, "running content is durably queued");
  assert.match(submitted[0], /@\.reasonix\/attachments\/notes\.txt/);
  assert.match(submitted[0], /@\.reasonix\/attachments\/screen\.png/);
  assert.match(submitted[0], /Earlier requirements/);

  await click(".composer-effort-control button");
  assert.doesNotMatch(document.querySelector(".composer-access-menu")?.textContent ?? "", /Currently applied|Next turn/i, "existing menu presentation stays unchanged");
  await click('[role="menuitemradio"][data-value="max"]');
  assert.deepEqual(selected, ["max"]);
  assert.match(document.querySelector(".composer-effort-control")?.textContent ?? "", /auto/i, "no optimistic selection before acknowledgement");
  assert.equal(document.querySelector<HTMLButtonElement>(".composer-effort-control button")?.getAttribute("title"), null, "applied effort carries no next-turn hint");
  await rerender({ effort: { ...effort, pending: "max" } });
  assert.equal(document.querySelector(".composer-effort-control")?.textContent, "max", "the existing label displays the acknowledged choice without a new badge");
  assert.equal(document.querySelector<HTMLButtonElement>(".composer-effort-control button")?.getAttribute("title"), "Selected for the next task turn", "pending effort explains it applies to the next task turn");
  await click(".composer-effort-control button");
  await click('[role="menuitemradio"][data-value="auto"]');
  assert.deepEqual(selected, ["max", "auto"], "selecting current value cancels a pending selection");

  await rerender({ running: false });
  assert.equal(document.querySelector(".composer-effort-control")?.textContent, "max", "stopping does not clear the backend selection");
  await rerender({ effort: { ...effort, current: "max" } });
  assert.doesNotMatch(document.querySelector(".composer-effort-control")?.textContent ?? "", /Next turn/);
  assert.equal(document.querySelector<HTMLButtonElement>(".composer-effort-control button")?.getAttribute("title"), null, "applied effort drops the next-turn hint");

  await rerender({ running: true, effort, remoteSession: true, attachmentInputEnabled: false });
  assert.equal(plus.disabled, true, "remote running content menu keeps its existing guard");
  const remoteEffort = document.querySelector<HTMLButtonElement>(".composer-effort-control button")!;
  assert.equal(remoteEffort.disabled, true, "remote sessions cannot defer even if an unexpected capability arrives");
  const before = selected.length;
  await act(async () => { remoteEffort.click(); });
  assert.equal(selected.length, before);

  await rerender({ remoteSession: false, effort: { ...effort, canDefer: undefined } });
  assert.equal(document.querySelector<HTMLButtonElement>(".composer-effort-control button")?.disabled, true, "older backend retains its guard");
  await rerender({ readOnly: true, attachmentInputEnabled: true });
  assert.equal(plus.disabled, true, "read-only mode still blocks the menu");
  assert.equal(fileInput.disabled, true, "read-only mode still blocks file selection");
  console.log("PASS running composer: attachments, history, save barrier, enqueue, effort acknowledgement, cancel, remote and legacy guards");
} finally {
  await act(async () => { root.unmount(); });
  dom.window.close();
}
