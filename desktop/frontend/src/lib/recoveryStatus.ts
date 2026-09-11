import type { Translator } from "./i18n";

export interface RecoveryStatus {
  state?: "recovery_required" | string;
  call_id?: string;
  attempt_id?: string;
  requires_user_decision?: boolean;
  read_only?: boolean;
  phase?: string;
  reason?: string;
  next_attempt_at?: number;
  waited_ms?: number;
  wait_budget_ms?: number;
  waiting?: boolean;
}

export interface RecoveryRetry {
  attempt: number;
  max: number;
  recovery?: RecoveryStatus;
}

export interface RecoveryEventFields {
  recovery?: RecoveryStatus;
  retryAttempt?: number;
  retryMax?: number;
}

export function recoveryNextAttemptSeconds(recovery: RecoveryStatus, now: number): number {
  return Math.max(0, Math.ceil(((recovery.next_attempt_at ?? now) - now) / 1000));
}

export function recoveryStatusText(t: Translator, retry: RecoveryRetry, now: number): string {
  if (!retry.recovery?.waiting) return t("status.retrying", { attempt: retry.attempt, max: retry.max });
  const phase = t(retry.recovery.phase === "connect" ? "status.recoveryNetwork" : "status.recoveryProvider");
  return t("status.recoveryWaiting", { seconds: recoveryNextAttemptSeconds(retry.recovery, now), phase });
}
