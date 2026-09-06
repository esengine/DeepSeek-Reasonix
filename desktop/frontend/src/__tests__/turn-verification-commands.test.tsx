import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useTurnVerificationCommands } from "../app-runtime/useTurnVerificationCommands";
import type { WireCompletionSummary } from "../lib/types";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);

function summary(mutations: number): WireCompletionSummary {
  return {
    preset: "balanced",
    verdict: "partial",
    mutations,
    checks_passed: 2,
    checks_failed: 1,
    checks_suppressed: 0,
    review: "passed",
    gap_kinds: [],
    constraint_degraded: false,
  };
}

const dockCalls: string[] = [];
let states!: ReturnType<typeof useTurnVerificationCommands>;
function Probe(props: { activeTabId?: string; turnStartAt?: number; completionSummary?: WireCompletionSummary }) {
  states = useTurnVerificationCommands({
    activeTabId: props.activeTabId,
    turnStartAt: props.turnStartAt ?? 0,
    completionSummary: props.completionSummary,
    openChangedDock: () => { dockCalls.push("changed"); },
  });
  return null;
}
const paint = (props?: { activeTabId?: string; turnStartAt?: number; completionSummary?: WireCompletionSummary }) =>
  act(async () => root.render(<Probe activeTabId="A" turnStartAt={100} completionSummary={summary(1)} {...props} />));

try {
  await paint();
  assert.equal(states.verificationRevealRequest, null, "no reveal request exists before the first open");

  const historical = summary(7);
  await act(async () => { states.openTurnVerification(historical); });
  assert.deepEqual(dockCalls, ["changed"], "opening verification reveals the changed-files dock");
  assert.deepEqual(states.verificationRevealRequest, {
    id: 1, summary: historical, tabId: "A", turnStartAt: 100, currentSummary: summary(1),
  }, "the reveal request binds the clicked summary to the tab and turn that published it");

  const second = summary(9);
  await act(async () => { states.openTurnVerification(second); });
  assert.equal(states.verificationRevealRequest?.id, 2, "reveal request ids increase monotonically");
  assert.equal(states.verificationRevealRequest?.summary, second, "the newest open replaces the pending request");
  assert.equal(dockCalls.length, 2, "every open re-reveals the dock");

  await paint({ turnStartAt: 200 });
  assert.equal(states.verificationRevealRequest, null, "a new turn clears the historical reveal");

  await act(async () => { states.openTurnVerification(summary(3)); });
  assert.equal(states.verificationRevealRequest?.id, 3, "the reveal sequence survives resets");
  await paint({ activeTabId: "B" });
  assert.equal(states.verificationRevealRequest, null, "switching tabs clears the historical reveal");

  await act(async () => { states.openTurnVerification(summary(4)); });
  await paint({ completionSummary: summary(2) });
  assert.equal(states.verificationRevealRequest, null, "a new completion summary clears the historical reveal");

  await paint({ activeTabId: undefined });
  await act(async () => { states.openTurnVerification(summary(5)); });
  assert.equal(states.verificationRevealRequest?.tabId, "", "opening without an active tab records an empty tab binding");

  console.log("turn verification commands: dock reveal, sequenced requests and reset lifecycle passed");
} finally { dom.window.close(); }
