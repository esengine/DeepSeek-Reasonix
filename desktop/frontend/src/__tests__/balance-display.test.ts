// Run: tsx src/__tests__/balance-display.test.ts
// Tests for balance display logic — cycleMode and getLabel simulate
// the BalanceIndicator component's state transitions.

let passed = 0;
let failed = 0;

function eq<T>(a: T, b: T, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

// ---- cycleMode: simulates the click-to-cycle logic ----

function cycleMode(current: number, entryCount: number): number {
  if (entryCount <= 1) return 0;
  return (current + 1) % (entryCount + 1);
}

function getLabel(balanceMode: number, displayAll: string, entries: { display: string }[]): string {
  if (entries.length === 0) return "-";
  if (balanceMode === 0) return displayAll;
  return entries[balanceMode - 1]?.display ?? displayAll;
}

// ---- Tests ----

console.log("\nbalance display logic");

// Single currency — always mode 0
eq(cycleMode(0, 1), 0, "single currency stays at mode 0");

// Two currencies — cycle 0→1→2→0
eq(cycleMode(0, 2), 1, "two currencies: mode 0 → 1");
eq(cycleMode(1, 2), 2, "two currencies: mode 1 → 2");
eq(cycleMode(2, 2), 0, "two currencies: mode 2 → 0");

// Three currencies — cycle 0→1→2→3→0
eq(cycleMode(0, 3), 1, "three currencies: mode 0 → 1");
eq(cycleMode(3, 3), 0, "three currencies: mode 3 → 0");

// getLabel — empty entries
eq(getLabel(0, "¥110.00 | $15.30", []), "-", "empty entries shows dash");

// getLabel — mode 0 shows all
eq(getLabel(0, "¥110.00 | $15.30", [
  { display: "¥110.00" },
  { display: "$15.30" },
]), "¥110.00 | $15.30", "mode 0 shows all");

// getLabel — mode 1 shows first currency
eq(getLabel(1, "¥110.00 | $15.30", [
  { display: "¥110.00" },
  { display: "$15.30" },
]), "¥110.00", "mode 1 shows first currency");

// getLabel — mode 2 shows second currency
eq(getLabel(2, "¥110.00 | $15.30", [
  { display: "¥110.00" },
  { display: "$15.30" },
]), "$15.30", "mode 2 shows second currency");

// ---- summary ----
const total = passed + failed;
console.log(`\n${total} tests: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
