import assert from "node:assert/strict";
import { act } from "react";
import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";
const harness = await createTranscriptHarness();
const items: Item[] = [
  { kind: "user", id: "u42", text: "history starts mid-session", checkpointTurn: 42 },
  { kind: "assistant", id: "a42", text: "answer", reasoning: "", streaming: false,
    createdAt: new Date(2026, 8, 12, 12, 34).getTime(), turnDurationMs: 29_000, tokensPerSecond: 183,
    turnUsage: { totalTokens: 185_225, uncachedInputTokens: 26_278, cacheReadTokens: 155_520, outputTokens: 3_427, reasoningTokens: 1_909, routes: ["deepseek-official/deepseek-flash"] } },
];
const calls: number[] = [];
const props = { checkpoints: [{ turn: 42, prompt: "history starts mid-session", files: [], time: 1, canConversation: true }],
  onFork: (turn: number) => { calls.push(turn); } };
try {
  await harness.render(items, props); await harness.settle();
  const branch = () => harness.container.querySelector<HTMLButtonElement>(".chat-actions button.chat-action-icon:not(.copybtn)")!;
  assert.ok(branch(), "completed turn exposes the icon-only Harness branch action");
  assert.equal(branch().textContent, "", "branch label stays in the tooltip instead of the reading flow");
  assert.equal(branch().closest(".chat-actions")?.getAttribute("data-actions-reveal"), "always", "latest turn actions remain visible");
  assert.equal(branch().hasAttribute("disabled"), false, "available branch is interactive");
  assert.equal(branch().getAttribute("aria-disabled"), null);
  await act(async () => { branch().focus(); await new Promise(resolve => setTimeout(resolve, 1)); });
  assert.equal(harness.dom.window.document.querySelector('[role="tooltip"]')?.textContent, branch().getAttribute("aria-label"), "keyboard focus exposes the Harness tooltip");
  await act(async () => branch().click());
  assert.deepEqual(calls, [42], "ordinary branch uses the authoritative checkpoint, never a page index");
  const statButtons = () => [...harness.container.querySelectorAll<HTMLButtonElement>(".chat-stat-trigger")];
  assert.equal(statButtons().length, 2, "completed answers expose Harness usage and time pills");
  assert.match(statButtons()[0].textContent ?? "", /185\.2K|185K/);
  await act(async () => statButtons()[0].click());
  const usageDialog = harness.dom.window.document.querySelector<HTMLElement>("[data-turn-usage-details]")!;
  assert.ok(usageDialog, "usage pill opens its anchored details dialog");
  assert.match(usageDialog.textContent ?? "", /deepseek-official\/deepseek-flash/);
  assert.match(usageDialog.textContent ?? "", /155,520/);
  await act(async () => statButtons()[1].click());
  const timeDialog = harness.dom.window.document.querySelector<HTMLElement>("[data-turn-time-details]")!;
  assert.ok(timeDialog, "time pill opens its anchored details dialog");
  assert.match(timeDialog.textContent ?? "", /29/);
  assert.match(timeDialog.textContent ?? "", /183/);
  assert.match(harness.container.querySelector(".chat-actions__time")?.textContent ?? "", /12:34/);
  assert.equal(harness.container.textContent?.includes("Like"), false, "feedback actions are intentionally not transplanted");
  assert.equal(harness.container.querySelector(".msg-edit"), null);
  for (const disabled of [{ running: true }, { rewindDisabled: true }, { checkpoints: [] }]) {
    await harness.render(items, { ...props, ...disabled }); await harness.settle();
    const before = calls.length;
    assert.equal(branch().hasAttribute("disabled"), false, "unavailable action remains focusable for its explanation");
    assert.equal(branch().getAttribute("aria-disabled"), "true");
    assert.ok(branch().getAttribute("aria-describedby"));
    await act(async () => branch().click());
    assert.equal(calls.length, before, "unavailable branch never dispatches");
  }
  console.log("chat branches: Harness icon action, checkpoint identity and capability gates passed");
} finally { await harness.unmount(); await harness.close(); }
