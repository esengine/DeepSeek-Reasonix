export const RESTART_BUDGET_MAX = 3;
export const RESTART_BUDGET_WINDOW_MS = 5 * 60 * 1000;

export class RestartBudget {
  private stamps: number[] = [];

  constructor(private readonly max = RESTART_BUDGET_MAX, private readonly windowMs = RESTART_BUDGET_WINDOW_MS) {}

  allow(now = Date.now()): boolean {
    this.stamps = this.stamps.filter((stamp) => now - stamp < this.windowMs);
    if (this.stamps.length >= this.max) return false;
    this.stamps.push(now);
    return true;
  }
}
