import { ChevronDown, ChevronUp, CornerDownRight, Trash2 } from "lucide-react";
import { guidanceNeedsRetry } from "../lib/composerGuidance";
import { useI18n } from "../lib/i18n";
import type { StructuredInvocationSubmit } from "../lib/invocationDisplay";
import { InboxRecoveryBanner } from "./InboxRecoveryBanner";
import { Tooltip } from "./Tooltip";

export type PendingGuidance = {
  id: string;
  text: string;
  submitText: string;
  state?: string;
  intent?: string;
  source?: string;
  paused?: boolean;
  recoveredCount?: number;
  structured?: StructuredInvocationSubmit;
};

export type InboxRecoveryNotice = {
  draftKey: string;
  tabId: string;
  count: number;
  recovered: boolean;
};

function guidanceIsInFlight(state?: string): boolean {
  return state === "running" || state === "steer_accepted" || state === "steer_consumed";
}

export function ComposerGuidanceShelf({
  recovery,
  recoveryDisabled,
  items,
  expanded,
  running,
  disabled,
  readOnly,
  sendingId,
  onReview,
  onRecoveryResumed,
  onRecoveryError,
  onToggleExpanded,
  onSend,
  onDismiss,
}: {
  recovery: InboxRecoveryNotice | null;
  recoveryDisabled: boolean;
  items: PendingGuidance[];
  expanded: boolean;
  running: boolean;
  disabled: boolean;
  readOnly: boolean;
  sendingId: string | null;
  onReview: () => void;
  onRecoveryResumed: () => void;
  onRecoveryError: (error: unknown) => void;
  onToggleExpanded: () => void;
  onSend: (item: PendingGuidance) => void;
  onDismiss: (item: PendingGuidance) => void;
}) {
  const { t } = useI18n();
  const visible = expanded ? items : items.slice(0, 2);
  const hiddenCount = Math.max(0, items.length - 2);

  return (
    <>
      {recovery && (
        <InboxRecoveryBanner
          key={`${recovery.draftKey}:${recovery.tabId}`}
          count={recovery.count}
          recovered={recovery.recovered}
          disabled={recoveryDisabled}
          tabId={recovery.tabId}
          onReview={onReview}
          onResumed={onRecoveryResumed}
          onError={onRecoveryError}
        />
      )}
      {items.length > 0 && (
        <div className="composer-guidance-shelf" aria-label={t("composer.guidanceQueue")}>
          <div className="composer-guidance-head">
            <span className="composer-guidance-head__label">
              <CornerDownRight size={14} />
              <span>{t("composer.guidanceCount", { n: items.length })}</span>
            </span>
          </div>
          <div className="composer-guidance-list">
            {visible.map((item, index) => {
              const inFlight = guidanceIsInFlight(item.state);
              const needsRetry = guidanceNeedsRetry(item.state);
              const waitingForEarlier = !running && !inFlight && index > 0;
              const actionLabel = inFlight
                ? t("composer.guidanceInFlight")
                : waitingForEarlier
                  ? t("composer.guidanceWaiting")
                  : needsRetry
                    ? t("composer.guidanceRetry")
                    : t("composer.guidanceSend");
              return (
                <div className="composer-guidance-item" key={item.id}>
                  <CornerDownRight size={14} className="composer-guidance-item__icon" />
                  <span className="composer-guidance-item__text">{item.text}</span>
                  <Tooltip label={actionLabel}>
                    <button
                      className="composer-guidance-item__guide"
                      type="button"
                      aria-label={actionLabel}
                      disabled={inFlight || waitingForEarlier || disabled || readOnly || sendingId !== null || (running && !needsRetry && Boolean(item.structured)) || Boolean(item.paused)}
                      onClick={() => onSend(item)}
                    >
                      <CornerDownRight size={13} />
                      <span>{t(needsRetry ? "composer.guidanceRetryMode" : running ? "composer.guidanceMode" : "composer.guidanceSendMode")}</span>
                    </button>
                  </Tooltip>
                  <Tooltip label={inFlight ? actionLabel : t("composer.guidanceDismiss")}>
                    <button
                      className="composer-guidance-item__action"
                      type="button"
                      aria-label={inFlight ? actionLabel : t("composer.guidanceDismiss")}
                      disabled={inFlight || sendingId === item.id}
                      onClick={() => onDismiss(item)}
                    >
                      <Trash2 size={14} />
                    </button>
                  </Tooltip>
                </div>
              );
            })}
            {items.length > 2 && (
              <button
                className="composer-guidance-more"
                type="button"
                aria-expanded={expanded}
                onClick={onToggleExpanded}
              >
                {expanded ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
                <span>{expanded ? t("composer.guidanceCollapse") : t("composer.guidanceRemaining", { n: hiddenCount })}</span>
              </button>
            )}
          </div>
        </div>
      )}
    </>
  );
}
