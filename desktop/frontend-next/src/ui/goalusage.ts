import { t } from "../i18n";
import type { GoalRuntime } from "../port/session";

const STATUS: Record<string, string> = {
  running: "进行中",
  complete: "已完成",
  blocked: "已阻塞",
  stopped: "已停止",
};

// What the kernel writes today; an older record may carry a cause not listed
// here, and that one is shown by its code rather than guessed at.
const STOP: Record<string, string> = {
  budget_spend: "达到花费上限",
  goal_stuck: "目标没有进展",
  evaluator_unavailable: "无法评估目标是否完成",
  manual: "你手动停止",
  await_user: "在等你回答",
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

export function goalUsageText(r: GoalRuntime): string {
  return t("plan.turns_turns_requests_requests_tokens", {
    turns: String(r.turnsUsed), requests: String(r.requestsUsed), tokens: tokens(r.tokensUsed), time: workTime(r.workDurationMs),
  });
}
