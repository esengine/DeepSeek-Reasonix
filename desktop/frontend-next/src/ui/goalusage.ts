import { t } from "../i18n";
import type { GoalRuntime } from "../port/session";

const STATUS: Record<string, string> = {
  running: "plan.step.now",
  complete: "common.done_2",
  blocked: "plan.blocked",
  stopped: "plan.stopped",
};

// What the kernel writes today; an older record may carry a cause not listed
// here, and that one is shown by its code rather than guessed at.
const STOP: Record<string, string> = {
  budget_spend: "plan.the_spend_limit_was_reached",
  goal_stuck: "plan.the_goal_was_not_making",
  evaluator_unavailable: "plan.the_goal_could_not_be",
  manual: "plan.you_stopped_it",
  await_user: "plan.waiting_for_your_answer",
};

const tokens = (n: number): string => (n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${Math.round(n / 100) / 10}k` : String(n));

/** Active work time, rounded the way the terminal and ACP report it. */
export function workTime(ms: number): string {
  if (ms <= 0) return "0s";
  const seconds = Math.max(1, Math.floor((ms + 500) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor((seconds + 30) / 60);
  if (minutes < 60) return `${minutes}m`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m === 0 ? `${h}h` : `${h}h ${m}m`;
}

export const goalStatusText = (status: string): string => (STATUS[status] ? t(STATUS[status]) : status);

export const goalStopText = (cause: string): string => (STOP[cause] ? t(STOP[cause]) : cause);

// The board's one small line: requests, tokens and time, with the rounds only
// once there is one to count. Thousands are K, as everywhere on the board.
export function goalUsageLine(r: GoalRuntime): string {
  const k = (n: number): string => (n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${Math.round(n / 100) / 10}K` : String(n));
  return t("plan.goal.usage_line", { requests: String(r.requestsUsed), tokens: k(r.tokensUsed), time: workTime(r.workDurationMs) });
}

export const goalTone = (status: string): "live" | "ok" | "warn" => (status === "running" ? "live" : status === "complete" ? "ok" : "warn");

export function goalUsageText(r: GoalRuntime): string {
  return t("plan.turns_turns_requests_requests_tokens", {
    turns: String(r.turnsUsed), requests: String(r.requestsUsed), tokens: tokens(r.tokensUsed), time: workTime(r.workDurationMs),
  });
}
