// Run: tsx src/__tests__/checkpoint-turn-transcript.test.tsx

import { initialState, reducer } from "../lib/useController";
import { createTranscriptHarness } from "./transcript-dom-harness";

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

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, `${label}${actual === expected ? "" : `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`}`);
}

console.log("\nturn checkpoint transcript integration");

let state = reducer(initialState, {
  type: "history_page",
  mode: "replace",
  page: {
    messages: [
      { role: "user", content: "existing prompt", checkpointTurn: 0 },
    ],
    startTurn: 0,
    endTurn: 1,
    totalTurns: 1,
    hasOlder: false,
  },
});
const cancelledSubmissionId = "transcript-cancelled-submission";
state = reducer(state, { type: "user", text: "cancelled prompt", seq: state.seq, submissionId: cancelledSubmissionId });
state = reducer(state, { type: "unsend" });
state = reducer(state, { type: "event", e: { kind: "turn_done", err: "context canceled", checkpointTurn: 1, submissionId: cancelledSubmissionId } });
state = reducer(state, {
  type: "checkpoints",
  checkpoints: [
    { turn: 0, prompt: "existing prompt", files: [], time: 1, canConversation: true },
    { turn: 1, prompt: "cancelled prompt", files: [], time: 2, canConversation: true },
  ],
});

const editTargets: number[] = [];
const harness = await createTranscriptHarness();
try {
  await harness.render(state.items, {
    checkpoints: state.checkpoints,
    onEditPrompt: (turn: number) => {
      editTargets.push(turn);
      return true;
    },
  });

  const cancelledMessage = Array.from(harness.container.querySelectorAll<HTMLElement>(".msg--user"))
    .find((element) => element.textContent?.includes("cancelled prompt"));
  eq(cancelledMessage?.dataset.turn, "1", "Transcript maps the cancelled user to checkpoint turn 1");
  const editButton = cancelledMessage?.querySelector<HTMLButtonElement>('button[aria-label="Edit"]');
  eq(editButton?.disabled, false, "checkpoint metadata enables Edit for the cancelled turn");

  const { act } = await import("react");
  await act(async () => editButton?.click());
  await harness.flush();
  const editForm = cancelledMessage?.querySelector<HTMLFormElement>("form.msg-edit");
  await act(async () => {
    editForm?.dispatchEvent(new harness.dom.window.Event("submit", { bubbles: true, cancelable: true }));
  });
  await harness.flush();
  eq(editTargets[0], 1, "inline resend targets the authoritative checkpoint turn 1");
} finally {
  await harness.unmount();
  await harness.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
