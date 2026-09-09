import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useDeliveryContinueCommands } from "../app-runtime/useDeliveryContinueCommands";
import { createSessionSurfaceFence } from "../app-runtime/sessionTarget";
import type { Translator } from "../lib/i18n";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);

const t = ((key: string) => key) as Translator;

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

const calls: string[] = [];
let resumeGate: ReturnType<typeof deferred<boolean>> | null = null;
let resumeResult = true;
const fence = createSessionSurfaceFence();

let states!: ReturnType<typeof useDeliveryContinueCommands>;
function Probe({ ready = true, goal }: { ready?: boolean; goal?: string }) {
  states = useDeliveryContinueCommands({
    surfaceFence: fence,
    ready,
    goal,
    t,
    ports: {
      resumeGoal: async (tabId) => {
        calls.push(`resume:${tabId}`);
        if (resumeGate) return resumeGate.promise;
        return resumeResult;
      },
      recoverDelivery: async (tabId, prompt) => { calls.push(`send:${tabId}:${prompt}`); },
    },
  });
  return null;
}
const paint = (props?: { ready?: boolean; goal?: string }) => act(async () => root.render(<Probe {...props} />));

try {
  await paint();
  fence.commit("A", "A:1");

  await paint({ ready: false });
  await act(async () => { await states.handleDeliveryContinue(); });
  assert.deepEqual(calls, [], "a controller that is not ready continues nothing");

  await paint({ ready: true });
  await act(async () => { await states.handleDeliveryContinue(); });
  assert.deepEqual(calls, ["send:A:notice.deliveryIncompleteContinuePrompt"],
    "a goal-less delivery sends the recovery prompt to the committed tab");
  calls.length = 0;

  await paint({ goal: "ship it" });
  await act(async () => { await states.handleDeliveryContinue(); });
  assert.deepEqual(calls, ["resume:A", "send:A:notice.deliveryIncompleteContinuePrompt"],
    "a goal tab resumes its goal before the recovery send");
  calls.length = 0;

  resumeResult = false;
  await act(async () => { await states.handleDeliveryContinue(); });
  assert.deepEqual(calls, ["resume:A"], "a goal that refuses to resume is not poked further");
  resumeResult = true;

  resumeGate = deferred<boolean>();
  calls.length = 0;
  let stale: Promise<void> | undefined;
  await act(async () => { stale = states.handleDeliveryContinue(); });
  fence.commit("B", "B:1");
  await act(async () => {
    resumeGate!.resolve(true);
    await stale;
  });
  assert.deepEqual(calls, ["resume:A"], "a mid-flight tab switch revokes the captured ownership and blocks the send");
  resumeGate = null;
  fence.commit("A", "A:1");
  calls.length = 0;
  await act(async () => { await states.handleDeliveryContinue(); });
  assert.deepEqual(calls, ["resume:A", "send:A:notice.deliveryIncompleteContinuePrompt"],
    "a fresh capture on the restored tab owns the UI again");

  fence.dispose();
  calls.length = 0;
  await act(async () => { await states.handleDeliveryContinue(); });
  assert.deepEqual(calls, [], "without a committed surface there is no continuation target");

  console.log("delivery continue commands: ready gate, goal resume chain, stale-ownership fence and empty-target gate passed");
} finally { dom.window.close(); }
