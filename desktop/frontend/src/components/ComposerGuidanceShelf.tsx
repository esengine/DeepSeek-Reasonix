import { useState } from "react";
import { Check, ChevronDown, ChevronUp, CornerDownRight, Pencil, Trash2, X } from "lucide-react";
import { guidanceHasKnownPendingState, guidanceIsEditable, guidanceIsInFlight, guidanceNeedsRetry } from "../lib/composerGuidance";
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
  onEdit,
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
  onEdit?: (item: PendingGuidance, text: string) => void | Promise<void>;
}) {
  const { t } = useI18n();
  const visible = expanded ? items : items.slice(0, 2);
  const hiddenCount = Math.max(0, items.length - 2);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState("");
  const [editBusy, setEditBusy] = useState(false);

  const startEdit = (item: PendingGuidance) => {
    setEditingId(item.id);
    setEditDraft(item.submitText.trim() || item.text);
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditDraft("");
    setEditBusy(false);
  };

  const saveEdit = async (item: PendingGuidance) => {
    if (!onEdit || editBusy) return;
    const next = editDraft.trim();
    if (!next) return;
    setEditBusy(true);
    try {
      await onEdit(item, next);
      cancelEdit();
    } catch {
      setEditBusy(false);
    }
  };

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
              const unknownState = !guidanceHasKnownPendingState(item.state);
              const needsRetry = guidanceNeedsRetry(item.state);
              const waitingForEarlier = !running && !inFlight && index > 0;
              const canEdit = Boolean(onEdit) && !readOnly && !disabled && guidanceIsEditable(item) && !waitingForEarlier && sendingId === null;
              const editing = editingId === item.id;
              const actionLabel = inFlight
                ? t("composer.guidanceInFlight")
                : waitingForEarlier
                  ? t("composer.guidanceWaiting")
                  : needsRetry
                    ? t("composer.guidanceRetry")
                    : t("composer.guidanceSend");
              return (
                <div className={`composer-guidance-item${editing ? " composer-guidance-item--editing" : ""}`} key={item.id}>
                  <CornerDownRight size={14} className="composer-guidance-item__icon" />
                  {editing ? (
                    <input
                      className="composer-guidance-item__editor"
                      value={editDraft}
                      onChange={(event) => setEditDraft(event.target.value)}
                      aria-label={t("composer.guidanceEdit")}
                      disabled={editBusy}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          void saveEdit(item);
                        }
                        if (event.key === "Escape") {
                          event.preventDefault();
                          cancelEdit();
                        }
                      }}
                    />
                  ) : (
                    <span className="composer-guidance-item__text">{item.text.trim() || t("composer.guidanceEmptyPreview")}</span>
                  )}
                  {editing ? (
                    <>
                      <Tooltip label={t("composer.guidanceSaveEdit")}>
                        <button
                          className="composer-guidance-item__action"
                          type="button"
                          aria-label={t("composer.guidanceSaveEdit")}
                          disabled={editBusy || !editDraft.trim()}
                          onClick={() => void saveEdit(item)}
                        >
                          <Check size={14} />
                        </button>
                      </Tooltip>
                      <Tooltip label={t("composer.guidanceCancelEdit")}>
                        <button
                          className="composer-guidance-item__action"
                          type="button"
                          aria-label={t("composer.guidanceCancelEdit")}
                          disabled={editBusy}
                          onClick={cancelEdit}
                        >
                          <X size={14} />
                        </button>
                      </Tooltip>
                    </>
                  ) : (
                    <>
                      {canEdit && (
                        <Tooltip label={t("composer.guidanceEdit")}>
                          <button
                            className="composer-guidance-item__action"
                            type="button"
                            aria-label={t("composer.guidanceEdit")}
                            onClick={() => startEdit(item)}
                          >
                            <Pencil size={14} />
                          </button>
                        </Tooltip>
                      )}
                      <Tooltip label={actionLabel}>
                        <button
                          className="composer-guidance-item__guide"
                          type="button"
                          aria-label={actionLabel}
                          disabled={inFlight || unknownState || waitingForEarlier || disabled || readOnly || sendingId !== null || (running && !needsRetry && Boolean(item.structured)) || Boolean(item.paused)}
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
                          disabled={disabled || readOnly || inFlight || unknownState || sendingId === item.id}
                          onClick={() => onDismiss(item)}
                        >
                          <Trash2 size={14} />
                        </button>
                      </Tooltip>
                    </>
                  )}
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
