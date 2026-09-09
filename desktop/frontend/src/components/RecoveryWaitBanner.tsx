import { Square } from "lucide-react";
import { useI18n } from "../lib/i18n";
import { recoveryNextAttemptSeconds, type RecoveryRetry } from "../lib/recoveryStatus";

interface RecoveryWaitBannerProps {
  retry: RecoveryRetry;
  now: number;
  onStop: () => void;
  stopDisabled?: boolean;
}

export function RecoveryWaitBanner({ retry, now, onStop, stopDisabled = false }: RecoveryWaitBannerProps) {
  const { t } = useI18n();
  const recovery = retry.recovery;
  if (!recovery?.waiting) return null;
  const title = t(recovery.phase === "connect" ? "status.recoveryWaitTitleNetwork" : "status.recoveryWaitTitleProvider");
  const waited = Math.max(0, Math.floor((recovery.waited_ms ?? 0) / 60_000));
  const budget = Math.max(0, Math.round((recovery.wait_budget_ms ?? 0) / 60_000));
  return (
    <div className="recovery-wait-banner" role="region" aria-label={title} data-phase={recovery.phase ?? ""}>
      <div className="recovery-wait-banner__body">
        <div className="recovery-wait-banner__title">{title}</div>
        <div className="recovery-wait-banner__meta">
          <span className="recovery-wait-banner__countdown">
            {t("status.recoveryWaitNext", { seconds: recoveryNextAttemptSeconds(recovery, now) })}
          </span>
          {budget > 0 && (
            <span className="recovery-wait-banner__progress">{t("status.recoveryWaitProgress", { waited, budget })}</span>
          )}
          {recovery.reason && <code className="recovery-wait-banner__code">{recovery.reason}</code>}
        </div>
        <div className="recovery-wait-banner__hint">{t("status.recoveryWaitHint")}</div>
      </div>
      <button type="button" className="recovery-wait-banner__stop" onClick={onStop} disabled={stopDisabled}>
        <Square size={12} fill="currentColor" aria-hidden="true" />
        {t("status.recoveryWaitStop")}
      </button>
    </div>
  );
}
