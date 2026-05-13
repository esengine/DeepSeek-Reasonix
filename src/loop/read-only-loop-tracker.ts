/** Default streak length before the loop force-escalates flash→pro on consecutive read-only tool calls (#681). */
export const READONLY_LOOP_ESCALATION_THRESHOLD = 8;

/** Streak of consecutive read-only tool calls within a step; mutating call resets. Crossing the threshold lets the loop escalate flash→pro before the user pays for many more reads. */
export class ReadOnlyLoopTracker {
  private streak = 0;
  private readonly threshold: number;

  constructor(threshold: number = READONLY_LOOP_ESCALATION_THRESHOLD) {
    this.threshold = Math.max(1, threshold);
  }

  reset(): void {
    this.streak = 0;
  }

  /** True ONLY on the call where the streak crosses the configured threshold. */
  noteAndCrossedThreshold(isReadOnly: boolean): boolean {
    if (!isReadOnly) {
      this.streak = 0;
      return false;
    }
    const before = this.streak;
    this.streak += 1;
    return before < this.threshold && this.streak >= this.threshold;
  }

  get currentStreak(): number {
    return this.streak;
  }
}
