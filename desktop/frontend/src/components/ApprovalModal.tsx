import { useEffect, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { WireApproval } from "../lib/types";

export function ApprovalModal({
  approval,
  onAnswer,
  onRevisePlan,
}: {
  approval: WireApproval;
  onAnswer: (allow: boolean, session: boolean) => void;
  onRevisePlan?: (text: string) => void;
}) {
  const t = useT();
  const [revisionOpen, setRevisionOpen] = useState(false);
  const [revisionText, setRevisionText] = useState("");
  const cardRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const isPlanApproval = approval.tool === "exit_plan_mode";

  const choosePlanAction = (key: string) => {
    if (key === "1") onAnswer(false, false);
    else if (key === "2") onAnswer(true, false);
    else if (key === "3") setRevisionOpen((open) => !open);
    else if (key === "Escape") onAnswer(false, false);
  };

  const chooseToolAction = (key: string) => {
    if (key === "1" || key === "Escape") onAnswer(false, false);
    else if (key === "2") onAnswer(true, false);
    else if (key === "3") onAnswer(true, true);
  };

  useEffect(() => {
    cardRef.current?.focus();
  }, [approval.id]);

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || target?.isContentEditable) return;
      if (event.key !== "1" && event.key !== "2" && event.key !== "3" && event.key !== "Escape") return;
      event.preventDefault();
      if (isPlanApproval) choosePlanAction(event.key);
      else chooseToolAction(event.key);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [isPlanApproval, onAnswer]);

  useEffect(() => {
    if (revisionOpen) inputRef.current?.focus();
  }, [revisionOpen]);

  const submitRevision = () => {
    const text = revisionText.trim();
    if (!text) {
      inputRef.current?.focus();
      return;
    }
    onRevisePlan?.(text);
  };

  // The plan is already shown above as the assistant's reply; this is just the gate.
  if (isPlanApproval) {
    return (
      <div className="plan-approval-dock" aria-live="polite">
        <div
          ref={cardRef}
          className="plan-approval-card"
          role="dialog"
          aria-modal="false"
          aria-labelledby="plan-approval-title"
          tabIndex={-1}
        >
          <div className="plan-approval-card__header">
            <div>
              <div id="plan-approval-title" className="plan-approval-card__title">
                {t("approval.planTitle")}
              </div>
              <div className="plan-approval-card__note">{t("approval.planNote")}</div>
            </div>
          </div>
          <div className="plan-approval-card__choices">
            <button className="plan-choice" onClick={() => onAnswer(false, false)}>
              <span className="plan-choice__key">1</span>
              <span className="plan-choice__copy">
                <span className="plan-choice__label">{t("approval.keepPlanning")}</span>
                <span className="plan-choice__hint">{t("approval.keepPlanningHint")}</span>
              </span>
            </button>
            <button className="plan-choice plan-choice--primary" onClick={() => onAnswer(true, false)}>
              <span className="plan-choice__key">2</span>
              <span className="plan-choice__copy">
                <span className="plan-choice__label">{t("approval.startExecution")}</span>
                <span className="plan-choice__hint">{t("approval.startExecutionHint")}</span>
              </span>
            </button>
            <button className="plan-choice" onClick={() => setRevisionOpen((open) => !open)}>
              <span className="plan-choice__key">3</span>
              <span className="plan-choice__copy">
                <span className="plan-choice__label">{t("approval.revisePlan")}</span>
                <span className="plan-choice__hint">{t("approval.revisePlanHint")}</span>
              </span>
            </button>
          </div>
          {revisionOpen && (
            <div className="plan-revision">
              <textarea
                ref={inputRef}
                className="plan-revision__input"
                value={revisionText}
                rows={3}
                placeholder={t("approval.revisePlanPlaceholder")}
                onChange={(event) => setRevisionText(event.target.value)}
                onKeyDown={(event) => {
                  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") submitRevision();
                  event.stopPropagation();
                }}
              />
              <div className="plan-revision__actions">
                <button className="btn" onClick={() => setRevisionOpen(false)}>
                  {t("common.cancel")}
                </button>
                <button className="btn btn--primary" onClick={submitRevision}>
                  {t("approval.sendRevision")}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="plan-approval-dock" aria-live="polite">
      <div
        ref={cardRef}
        className="plan-approval-card"
        role="dialog"
        aria-modal="false"
        aria-labelledby="tool-approval-title"
        tabIndex={-1}
      >
        <div className="plan-approval-card__header">
          <div>
            <div id="tool-approval-title" className="plan-approval-card__title">
              {t("approval.toolTitle")}
            </div>
            <div className="plan-approval-card__note">{t("approval.toolNote")}</div>
          </div>
        </div>
        <div className="approval-tool">
          <span className="approval-tool__label">{t("approval.toolLabel")}</span>
          <span className="tool__name">{approval.tool}</span>
        </div>
        {approval.subject && <pre className="approval-subject">{approval.subject}</pre>}
        <div className="plan-approval-card__choices">
          <button className="plan-choice" onClick={() => onAnswer(false, false)}>
            <span className="plan-choice__key">1</span>
            <span className="plan-choice__copy">
              <span className="plan-choice__label">{t("approval.deny")}</span>
              <span className="plan-choice__hint">{t("approval.denyHint")}</span>
            </span>
          </button>
          <button className="plan-choice plan-choice--primary" onClick={() => onAnswer(true, false)}>
            <span className="plan-choice__key">2</span>
            <span className="plan-choice__copy">
              <span className="plan-choice__label">{t("approval.allowOnce")}</span>
              <span className="plan-choice__hint">{t("approval.allowOnceHint")}</span>
            </span>
          </button>
          <button className="plan-choice" onClick={() => onAnswer(true, true)}>
            <span className="plan-choice__key">3</span>
            <span className="plan-choice__copy">
              <span className="plan-choice__label">{t("approval.allowSession")}</span>
              <span className="plan-choice__hint">{t("approval.allowSessionHint")}</span>
            </span>
          </button>
        </div>
      </div>
    </div>
  );
}
