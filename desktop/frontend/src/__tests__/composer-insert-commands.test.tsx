import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useComposerInsertCommands, type ComposerInsertCommandsInput } from "../app-runtime/useComposerInsertCommands";
import type { Translator } from "../lib/i18n";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);

const t = ((key: string) => key) as Translator;
const toasts: string[] = [];

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

const terminalReads: string[] = [];
let terminalGate: ReturnType<typeof deferred<string>> | null = null;

const operations: ComposerInsertCommandsInput["operations"] = async (target, channel, input, execute) => {
  const authority = { checkpoint() {}, ownsUI: () => true };
  try {
    const value = await execute(input, authority);
    return { status: "completed", value };
  } catch (error) {
    return { status: "failed", error };
  }
};

let states!: ReturnType<typeof useComposerInsertCommands>;
function Probe({ approval }: { approval?: { id: string; tool: string } | null }) {
  states = useComposerInsertCommands({
    activeTabId: "A",
    sessionKey: "A:1",
    approval,
    operations,
    t,
    showToast: (message) => { toasts.push(message); },
    ports: {
      terminalOutput: async (tabId, sessionId) => {
        terminalReads.push(`${tabId}:${sessionId}`);
        return terminalGate ? terminalGate.promise : "last output";
      },
    },
  });
  return null;
}
const paint = (approval?: { id: string; tool: string } | null) =>
  act(async () => root.render(<Probe approval={approval} />));

try {
  await paint();
  await act(async () => { states.addWorkspaceTextToComposer("hello"); });
  assert.equal(states.composerInsertRequest?.text, "hello", "plain workspace text lands in the composer");
  assert.equal(states.composerInsertRequest?.mode, undefined, "plain insert keeps the default append mode");

  await act(async () => { states.prefillSubagentCommand("/run tests"); });
  assert.equal(states.composerInsertRequest?.mode, "prefix", "subagent prefill uses prefix mode");

  await act(async () => { states.replaceComposerInsert("A", ""); });
  assert.equal(states.composerInsertRequest?.mode, "replace", "undo clears through a replace insert");

  await act(async () => { states.addSelectedTextToComposer("  snippet  "); });
  assert.equal(states.selectedTextRequest?.text, "snippet", "selected text is trimmed before insert");
  await act(async () => { states.addSelectedTextToComposer("   "); });
  assert.equal(states.selectedTextRequest?.text, "snippet", "blank selections insert nothing");

  await act(async () => { states.addWorkspaceCodeToComposer("src/a.ts", "const a = 1;"); });
  assert.equal(states.selectedTextRequest?.path, "src/a.ts", "workspace code carries its path");

  await act(async () => { states.handleRevisionActiveChange(true); });
  await paint({ id: "ap-1", tool: "exit_plan_mode" });
  await act(async () => { states.addWorkspaceTextToComposer("revise this"); });
  assert.equal(states.activePlanRevisionInsertRequest?.text, "revise this", "plan-revision target routes plain text to the revision input");
  assert.equal(states.composerInsertRequest?.mode, "replace", "plan-revision routing does not touch the composer");
  await act(async () => { states.addWorkspaceCodeToComposer("src/b.ts", "code"); });
  assert.equal(states.activePlanRevisionInsertRequest?.text?.includes("src/b.ts"), true, "code lands in the revision input as a fenced reference");

  await paint({ id: "ap-2", tool: "exit_plan_mode" });
  assert.equal(states.activePlanRevisionInsertRequest, null, "a replacement approval id invalidates the pending revision insert");

  await paint(null);
  await act(async () => { await states.addTerminalOutputToComposer("term-9"); });
  assert.deepEqual(terminalReads, ["A:term-9"], "terminal output reads through the session port");
  assert.equal(states.composerInsertRequest?.text?.includes("last output"), true, "terminal output is formatted into the composer");

  terminalGate = deferred<string>();
  const pending = states.addTerminalOutputToComposer("term-10");
  await act(async () => { terminalGate!.resolve(""); await pending; });
  assert.deepEqual(toasts, ["terminal.noOutput"], "empty terminal output reports once");

  await act(async () => root.unmount());
  console.log("composer insert commands: routing, plan-revision target, selection trimming and terminal output chains passed");
} finally { dom.window.close(); }
