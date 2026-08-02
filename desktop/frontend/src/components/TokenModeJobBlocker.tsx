import { Activity, Loader2 } from "lucide-react";
import type { Translator } from "../lib/i18n";

interface Props {
  t: Translator;
  count: number;
  busy: boolean;
  error?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * Keeps the recovery action next to the work-mode control. Cancelling running
 * jobs is destructive, so it remains an explicit user decision; once accepted,
 * the caller completes the originally requested mode switch automatically.
 */
export function TokenModeJobBlocker({ t, count, busy, error, onConfirm, onCancel }: Props) {
  return (
    <div className="composer-recovery" role="status" aria-live="polite">
      <Activity size={14} aria-hidden="true" />
      <span className="composer-recovery__copy">
        <strong>{t("status.tokenModeJobsBlocked", { n: count })}</strong>
        {error && <small>{error}</small>}
      </span>
      <button type="button" className="btn btn--small btn--primary" disabled={busy} onClick={onConfirm}>
        {busy && <Loader2 size={12} className="spin" aria-hidden="true" />}
        {busy ? t("status.tokenModeStoppingAndSwitching") : t("status.tokenModeStopAndSwitch")}
      </button>
      <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
        {t("common.cancel")}
      </button>
    </div>
  );
}
