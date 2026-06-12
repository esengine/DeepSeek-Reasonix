// Run: tsx src/__tests__/context-panel-breakdown.test.ts

import { contextBreakdown, contextCostDisplay } from "../components/ContextPanel";
import { currencySymbol, formatMoney } from "../lib/money";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\ncontext panel breakdown");

const mock = contextBreakdown(42_124, 128_000, 22_134, 12_345, 7_521);
eq(
  {
    promptTokens: mock.promptTokens,
    completionTokens: mock.completionTokens,
    reasoningTokens: mock.reasoningTokens,
    otherTokens: mock.otherTokens,
  },
  {
    promptTokens: 22_134,
    completionTokens: 4_824,
    reasoningTokens: 7_521,
    otherTokens: 7_645,
  },
  "reasoning is split out of completion rather than double-counted",
);
eq(
  mock.promptTokens + mock.completionTokens + mock.reasoningTokens + mock.otherTokens,
  42_124,
  "legend values sum to used context tokens",
);
eq(Math.round(mock.otherPct), 33, "donut endpoint follows used/window percent");

const oversized = contextBreakdown(61_000, 1_000_000, 1_622_277, 12_049, 3_217);
eq(
  oversized.promptTokens + oversized.completionTokens + oversized.reasoningTokens + oversized.otherTokens,
  61_000,
  "oversized provider breakdown is normalized to used context tokens",
);
eq(Math.round(oversized.otherPct * 10) / 10, 6.1, "oversized provider breakdown does not fill the ring");

const unknownWindow = contextBreakdown(42_124, 0, 22_134, 12_345, 7_521);
eq(
  {
    promptPct: unknownWindow.promptPct,
    completionPct: unknownWindow.completionPct,
    reasoningPct: unknownWindow.reasoningPct,
    otherPct: unknownWindow.otherPct,
  },
  {
    promptPct: 0,
    completionPct: 0,
    reasoningPct: 0,
    otherPct: 0,
  },
  "unknown context window keeps donut segments empty",
);

console.log("\ncontext panel cost");

const infoCost = contextCostDisplay({
  info: { sessionCost: 0.1759, sessionCurrency: "$", sessionCostUsd: 0.1759 },
  sessionCost: 0,
  sessionCurrency: "¥",
  usage: { cost: 0, costUsd: 0, currency: "¥" },
});
eq(infoCost, { amount: 0.1759, currency: "$" }, "panel cost keeps the panel currency instead of state default");
eq(formatMoney(infoCost.amount, infoCost.currency, "dash"), "$0.1759", "USD panel cost renders with dollar sign");
eq(currencySymbol("楼"), "¥", "unexpected currency text does not leak into money values");
eq(currencySymbol("aud"), "AUD ", "unknown ISO currency codes stay readable");
eq(currencySymbol("A$"), "A$", "compact multi-character currency symbols are preserved");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
