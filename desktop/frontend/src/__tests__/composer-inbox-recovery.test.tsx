// Run: tsx src/__tests__/composer-inbox-recovery.test.tsx

import { act } from "react";
import { installDom, installBridgeApp, renderComposer } from "./composerInboxHarness";
import { runtimeStateStore, type RuntimeProjection } from "../lib/runtimeStateStore";
import { acceptRuntimeState } from "../lib/runtimeStateReducer";

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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, check: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    if (check()) return;
    await act(async () => { await flushTimers(); });
  }
  ok(false, label);
}

function recoveredSnapshot(count = 3) {
  const items = Array.from({ length: count }, (_, index) => ({
    id: `recovered-${index + 1}`,
    intent: "followup",
    state: "uncertain",
    preview: `Recovered instruction ${index + 1}`,
    byteSize: 128,
    position: index + 1,
  }));
  return {
    revision: 1,
    paused: true,
    recovered: true,
    recoveredCount: count,
    items,
    itemsCount: items.length,
    bytes: items.length * 128,
    maxItems: 64,
    maxBytes: 64 * 1024 * 1024,
  };
}

console.log("\ncomposer inbox recovery");

{
  const dom = installDom();
  let snapshot = recoveredSnapshot();
  const pauseCalls: Array<{ tabId: string; paused: boolean }> = [];
  installBridgeApp({
    InboxSnapshot: async () => snapshot,
    SetInboxPaused: async (tabId: string, paused: boolean) => {
      pauseCalls.push({ tabId, paused });
      if (!paused) snapshot = { ...snapshot, paused: false, recovered: false };
    },
  });
  const { root } = await renderComposer();

  await waitFor("recovery banner rendered", () => document.querySelector(".composer-inbox-recovery") !== null);
  ok(document.querySelector(".composer-inbox-recovery")?.textContent?.includes("Recovered 3 pending instructions") === true, "banner reports recovered count");
  ok(document.querySelectorAll(".composer-guidance-item").length === 2, "queue starts with bounded preview");

  const review = document.querySelector(".composer-inbox-recovery .btn") as HTMLButtonElement;
  await act(async () => { review.click(); await flushTimers(); });
  ok(document.querySelectorAll(".composer-guidance-item").length === 3, "review action expands the recovered queue");

  const buttons = Array.from(document.querySelectorAll(".composer-inbox-recovery .btn")) as HTMLButtonElement[];
  await act(async () => { buttons[2].click(); await flushTimers(); });
  ok(pauseCalls.at(-1)?.paused === true, "keep paused confirms the server-side pause");
  ok(document.querySelector(".composer-inbox-recovery") !== null, "keep paused preserves the resume path");
  ok(buttons[2].textContent === "Paused" && buttons[2].disabled, "confirmed pause is visible and idempotent");

  await act(async () => { buttons[1].click(); await flushTimers(); });
  ok(pauseCalls.at(-1)?.paused === false, "continue resumes the durable inbox");
  await waitFor("recovery banner cleared after resume", () => document.querySelector(".composer-inbox-recovery") === null);
  ok(document.querySelector(".composer-inbox-recovery") === null, "successful resume clears the recovery banner");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  let resolveTabA!: (value: ReturnType<typeof recoveredSnapshot>) => void;
  const tabAPromise = new Promise<ReturnType<typeof recoveredSnapshot>>((resolve) => { resolveTabA = resolve; });
  installBridgeApp({
    InboxSnapshot: async (tabId: string) => tabId === "tab-a"
      ? tabAPromise
      : { ...recoveredSnapshot(0), paused: false, recovered: false },
    SetInboxPaused: async () => {},
  });
  const { root, rerender } = await renderComposer();
  await rerender({ tabId: "tab-b", sessionKey: "session-b" });
  resolveTabA(recoveredSnapshot(2));
  await act(async () => { await flushTimers(); });
  ok(document.querySelector(".composer-inbox-recovery") === null, "stale snapshot cannot show recovery controls on another session");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom("zh-CN");
  const snapshot = {
    revision: 3,
    paused: false,
    recovered: false,
    recoveredCount: 0,
    items: [{
      id: "queued-before-pause",
      intent: "followup",
      state: "queued",
      preview: "引导当前回合",
      byteSize: 64,
      position: 1,
    }],
    itemsCount: 1,
    bytes: 64,
    maxItems: 64,
    maxBytes: 64 * 1024 * 1024,
  };
  installBridgeApp({
    InboxSnapshot: async () => snapshot,
    SteerInboxItem: async () => { throw new Error("reasonix_error:inbox_paused"); },
  });
  const { root } = await renderComposer({ running: true });

  await waitFor("queued guidance rendered for localization check", () => document.querySelector(".composer-guidance-item__guide") !== null);
  const guide = document.querySelector(".composer-guidance-item__guide") as HTMLButtonElement;
  await act(async () => { guide.click(); await flushTimers(); });
  await waitFor("localized paused toast rendered", () => document.querySelector(".toast__text")?.textContent === "收件箱已暂停");
  ok(document.querySelector(".toast__text")?.textContent === "收件箱已暂停", "coded paused error renders in the active Chinese locale");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  let snapshot = {
    revision: 2,
    paused: true,
    recovered: false,
    recoveredCount: 0,
    items: [{
      id: "paused-queued-1",
      intent: "followup",
      state: "queued",
      preview: "Guide the active turn",
      byteSize: 64,
      position: 1,
    }],
    itemsCount: 1,
    bytes: 64,
    maxItems: 64,
    maxBytes: 64 * 1024 * 1024,
  };
  const pauseCalls: boolean[] = [];
  installBridgeApp({
    InboxSnapshot: async () => snapshot,
    SetInboxPaused: async (_tabId: string, paused: boolean) => {
      pauseCalls.push(paused);
      if (!paused) snapshot = { ...snapshot, paused: false };
    },
  });
  const { root } = await renderComposer({ running: true }, true);

  await waitFor("ordinary pause banner rendered", () => document.querySelector(".composer-inbox-recovery") !== null);
  const pauseBanner = document.querySelector(".composer-inbox-recovery");
  ok(pauseBanner?.textContent?.includes("Inbox is paused") === true, "non-recovered pause exposes an actionable pause banner");
  const guide = document.querySelector(".composer-guidance-item__guide") as HTMLButtonElement | null;
  ok(guide?.disabled === true, "paused inbox disables guide admission instead of surfacing a backend error");

  const buttons = Array.from(document.querySelectorAll(".composer-inbox-recovery .btn")) as HTMLButtonElement[];
  if (buttons[1]) {
    await act(async () => { buttons[1].click(); await flushTimers(); });
    ok(pauseCalls.at(-1) === false, "continue resumes a non-recovered paused inbox");
    await waitFor("ordinary pause banner cleared after resume", () => document.querySelector(".composer-inbox-recovery") === null);
  }

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  let snapshot = {
    revision: 4,
    paused: false,
    recovered: false,
    recoveredCount: 0,
    items: [
      {
        id: "idle-queued-1",
        intent: "followup",
        state: "queued",
        preview: "Send the next instruction",
        byteSize: 64,
        position: 1,
      },
      {
        id: "idle-queued-2",
        intent: "followup",
        state: "queued",
        preview: "Wait behind the first instruction",
        byteSize: 64,
        position: 2,
      },
    ],
    itemsCount: 2,
    bytes: 128,
    maxItems: 64,
    maxBytes: 64 * 1024 * 1024,
  };
  const pauseCalls: Array<{ tabId: string; paused: boolean }> = [];
  let steerCalls = 0;
  installBridgeApp({
    InboxSnapshot: async () => snapshot,
    SetInboxPaused: async (tabId: string, paused: boolean) => {
      pauseCalls.push({ tabId, paused });
      snapshot = { ...snapshot, items: [], itemsCount: 0, bytes: 0 };
    },
    SteerInboxItem: async () => { steerCalls += 1; },
  });
  const { root } = await renderComposer({ running: false });

  await waitFor("idle durable guidance rendered", () => document.querySelectorAll(".composer-guidance-item__guide").length === 2);
  const guides = Array.from(document.querySelectorAll(".composer-guidance-item__guide")) as HTMLButtonElement[];
  ok(!guides[0].disabled && guides[0].textContent === "Send", "idle FIFO head exposes an explicit send fallback");
  ok(guides[0].getAttribute("aria-label") === "Send this guidance to the transcript", "idle send fallback has an accessible label");
  ok(guides[1].disabled, "idle non-head guidance remains disabled to preserve FIFO order");
  ok(guides[1].getAttribute("aria-label") === "Waiting for earlier queued guidance", "idle non-head guidance explains the FIFO gate");
  await act(async () => { guides[0].click(); await flushTimers(); });
  ok(pauseCalls.length === 1 && pauseCalls[0].tabId === "tab-a" && pauseCalls[0].paused === false, "idle send re-kicks the Controller drain without changing pause state");
  ok(steerCalls === 0, "idle send never attempts active-turn steering");
  await waitFor("idle durable guidance clears after dispatch kick", () => document.querySelector(".composer-guidance-item") === null);

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  let steerCalls = 0;
  let deleteCalls = 0;
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 4,
      paused: false,
      recovered: false,
      recoveredCount: 0,
      items: [{
        id: "active-steer",
        intent: "steer",
        state: "steer_accepted",
        preview: "Already admitted guidance",
        byteSize: 64,
        position: 1,
      }],
      itemsCount: 1,
      bytes: 64,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
    SteerInboxItem: async () => { steerCalls += 1; },
    DeleteInboxItem: async () => { deleteCalls += 1; },
  });
  const { root } = await renderComposer({ running: true });
  await waitFor("active steer rendered", () => document.querySelector(".composer-guidance-item__guide") !== null);
  const guide = document.querySelector(".composer-guidance-item__guide") as HTMLButtonElement;
  const actions = document.querySelectorAll(".composer-guidance-item__action");
  const dismiss = actions[actions.length - 1] as HTMLButtonElement;
  ok(guide.disabled && dismiss.disabled, "active steer disables send and delete actions");
  ok(guide.getAttribute("aria-label") === "Guidance is already being applied", "active steer explains why actions are disabled");
  await act(async () => { guide.click(); dismiss.click(); await flushTimers(); });
  ok(steerCalls === 0 && deleteCalls === 0, "active steer never sends invalid backend operations");

  await act(async () => { root.unmount(); });
  dom.window.close();
}
{
  const dom = installDom();
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 5,
      paused: false,
      recovered: false,
      recoveredCount: 0,
      items: [
        {
          id: "consumed-steer",
          intent: "steer",
          state: "steer_consumed",
          preview: "Already shown in the transcript",
          byteSize: 64,
          position: 1,
        },
        {
          id: "still-queued",
          intent: "followup",
          state: "queued",
          preview: "Still waiting to run",
          byteSize: 64,
          position: 2,
        },
      ],
      itemsCount: 2,
      bytes: 128,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
  });
  const { root } = await renderComposer({ running: true });

  await waitFor("pending guidance rendered without consumed steer", () => document.querySelectorAll(".composer-guidance-item").length === 1);
  ok(document.querySelector(".composer-guidance-item")?.textContent?.includes("Still waiting to run") === true, "consumed steer is excluded from the pending guidance shelf");
  ok(document.body.textContent?.includes("Already shown in the transcript") === false, "consumed steer cannot reappear after inbox refresh");

  await act(async () => { root.unmount(); });
  dom.window.close();
}
{
  const dom = installDom();
  let retryCalls = 0;
  let steerCalls = 0;
  const snapshot = {
    revision: 5,
    paused: false,
    recovered: true,
    recoveredCount: 1,
    items: [{
      id: "uncertain-guidance",
      intent: "followup",
      state: "uncertain",
      preview: "Retry recovered guidance",
      byteSize: 64,
      position: 1,
    }],
    itemsCount: 1,
    bytes: 64,
    maxItems: 64,
    maxBytes: 64 * 1024 * 1024,
  };
  installBridgeApp({
    InboxSnapshot: async () => snapshot,
    RetryInboxItem: async () => { retryCalls += 1; },
    SteerInboxItem: async () => { steerCalls += 1; },
  });
  const { root } = await renderComposer({ running: false });

  await waitFor("uncertain guidance rendered", () => document.querySelector(".composer-guidance-item__guide") !== null);
  const retry = document.querySelector(".composer-guidance-item__guide") as HTMLButtonElement;
  ok(!retry.disabled && retry.textContent === "Retry", "uncertain idle guidance exposes an explicit retry action");
  ok(retry.getAttribute("aria-label") === "Retry this guidance", "retry action has a state-aware accessible label");
  await act(async () => { retry.click(); await flushTimers(); });
  ok(retryCalls === 1 && steerCalls === 0, "idle retry requeues through the Controller without an invalid steer");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  let retryCalls = 0;
  let steerCalls = 0;
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 6,
      paused: false,
      recovered: false,
      recoveredCount: 0,
      items: [{
        id: "blocked-guidance",
        intent: "steer",
        state: "blocked",
        preview: "Retry blocked guidance",
        byteSize: 64,
        position: 1,
      }],
      itemsCount: 1,
      bytes: 64,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
    RetryInboxItem: async () => { retryCalls += 1; },
    SteerInboxItem: async () => {
      steerCalls += 1;
      return { disposition: "queued_followup" };
    },
  });
  const { root } = await renderComposer({ running: true });

  await waitFor("blocked guidance rendered", () => document.querySelector(".composer-guidance-item__guide") !== null);
  const retry = document.querySelector(".composer-guidance-item__guide") as HTMLButtonElement;
  await act(async () => { retry.click(); await flushTimers(); });
  ok(retryCalls === 1 && steerCalls === 1, "busy retry requeues before attempting active-turn admission");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 7,
      paused: true,
      recovered: false,
      recoveredCount: 0,
      items: [{
        id: "processing-guidance",
        intent: "steer",
        state: "steer_accepted",
        preview: "Already being applied",
        byteSize: 64,
        position: 1,
      }],
      itemsCount: 1,
      bytes: 64,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
  });
  const { root } = await renderComposer({ running: true });
  await waitFor("in-flight paused item rendered", () => document.querySelector(".composer-guidance-item") !== null);
  ok(document.querySelector(".composer-inbox-recovery") === null, "pause banner hides while guidance is already being applied");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  let updated = "";
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 8,
      paused: false,
      recovered: false,
      recoveredCount: 0,
      items: [{
        id: "editable-guidance",
        intent: "followup",
        state: "queued",
        preview: "Original queued text",
        byteSize: 64,
        position: 1,
      }],
      itemsCount: 1,
      bytes: 64,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
    UpdateInboxItem: async (_tabId: string, _id: string, display: string) => {
      updated = display;
    },
  });
  const { root } = await renderComposer({ running: false });
  await waitFor("editable guidance rendered", () => document.querySelector("[aria-label=\"Edit this queued guidance\"]") !== null);
  const edit = document.querySelector("[aria-label=\"Edit this queued guidance\"]") as HTMLButtonElement;
  await act(async () => { edit.click(); await flushTimers(); });
  const editor = document.querySelector(".composer-guidance-item__editor") as HTMLInputElement;
  ok(editor?.value === "Original queued text", "edit mode loads the queued text");
  const save = document.querySelector("[aria-label=\"Save guidance edits\"]") as HTMLButtonElement;
  await act(async () => { save.click(); await flushTimers(); });
  ok(updated === "Original queued text", "edit saves through UpdateInboxItem");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 9,
      paused: true,
      recovered: false,
      recoveredCount: 0,
      items: [{
        id: "empty-preview",
        intent: "followup",
        state: "queued",
        preview: "",
        byteSize: 64,
        position: 1,
      }],
      itemsCount: 1,
      bytes: 64,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
    ReadInboxItem: async () => ({ id: "empty-preview", displayText: "Loaded body", rawText: "Loaded body", submitText: "Loaded body" }),
  });
  const { root } = await renderComposer({ running: false });
  await waitFor("empty preview hydrated", () => document.querySelector(".composer-guidance-item__text")?.textContent === "Loaded body");
  ok(document.querySelector(".composer-guidance-item__text")?.textContent === "Loaded body", "review path hydrates an empty snapshot preview");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  const snapshots: Record<string, ReturnType<typeof recoveredSnapshot>> = {
    "/sessions/a.jsonl": recoveredSnapshot(12),
    "/sessions/b.jsonl": { ...recoveredSnapshot(0), paused: false, recovered: false, recoveredCount: 0, items: [], itemsCount: 0, bytes: 0 },
  };
  installBridgeApp({
    InboxSnapshot: async () => {
      const path = currentPath;
      return { ...snapshots[path], sessionPath: path };
    },
  });
  let currentPath = "/sessions/a.jsonl";
  const { root, rerender } = await renderComposer({
    tabId: "tab-shared",
    sessionKey: "session-topic\0project\0/repo\0topic-a",
    inboxSessionPath: currentPath,
  });
  await waitFor("session A recovered queue rendered", () => document.querySelectorAll(".composer-guidance-item").length === 2);
  ok(document.querySelector(".composer-inbox-recovery")?.textContent?.includes("Recovered 12 pending instructions") === true, "session A shows its own recovered inbox");

  currentPath = "/sessions/b.jsonl";
  await rerender({ inboxSessionPath: currentPath });
  await waitFor("session B does not inherit session A inbox", () => document.querySelector(".composer-guidance-item") === null);
  ok(document.querySelector(".composer-inbox-recovery") === null, "sibling session does not inherit the previous recovered banner");
  ok(document.querySelector(".composer-guidance-item") === null, "sibling session does not inherit the previous queued guidance");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  let resolveStale!: (value: ReturnType<typeof recoveredSnapshot> & { sessionPath: string }) => void;
  const stalePromise = new Promise<ReturnType<typeof recoveredSnapshot> & { sessionPath: string }>((resolve) => { resolveStale = resolve; });
  installBridgeApp({
    InboxSnapshot: async (_tabId: string) => currentPath === "/sessions/a.jsonl"
      ? stalePromise
      : { ...recoveredSnapshot(0), paused: false, recovered: false, recoveredCount: 0, items: [], itemsCount: 0, bytes: 0, sessionPath: currentPath },
  });
  let currentPath = "/sessions/a.jsonl";
  const { root, rerender } = await renderComposer({
    tabId: "tab-shared",
    sessionKey: "session-topic\0project\0/repo\0topic-a",
    inboxSessionPath: currentPath,
  });
  currentPath = "/sessions/b.jsonl";
  await rerender({ inboxSessionPath: currentPath });
  resolveStale({ ...recoveredSnapshot(12), sessionPath: "/sessions/a.jsonl" });
  await act(async () => { await flushTimers(); });
  ok(document.querySelector(".composer-inbox-recovery") === null, "stale recovered snapshot cannot land on another session");
  ok(document.querySelector(".composer-guidance-item") === null, "stale queued items cannot land on another session");

  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  const longText = "x".repeat(240);
  let deletedID = "";
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 8,
      paused: false,
      recovered: false,
      items: [
        { id: "same-first", intent: "steer", state: "queued", preview: longText.slice(0, 120), source: "desktop", byteSize: 240, position: 1 },
        { id: "same-second", intent: "steer", state: "queued", preview: longText.slice(0, 120), source: "desktop", byteSize: 240, position: 2 },
      ],
      itemsCount: 2,
      bytes: 480,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
    DeleteInboxItem: async (_tabId: string, id: string) => { deletedID = id; },
  });
  const { root, rerender } = await renderComposer({ running: true });
  await waitFor("duplicate long guidance rendered", () => document.querySelectorAll(".composer-guidance-item").length === 2);
  await rerender({ guidanceConsumedKey: "steer-event-1", guidanceConsumedItemId: "same-second", guidanceConsumedText: longText });
  await waitFor("item id removes one duplicate", () => document.querySelectorAll(".composer-guidance-item").length === 1);
  const duplicateActions = document.querySelectorAll(".composer-guidance-item__action");
  const dismiss = duplicateActions[duplicateActions.length - 1] as HTMLButtonElement;
  await act(async () => { dismiss.click(); await flushTimers(); });
  ok(deletedID === "same-first", "consumed steer uses item ID for long duplicate text");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 9,
      paused: true,
      recovered: true,
      recoveredCount: 3,
      items: [
        { id: "already-running", intent: "followup", state: "running", preview: "running", byteSize: 32, position: 1 },
        { id: "already-consumed", intent: "steer", state: "steer_consumed", preview: "consumed", byteSize: 32, position: 2 },
        { id: "still-pending", intent: "followup", state: "uncertain", preview: "pending", byteSize: 32, position: 3 },
      ],
      itemsCount: 3,
      bytes: 96,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
  });
  const { root } = await renderComposer({ running: false });
  await waitFor("delivered states filtered", () => document.querySelectorAll(".composer-guidance-item").length === 1);
  ok(document.body.textContent?.includes("running") === false && document.body.textContent?.includes("consumed") === false, "running and consumed items stay out of the pending queue");
  ok(document.querySelector(".composer-inbox-recovery")?.textContent?.includes("Recovered 1 pending instruction") === true, "recovery count reflects visible pending items");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 10,
      paused: false,
      recovered: false,
      items: [
        { id: "withdrawn", intent: "followup", state: "queued", preview: "restore me", source: "desktop", byteSize: 32, position: 1 },
        { id: "delivered", intent: "steer", state: "queued", preview: "do not restore me", source: "desktop", byteSize: 32, position: 2 },
      ],
      itemsCount: 2,
      bytes: 64,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
  });
  const { root } = await renderComposer({
    running: true,
    onCancel: async () => ({ discardedItemIds: ["withdrawn"] }),
  });
  await waitFor("mixed cancel queue rendered", () => document.querySelectorAll(".composer-guidance-item").length === 2);
  const stop = document.querySelector(".composer__btn--stop") as HTMLButtonElement;
  await act(async () => { stop.click(); await flushTimers(); });
  const textarea = document.querySelector("textarea") as HTMLTextAreaElement;
  ok(textarea.value.includes("restore me") && !textarea.value.includes("do not restore me"), "stop restores only backend-confirmed withdrawn messages");
  ok(document.querySelector(".composer-guidance-item")?.textContent?.includes("do not restore me") === true, "non-withdrawn durable message remains backend-owned");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom();
  installBridgeApp({
    InboxSnapshot: async () => ({
      revision: 11,
      paused: false,
      recovered: false,
      items: [{ id: "readonly", intent: "followup", state: "queued", preview: "readonly item", source: "desktop", byteSize: 32, position: 1 }],
      itemsCount: 1,
      bytes: 32,
      maxItems: 64,
      maxBytes: 64 * 1024 * 1024,
    }),
  });
  const { root } = await renderComposer({ running: true, readOnly: true });
  await waitFor("readonly item rendered", () => document.querySelector(".composer-guidance-item__action") !== null);
  ok((document.querySelector(".composer-guidance-item__action") as HTMLButtonElement).disabled, "read-only queue disables delete");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

{
  const dom = installDom("zh-CN");
  const calls: { display: string; submit: string; id: string }[] = [];
  const lookups: string[] = [];
  let rejectEnqueue = true, cancelled = 0, direct = 0;
  installBridgeApp({
    InboxSnapshot: async () => ({ ...recoveredSnapshot(0), paused: false, recovered: false }),
    CaptureInboxTarget: async () => ({ tabId: "tab-a", sessionPath: "session-a", generation: 1, selection: 0, remote: false }),
    EnqueueInboxFollowupForTarget: async (_target: unknown, display: string, submit: string, _invocations: unknown, id: string) => {
      calls.push({ display, submit, id });
      if (rejectEnqueue) throw new Error("transport outcome unknown");
      return { itemId: "durable-followup", disposition: "queued_followup", position: 1, paused: false };
    },
    LookupInboxFollowupForTarget: async (_target: unknown, id: string) => {
      lookups.push(id);
      return { itemId: "durable-followup", disposition: "idempotent_hit", position: 0, paused: false };
    },
  });
  const snapshot: RuntimeProjection = { epoch: "finishing-test", revision: 1, topics: [], sessions: [{
    tabId: "tab-a", scope: "project", workspaceRoot: "/repo", topicId: "topic", sessionPath: "session-a", sessionGeneration: 1,
    open: true, remote: false, freshness: "synced", state: { schemaVersion: 1, runtimeEpoch: "controller-test", revision: 1,
      phase: "finishing", running: true, turnId: "turn", turnStatus: "completed", turnEventSeq: 3,
      pendingPrompt: false, cancelRequested: false, cancellable: false, backgroundJobs: 0, activity: "" },
  }] };
  acceptRuntimeState(runtimeStateStore, snapshot, true);
  const { root } = await renderComposer({ running: false, inboxSessionPath: "session-a", onSend: () => { direct++; }, onCancel: () => { cancelled++; } });
  ok(Boolean(document.querySelector(".composer-run-strip")?.textContent?.includes("正在收尾")), "finishing is visibly announced from shared state after TurnDone");
  ok(!document.querySelector(".composer-card--running") && !document.querySelector(".composer-run-strip__dot"), "finishing does not animate as model execution");
  ok(!document.querySelector(".composer__btn--stop"), "finishing hides current-turn stop");
  const input = document.querySelector("textarea.composer__input:not([aria-hidden=true])") as HTMLTextAreaElement;
  await act(async () => {
    const paste = new window.Event("paste", { bubbles: true, cancelable: true });
    Object.defineProperty(paste, "clipboardData", { value: { files: [], items: [], types: ["text/plain"], getData: () => "follow up after cleanup" } });
    input.dispatchEvent(paste);
    await flushTimers();
  });
  await act(async () => { (document.querySelector(".composer__btn--send") as HTMLButtonElement).click(); await flushTimers(); });
  ok(calls.length === 1 && input.value.includes("follow up"), "uncertain queue failure retains draft and makes one request");
  rejectEnqueue = false;
  await act(async () => { (document.querySelector(".composer__btn--send") as HTMLButtonElement).click(); await flushTimers(); });
  ok(calls.length === 1 && calls[0].id !== "" && calls[0].id === lookups[0], "explicit retry only looks up the original idempotency key");
  ok(calls[0]?.submit === "follow up after cleanup" && input.value === "", "durable queue receipt clears the complete submitted draft");
  ok(direct === 0 && cancelled === 0, "finishing neither directly submits nor cancels");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

if (failed > 0) {
  process.stderr.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
