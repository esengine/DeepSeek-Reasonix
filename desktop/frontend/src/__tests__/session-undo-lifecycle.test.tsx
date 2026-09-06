import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useSessionUndo, type RewindResultView } from "../app-runtime/useSessionUndo";
import type { Item } from "../lib/useController";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
function deferred() {
  let resolve!: (value: RewindResultView) => void;
  const promise = new Promise<RewindResultView>((yes) => { resolve = yes; });
  return { promise, resolve };
}
function user(text: string, checkpointTurn: number): Item {
  return { kind: "user", id: `u:${text}`, text, submitText: text, checkpointTurn } as Item;
}
const calls: string[] = [];
const outcomes = new Map<string, { outcome?: RewindResultView; gate?: ReturnType<typeof deferred> }>();
let states!: ReturnType<typeof useSessionUndo>;
function Probe({ readOnly = false, hydrating = false }: { readOnly?: boolean; hydrating?: boolean }) {
  const items: Item[] = hydrating ? [] : [user("one", 1), user("two", 2)];
  states = useSessionUndo({
    activeTabId: "A", activeTabReadOnly: readOnly, items,
    hydratePlaceholderActive: hydrating, controllerReady: true, running: false,
    messageActionOpen: false, approvalOpen: false, askOpen: false, clearContextPending: false,
    ports: {
      rewindForTab: async () => { calls.push("rewind"); return true; },
      rewindForTabDetailed: async (tabId, turn, scope) => {
        calls.push(`detailed:${tabId}:${turn}:${scope}`);
        const entry = outcomes.get(`${turn}:${scope}`);
        if (entry?.gate) return entry.gate.promise;
        return entry?.outcome ?? { ok: true };
      },
      refreshTabMetas: () => { calls.push("refresh-metas"); },
      undoRewindForTab: async () => { calls.push("undo"); return true; },
      sendToTab: async () => { calls.push("send"); },
      composeInsert: (_tabId, text) => { calls.push(`insert:${text}`); },
      refreshDock: () => { calls.push("dock"); },
      refreshProject: () => { calls.push("project"); },
    },
  });
  return null;
}
const paint = (options?: { readOnly?: boolean; hydrating?: boolean }) => act(async () => root.render(<Probe {...options} />));
try {
  await paint();
  await act(async () => { await states.handleMessageAction(0, "code"); });
  assert.equal(states.rewindState?.turnDiff, 0, "code-only rewind stores a zero-turn undo banner");
  assert.equal(states.rewindState?.transactionId, undefined, "empty backend result leaves no transaction id");
  assert.ok(calls.includes("dock") && calls.includes("project"), "code rewind refreshes files and project after success");
  assert.ok(!calls.some((call) => call.startsWith("insert:")), "code rewind never fills the composer");

  outcomes.set("0:code", { outcome: { ok: true, transactionId: "tx-9", undoAvailable: true, written: ["a.txt"], deleted: [] } });
  calls.length = 0;
  await act(async () => { await states.handleMessageAction(0, "code"); });
  assert.equal(states.rewindState?.transactionId, "tx-9", "code-only rewind retains the committed transaction id for real undo");
  assert.equal(states.rewindState?.undoAvailable, true, "undo stays available when the backend reports it");

  calls.length = 0;
  await act(async () => { states.setRewindStateForTab("A", null); });
  assert.equal(states.rewindState, null, "setRewindStateForTab clears the source banner");

  await act(async () => { await states.handleMessageAction(5, "both"); });
  assert.ok(calls.includes("rewind"), "a turn with no matching user boundary falls back to the controller rewind");
  assert.ok(calls.includes("dock") && calls.includes("project"), "fallback refresh still runs for scope both");

  outcomes.set("1:both", { gate: deferred() });
  calls.length = 0;
  const full = states.handleMessageAction(1, "both");
  await act(async () => {});
  await act(async () => {
    outcomes.get("1:both")!.gate!.resolve({ ok: true, transactionId: "tx-2", undoAvailable: true, written: [], deleted: [] });
    await full;
  });
  assert.equal(states.rewindState?.transactionId, "tx-2", "full rewind records the committed transaction id");
  assert.ok(calls.includes("insert:one"), "successful full rewind fills the composer with the original prompt");
  assert.ok(!calls.includes("refresh-metas"), "full rewind does not trigger a tab-list refresh");
  assert.equal(states.rewindCommitting, false, "committing flag clears after success");

  outcomes.set("2:both", { gate: deferred() });
  calls.length = 0;
  const failed = states.handleMessageAction(2, "both");
  await act(async () => {});
  await act(async () => {
    outcomes.get("2:both")!.gate!.resolve({ ok: false });
    await failed;
  });
  assert.equal(states.rewindState?.transactionId, "tx-2", "failed rewind leaves the previous banner untouched");
  assert.ok(!calls.some((call) => call.startsWith("insert:")), "failed rewind inserts nothing");
  assert.equal(states.rewindCommitting, false, "committing flag clears after failure");

  calls.length = 0;
  await act(async () => { states.setRewindStateForTab("A", { turnDiff: 1, transactionId: "pending-tx", undoAvailable: true }); });
  await act(async () => { await states.handleEditPrompt(0, " edited ", " submit "); });
  assert.deepEqual(calls, [], "edit prompt is blocked while an undo banner owns the source tab");

  await act(async () => { states.setRewindStateForTab("A", null); });
  calls.length = 0;
  outcomes.set("0:conversation", { outcome: { ok: true, tabId: "A", transactionId: "edit-tx", undoAvailable: true } });
  await act(async () => { await states.handleEditPrompt(0, " edited ", " submit "); });
  assert.ok(calls.includes("detailed:A:0:conversation"), "allowed edit rewinds through the detailed backend");
  assert.ok(calls.includes("send"), "allowed edit resends the edited prompt after the conversation rewind");

  console.log("session undo lifecycle: code transaction retention, banners, failed rewinds and edit gates passed");
} finally { dom.window.close(); }
