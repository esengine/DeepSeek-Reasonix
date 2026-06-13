import { useEffect, useRef, useState } from "react";
import gsap from "gsap";
import { useT } from "../lib/i18n";
import type { WireApproval } from "../lib/types";
import { PromptAction, PromptDetailToggle, PromptShelf } from "./PromptShelf";
import { playAttentionChime } from "../lib/sound";
import { DUR_FAST } from "../lib/gsapAnimations";

export function ApprovalModal({
  approval,
  onAnswer,
  onRevisePlan,
  onExitPlan,
}: {
  approval: WireApproval;
  onAnswer: (allow: boolean, session: boolean, persist: boolean) => void;
  onRevisePlan?: (text: string) => void;
  onExitPlan?: () => void;
}) {
  const t = useT();
  const [revisionOpen, setRevisionOpen] = useState(false);
  const [revisionText, setRevisionText] = useState("");
  const [detailsOpen, setDetailsOpen] = useState(false);
  const cardRef = useRef<HTMLDivElement | null>(null);
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  // When consecutive approvals arrive, animate the old card out before
  // the new one slides in.  GSAP fromTo on the shelf wrapper avoids the
  // jarring pop when the API cycles through 4+ pending approvals.
  const closingRef = useRef(false);
  const isPlanApproval = approval.tool === "exit_plan_mode";
  const subject = approval.subject.trim();
  const subjectSummary = subject.split("\n").find((line) => line.trim())?.trim() ?? "";


  const answerWithExit = (fn: () => void) => {
    if (closingRef.current) return;
    closingRef.current = true;
    const el = shelfRef.current;
    if (el) {
      gsap.to(el, {
        opacity: 0,
        y: 8,
        duration: DUR_FAST,
        ease: "power2.in",
        onComplete: fn,
      });
    } else {
      fn();
    }
  };

  const choosePlanAction = (key: string) => {
    if (key === "1") setRevisionOpen((open) => !open);
    else if (key === "2") answerWithExit(() => onAnswer(true, false, false));
    else if (key === "3" || key === "Escape") answerWithExit(() => (onExitPlan ?? (() => onAnswer(false, false, false)))());
  };

  const chooseToolAction = (key: string) => {
    if (key === "1") answerWithExit(() => onAnswer(true, false, false));
    else if (key === "2") answerWithExit(() => onAnswer(true, true, false));
    else if (key === "3") answerWithExit(() => onAnswer(true, true, true));
    else if (key === "4" || key === "Escape") answerWithExit(() => onAnswer(false, false, false));
  };

  useEffect(() => {
    cardRef.current?.focus();
    setRevisionOpen(false);
    setRevisionText("");
    setDetailsOpen(false);
    playAttentionChime();
  }, [approval.id]);

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || target?.isContentEditable) return;
      if (event.key !== "1" && event.key !== "2" && event.key !== "3" && event.key !== "4" && event.key !== "Escape") return;
      event.preventDefault();
      if (isPlanApproval) choosePlanAction(event.key);
      else chooseToolAction(event.key);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [isPlanApproval, onAnswer, onExitPlan]);

  useEffect(() => {
    if (revisionOpen) inputRef.current?.focus();
  }, [revisionOpen]);

  const submitRevision = () => {
    const text = revisionText.trim();
    if (!text) {
      inputRef.current?.focus();
      return;
    }
    answerWithExit(() => onRevisePlan?.(text));
  };

  // The plan is already shown above as the assistant's reply; this is just the gate.
  if (isPlanApproval) {
    return (
      <div ref={shelfRef}>
        <PromptShelf
          barRef={cardRef}
        titleId="plan-approval-title"
        title={t("approval.planReady")}
        meta={t("approval.planReadyHint")}
        actions={
          <>
            <PromptAction keyLabel="1" label={t("approval.revisePlan")} onClick={() => setRevisionOpen((open) => !open)} />
            <PromptAction keyLabel="2" label={t("approval.startExecution")} onClick={() => answerWithExit(() => onAnswer(true, false, false))} selected />
            <PromptAction
              keyLabel="3"
              label={t("approval.exitPlan")}
              onClick={() => answerWithExit(() => (onExitPlan ?? (() => onAnswer(false, false, false)))())}
            />
          </>
        }
      >
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
      </PromptShelf>
      </div>
    );
  }

  return (
    <div ref={shelfRef}>
      <PromptShelf
        barRef={cardRef}
      titleId="tool-approval-title"
      title={t("approval.toolPending")}
      actionsWrap
      meta={
        <>
          <span className="tool__name">{approval.tool}</span>
          {subjectSummary && <span className="prompt-shelf__subject"> · {subjectSummary}</span>}
        </>
      }
      actions={
        <>
          {subject && (
            <PromptDetailToggle
              open={detailsOpen}
              label={t("approval.details")}
              openLabel={t("approval.hideDetails")}
              onClick={() => setDetailsOpen((open) => !open)}
            />
          )}
          <PromptAction keyLabel="1" label={t("approval.allowOnce")} onClick={() => answerWithExit(() => onAnswer(true, false, false))} selected />
          <PromptAction keyLabel="2" label={t("approval.allowRuleSession")} onClick={() => answerWithExit(() => onAnswer(true, true, false))} />
          <PromptAction keyLabel="3" label={t("approval.allowRulePersistent")} onClick={() => answerWithExit(() => onAnswer(true, true, true))} />
          <PromptAction keyLabel="4" label={t("approval.deny")} onClick={() => answerWithExit(() => onAnswer(false, false, false))} />
        </>
      }
    >
      {detailsOpen && subject && (
        <pre className="approval-subject">{subject}</pre>
      )}
    </PromptShelf>
    </div>
  );

}
