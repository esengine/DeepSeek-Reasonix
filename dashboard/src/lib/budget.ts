export type BudgetState =
  | { kind: "off"; spent: number }
  | { kind: "running"; cap: number; spent: number; pct: number }
  | { kind: "warn"; cap: number; spent: number; pct: number }
  | { kind: "exhausted"; cap: number; spent: number; pct: number };

export function deriveBudgetState(
  cap: number | null | undefined,
  spent: number | null | undefined,
): BudgetState {
  const safeSpent = typeof spent === "number" && spent >= 0 ? spent : 0;
  if (typeof cap !== "number" || cap <= 0) {
    return { kind: "off", spent: safeSpent };
  }
  const pct = (safeSpent / cap) * 100;
  if (pct >= 100) return { kind: "exhausted", cap, spent: safeSpent, pct };
  if (pct >= 80) return { kind: "warn", cap, spent: safeSpent, pct };
  return { kind: "running", cap, spent: safeSpent, pct };
}

/** Default quick-cap menu — round dollar amounts users actually pick. */
export const QUICK_CAPS_USD: ReadonlyArray<number> = [1, 5, 10, 25, 50];

/** 1.5× / 2× / 4× the current cap, snapped to a "nice" round number per bucket. */
export function bumpSuggestions(currentCap: number): number[] {
  if (currentCap <= 0) return [];
  return [niceUp(currentCap * 1.5), niceUp(currentCap * 2), niceUp(currentCap * 4)];
}

function niceUp(n: number): number {
  // Subtract a tiny epsilon before ceil so FP noise (0.4 * 1.5 = 0.6000…01)
  // doesn't bump a value to the next bucket.
  const eps = 1e-9;
  if (n < 1) return Math.ceil((n - eps) * 10) / 10;
  if (n < 10) return Math.ceil((n - eps) * 2) / 2;
  if (n < 100) return Math.ceil(n - eps);
  return Math.ceil((n - eps) / 5) * 5;
}

/** Tone class shared between the cockpit tile and the settings gauge. */
export function budgetTone(state: BudgetState): "" | "warn" | "err" {
  if (state.kind === "exhausted") return "err";
  if (state.kind === "warn") return "warn";
  return "";
}
