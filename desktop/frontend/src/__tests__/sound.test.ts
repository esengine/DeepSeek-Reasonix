// Run: tsx src/__tests__/sound.test.ts

import { attentionChimeEventKey, clearAttentionChimeKeys, shouldPlayAttentionChimeForEvent } from "../lib/sound";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nsound notifications");

{
  eq(attentionChimeEventKey({ kind: "approval_request", tabId: "tab-a", approval: { id: "approval-1" } }), "approval:tab-a:approval-1", "approval request builds a tab-scoped chime key");
  eq(attentionChimeEventKey({ kind: "ask_request", tabId: "tab-a", ask: { id: "ask-1" } }), "ask:tab-a:ask-1", "ask request builds a tab-scoped chime key");
  eq(attentionChimeEventKey({ kind: "approval_request", approval: { id: "approval-1" } }), "approval::approval-1", "legacy approval events without a tab still build a stable key");
  eq(attentionChimeEventKey({ kind: "turn_done" }), undefined, "non-attention events do not build chime keys");
}

{
  const seen = new Set<string>();
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-a", approval: { id: "1" } }, seen), true, "first approval event plays");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-a", approval: { id: "1" } }, seen), false, "replayed approval event for the same tab is deduped");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-b", approval: { id: "1" } }, seen), true, "same approval id from another tab still plays");
  eq(shouldPlayAttentionChimeForEvent({ kind: "ask_request", tabId: "tab-a", ask: { id: "1" } }, seen), true, "ask id sharing an approval id still plays");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request" }, seen), false, "malformed approval event does not play");
}

{
  // The dedupe set stays bounded: after many unique prompts it self-prunes
  // while still deduping recently seen ids.
  const seen = new Set<string>();
  for (let i = 0; i < 2000; i++) {
    shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "t", approval: { id: String(i) } }, seen);
  }
  eq(seen.size <= 1024, true, "dedupe set stays bounded after 2000 unique prompts");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "t", approval: { id: "1999" } }, seen), false, "most recent prompt id is still deduped after pruning");
}

{
  // Runtime rebuild clears dedupe keys so reissued ids chime again.
  const seen = new Set<string>();
  shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-a", approval: { id: "1" } }, seen);
  shouldPlayAttentionChimeForEvent({ kind: "ask_request", tabId: "tab-b", ask: { id: "1" } }, seen);
  clearAttentionChimeKeys(seen, "tab-a");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-a", approval: { id: "1" } }, seen), true, "rebuilt tab's reissued approval id chimes again");
  eq(shouldPlayAttentionChimeForEvent({ kind: "ask_request", tabId: "tab-b", ask: { id: "1" } }, seen), false, "other tab's keys survive a scoped clear");
  clearAttentionChimeKeys(seen);
  eq(shouldPlayAttentionChimeForEvent({ kind: "ask_request", tabId: "tab-b", ask: { id: "1" } }, seen), true, "tab-less ready clears every key");
}

if (failed) {
  console.error(`sound notifications: ${failed} failed, ${passed} passed`);
  process.exit(1);
}

console.log(`sound notifications: ${passed} passed`);
