// Run: tsx src/__tests__/quota-recovery-intent.test.ts

import {
  consumeQuotaRecoveryIntent,
  quotaRecoverySessionPath,
  type QuotaRecoveryIntent,
} from "../lib/quotaRecovery";

function ok(value: boolean, label: string) {
  if (!value) throw new Error(label);
  process.stdout.write(`  PASS  ${label}\n`);
}

console.log("\nquota recovery intent");

const initial: QuotaRecoveryIntent = { tabId: "tab-a", sessionPath: "/sessions/a.jsonl" };
const ref: { current: QuotaRecoveryIntent | null } = { current: initial };
ok(consumeQuotaRecoveryIntent(ref, initial), "the current recovery attempt is consumed");
ok(ref.current === null, "a consumed recovery attempt is cleared");

ref.current = initial;
ref.current = null; // navigation left the tab while model rebuild was in flight
ok(!consumeQuotaRecoveryIntent(ref, initial), "navigation-invalidated recovery does not resume the old tab");

const newer: QuotaRecoveryIntent = { tabId: "tab-a", sessionPath: "/sessions/newer.jsonl" };
ref.current = newer;
ok(!consumeQuotaRecoveryIntent(ref, initial), "a stale completion does not consume a newer recovery action");
ok(ref.current === newer, "the newer recovery action remains intact");

ok(
  quotaRecoverySessionPath("", "/sessions/controller.jsonl") === "/sessions/controller.jsonl",
  "empty tab metadata falls back to the controller session identity",
);
ok(
  quotaRecoverySessionPath(" /sessions/tab.jsonl ", "/sessions/controller.jsonl") === "/sessions/tab.jsonl",
  "non-empty tab session identity remains authoritative",
);
