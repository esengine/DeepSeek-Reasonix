import assert from "node:assert/strict";
import { act } from "react";
import { createTranscriptHarness } from "./transcript-dom-harness";
import { applySessionExperience } from "../lib/sessionExperience";
import type { Item } from "../lib/useController";

const harness = await createTranscriptHarness({ deterministic: true });
const user: Item = { kind: "user", id: "u", text: "question" };
const answer: Item = { kind: "assistant", id: "a", text: "", reasoning: "first line\n\nlatest line", streaming: true };
try {
  for (const mode of ["standard", "deep"] as const) {
    await act(async () => applySessionExperience(mode));
    await harness.render([user, answer], { running: true });
    await harness.settle();
    const heading = harness.container.querySelector<HTMLElement>(".chat-reasoning [data-disclosure-row]")!;
    assert.ok(heading.textContent?.includes("latest line"));
    assert.equal(heading.getAttribute("aria-expanded"), "false", "old display preferences cannot auto-open thought bodies");
    assert.equal(harness.container.querySelector(".chat-reasoning__body"), null);
  }
  const heading = harness.container.querySelector<HTMLElement>(".chat-reasoning [data-disclosure-row]")!;
  await act(async () => heading.click());
  assert.ok(harness.container.querySelector(".chat-reasoning__body"));
  await harness.render([user, { ...answer, reasoning: "first line\n\nnew line", streaming: false, reasoningDurationMs: 1200 }]);
  await harness.settle();
  assert.equal(heading.getAttribute("aria-expanded"), "true", "manual disclosure survives settlement");
  assert.ok(!heading.textContent?.includes("latest line"), "expanded row does not repeat its summary");
  await act(async () => heading.click());
  assert.equal(harness.container.querySelector(".chat-reasoning__body"), null, "collapsed content is unmounted");
  console.log("thought presentation: latest/first line, duration, manual disclosure and legacy preference independence passed");
} finally {
  await act(async () => applySessionExperience("standard"));
  await harness.unmount(); await harness.close();
}
