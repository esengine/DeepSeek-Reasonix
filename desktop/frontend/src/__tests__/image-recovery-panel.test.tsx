import assert from "node:assert/strict";
import { act } from "react";
import { createTranscriptHarness } from "./transcript-dom-harness";
import { initialState, reducer } from "../lib/useController";
import type { WireEvent } from "../lib/types";

const event: WireEvent = {
  kind: "turn_done",
  turnId: "turn-image-recovery",
  err: "provider rejected an image",
  imageRecovery: {
    id: "recovery-token",
    reason: "invalid_image",
    candidates: [
      { identity: { messageId: "message-a", imageOrdinal: 0, contentDigest: "a".repeat(64) }, label: "first" },
      { identity: { messageId: "message-a", imageOrdinal: 1, contentDigest: "b".repeat(64) }, label: "second" },
    ],
  },
};
const state = reducer(initialState, { type: "event", e: event });
const notice = state.items.find((item) => item.kind === "notice" && item.action === "isolate_images");
assert.ok(notice?.kind === "notice", "ambiguous image rejection creates a recovery notice");
assert.equal(notice.imageRecovery?.candidates.length, 2);

const prompts: Array<{ display: string; submit?: string }> = [];
const harness = await createTranscriptHarness();
try {
  await harness.render([notice], {
    tabId: "tab-image-recovery",
    onPrompt: (display: string, submit?: string) => prompts.push({ display, submit }),
  });
  await harness.settle();
  const checkboxes = [...harness.container.querySelectorAll<HTMLInputElement>(".chat-notice--image-recovery input[type=checkbox]")];
  const save = harness.container.querySelector<HTMLButtonElement>(".chat-notice--image-recovery button");
  assert.equal(checkboxes.length, 2);
  assert.ok(checkboxes.every((checkbox) => !checkbox.checked), "recovery never preselects images");
  assert.ok(save?.disabled, "saving is disabled until the user selects an image");

  await act(async () => checkboxes[0].click());
  assert.equal(checkboxes[0].checked, true);
  assert.equal(checkboxes[1].checked, false);
  assert.equal(save?.disabled, false);

  await act(async () => save?.click());
  await harness.waitFor(
    () => harness.container.textContent?.includes("Send a new message") ?? false,
    "saved image recovery state",
  );
  assert.deepEqual(prompts, [], "manual image isolation does not replay the failed user request");
  assert.ok(checkboxes.every((checkbox) => checkbox.disabled), "resolved choices cannot be submitted twice");
} finally {
  await harness.unmount();
  await harness.close();
}
console.log("image recovery panel selection and no-replay behavior passed");
