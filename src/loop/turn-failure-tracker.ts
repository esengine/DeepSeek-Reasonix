import type { RepairReport } from "../repair/index.js";

export const FAILURE_ESCALATION_THRESHOLD = 3;

export class TurnFailureTracker {
  private count = 0;
  private types: Record<string, number> = {};

  reset(): void {
    this.count = 0;
    this.types = {};
  }

  /** True ONLY on the call where the count crosses FAILURE_ESCALATION_THRESHOLD. */
  noteAndCrossedThreshold(resultJson: string, repair?: RepairReport): boolean {
    const before = this.count;
    const bump = (kind: string, by = 1): void => {
      this.count += by;
      this.types[kind] = (this.types[kind] ?? 0) + by;
    };
    if (resultJson.includes('"error"') && resultJson.includes("search text not found")) {
      bump("search-mismatch");
    }
    if (repair) {
      if (repair.scavenged > 0) bump("scavenged", repair.scavenged);
      if (repair.truncationsFixed > 0) bump("truncated", repair.truncationsFixed);
      if (repair.stormsBroken > 0) bump("repeat-loop", repair.stormsBroken);
    }
    return before < FAILURE_ESCALATION_THRESHOLD && this.count >= FAILURE_ESCALATION_THRESHOLD;
  }

  formatBreakdown(): string {
    const parts = Object.entries(this.types)
      .filter(([, n]) => n > 0)
      .map(([kind, n]) => `${n}× ${kind}`);
    return parts.length > 0 ? parts.join(", ") : `${this.count} repair/error signal(s)`;
  }
}
