// Run: tsx src/__tests__/transcript-fold-preference.test.tsx
//
// Pins that switching the work-process-fold preference applies to folds
// already on screen through the live event bus — not only to folds mounted
// afterwards.

import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntranscript fold preference sync");

const harness = await createTranscriptHarness();
const { container } = harness;
const { setProcessFoldPreference } = await harness.loadModule<{
  setProcessFoldPreference: (pref: "auto" | "expanded") => void;
}>("/src/lib/processFoldPreference.ts");

const items: Item[] = [
  { kind: "user", id: "u1", text: "ask" },
  { kind: "assistant", id: "a1", text: "answered", reasoning: "quick thought", streaming: false, workDurationMs: 3_000 },
  {
    kind: "notice",
    id: "decision-1",
    level: "info",
    text: "Decision recorded: answered",
    decisionReceipt: {
      id: "ask-1",
      kind: "ask",
      subject: "Choose a model: DeepSeek V4",
      outcome: "answered",
    },
  },
];

try {
  await harness.render(items, { running: false });

  ok(container.querySelector(".turn-collapse"), "completed turn renders its work fold");
  ok(!container.querySelector(".turn-collapse--open"), "auto preference keeps the completed fold collapsed");
  // A collapsed fold builds no React subtree: neither the reasoning nor the
  // receipt notice it folded is in the DOM.
  ok(!container.textContent?.includes("quick thought"), "collapsed fold keeps its process content unmounted");
  ok(!container.textContent?.includes("Question answered"), "collapsed fold keeps the folded receipt unmounted");

  const { act } = await import("react");
  await act(async () => {
    setProcessFoldPreference("expanded");
  });
  await harness.flush();
  ok(container.querySelector(".turn-collapse--open"), "switching to keep-expanded opens folds already on screen");
  ok(container.textContent?.includes("quick thought"), "expanded fold mounts its process rows");
  ok(container.textContent?.includes("Question answered"), "Ask receipt keeps its completed title");
  ok(!container.querySelector(".notice-line__decision-outcome"), "Ask receipt does not repeat the answered outcome");

  await act(async () => {
    setProcessFoldPreference("auto");
  });
  await harness.flush();
  ok(!container.querySelector(".turn-collapse--open"), "switching back to auto collapses completed folds again");
  ok(!container.textContent?.includes("quick thought"), "re-collapsed fold unmounts its process rows again");
} finally {
  await harness.unmount();
  await harness.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
