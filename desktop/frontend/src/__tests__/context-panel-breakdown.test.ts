// Run: tsx src/__tests__/context-panel-breakdown.test.ts

import { contextBreakdown, contextCostDisplay, formatCacheHitRate, formatMetricTokens } from "../components/ContextPanel";
import { currencySymbol, formatMoney, formatMoneyLocalized } from "../lib/money";

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

function ok(condition: boolean, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
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

const issue5283 = contextBreakdown(6888, 1_000_000, 6840, 48, 48);
eq(
  {
    promptTokens: issue5283.promptTokens,
    completionTokens: issue5283.completionTokens,
    reasoningTokens: issue5283.reasoningTokens,
    otherTokens: issue5283.otherTokens,
  },
  {
    promptTokens: 6840,
    completionTokens: 0,
    reasoningTokens: 48,
    otherTokens: 0,
  },
  "prompt tokens are not scaled down when used context includes completion tokens",
);

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
const usdLocalized = formatMoneyLocalized(0.1759, "USD", { locale: "en" });
ok(/\$|USD|US\$/.test(usdLocalized) && usdLocalized.includes("0.1759"), "ISO USD cost renders with locale-aware currency formatting");
const cnyLocalized = formatMoneyLocalized(12.3, "CNY", { locale: "zh" });
ok(/¥|CNY|CN¥/.test(cnyLocalized) && cnyLocalized.includes("12.30"), "ISO CNY cost renders with locale-aware currency formatting");
eq(formatMoneyLocalized(0.1759, "A$", { locale: "en" }), "A$0.1759", "symbol currency remains symbol-based");
eq(formatMoneyLocalized(0, "USD", { locale: "en", empty: "dash" }), "-", "localized money preserves dash empty state");

console.log("\ncontext panel cache rate");

eq(formatCacheHitRate(99_950, 50), "99.95%", "cache hit rate preserves two decimal places");
eq(formatCacheHitRate(0, 10_000), "0.00%", "cache hit rate shows zero when usage data exists");
eq(formatCacheHitRate(0, 0), "-", "cache hit rate stays empty before usage data exists");

console.log("\ncontext panel metric token labels");

const exactMetric = formatMetricTokens(999_999, "en");
eq(exactMetric.display, "999,999", "sub-million metric tokens keep exact comma formatting");
eq(exactMetric.exact, "999,999", "sub-million exact metric title matches the display");

const largeMetric = formatMetricTokens(123_456_789, "en");
eq(largeMetric.display, "123,456,789", "large metric tokens keep exact comma formatting");
eq(largeMetric.exact, "123,456,789", "large metric exact title matches the display");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
