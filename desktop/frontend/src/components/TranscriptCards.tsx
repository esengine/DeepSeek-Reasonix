// Small transcript row cards: phase lines, steer bubbles, notice cards with
// decision receipts, and compaction cards.

import { useState } from "react";
import { CheckCheck, ChevronRight, CirclePlay, ClipboardCheck, FileSearch, Info, TriangleAlert } from "lucide-react";
import { useT } from "../lib/i18n";
import type { CompactionItem, NoticeItem } from "../lib/transcriptRows";
import type { WireCompletionSummary } from "../lib/types";
import { STEER_NOTICE_PREFIX } from "../lib/useController";
import { ProcessCompactIcon, ProcessPhaseIcon } from "./ProcessCard";
import { useTranscriptUserResizeIntent } from "./TranscriptLayoutIntentContext";

export function PhaseCard({ id, text }: { id: string; text: string }) {
  return <div className="phase" data-entrance={id}><ProcessPhaseIcon size={12} /><span>{text}</span></div>;
}

// A mid-turn steer is the user's own message, so it renders on the user side
// of the transcript instead of disappearing into the work fold.
export function SteerCard({ id, text }: { id: string; text: string }) {
  const t = useT();
  const body = text.startsWith(STEER_NOTICE_PREFIX) ? text.slice(STEER_NOTICE_PREFIX.length) : text;
  return (
    <div className="steer-line" data-entrance={id}>
      <div className="steer-line__bubble" title={t("transcript.steer")}>
        <span className="steer-line__icon" aria-hidden="true">↪</span>
        <span className="steer-line__text">{body}</span>
      </div>
    </div>
  );
}

function DecisionReceiptLine({ receipt }: { receipt: NonNullable<NoticeItem["decisionReceipt"]> }) {
  const t = useT();
  const titleKey = receipt.kind === "ask"
    ? "notice.decisionReceiptAsk"
    : receipt.kind === "plan"
    ? "notice.decisionReceiptPlan"
    : receipt.kind === "recovery"
    ? "notice.decisionReceiptRecovery"
    : "notice.decisionReceiptTool";
  const outcomeKeys: Record<string, string> = {
    allow_once: "notice.decisionAllowOnce",
    allow_session: "notice.decisionAllowSession",
    allow_persistent: "notice.decisionAllowPersistent",
    deny: "notice.decisionDeny",
    start_execution: "notice.decisionStartExecution",
    revise_plan: "notice.decisionRevisePlan",
    exit_plan: "notice.decisionExitPlan",
    recovery_continue: "notice.decisionRecoveryContinue",
    recovery_continue_task: "notice.decisionRecoveryContinueTask",
    recovery_revise: "notice.decisionRecoveryRevise",
    answered: "notice.decisionAnswered",
  };
  const outcome = outcomeKeys[receipt.outcome]
    ? t(outcomeKeys[receipt.outcome] as never)
    : receipt.outcome || t("notice.decisionReceiptTitle");
  const showOutcome = receipt.kind !== "ask" || receipt.outcome !== "answered";
  return (
    <div className="notice-line__decision-receipt">
      <span className="notice-line__decision-title">{t(titleKey as never)}</span>
      {showOutcome && <span className="notice-line__decision-outcome">{outcome}</span>}
      {receipt.tool && <code>{receipt.tool}</code>}
      {receipt.subject && <span className="notice-line__decision-subject">{receipt.subject}</span>}
    </div>
  );
}

export function NoticeCard({ item, onAction, onAccept, onOpenVerification, actionDisabled = false }: { item: NoticeItem; onAction?: () => void; onAccept?: () => void; onOpenVerification?: (summary: WireCompletionSummary) => void; actionDisabled?: boolean }) {
  const t = useT();
  const StatusIcon = item.level === "warn" ? TriangleAlert : Info;
  const ActionIcon = item.action === "open_changes" ? FileSearch : CirclePlay;
  const showVerification = item.variant === "completion" && Boolean(item.completionSummary && onOpenVerification);
  const showActions = Boolean((item.action && onAction) || onAccept || showVerification);
  return (
    <div className={`notice-line notice-line--${item.level}${item.variant ? ` notice-line--${item.variant}` : ""}`} data-entrance={item.id}>
      <StatusIcon className="notice-line__icon" size={14} aria-hidden="true" />
      <div className="notice-line__text">
        {item.decisionReceipt ? (
          <DecisionReceiptLine receipt={item.decisionReceipt} />
        ) : (
          <>
            {item.title ? <div className="notice-line__title">{item.title}</div> : null}
            <div className="notice-line__body">{item.text}</div>
          </>
        )}
        {showActions ? (
          <div className="notice-line__actions">
            {item.action && onAction ? (
              <button className="btn btn--small" type="button" onClick={onAction} disabled={actionDisabled}>
                <ActionIcon size={13} aria-hidden="true" />
                <span>{item.action === "open_changes" ? t("notice.completionViewChanges") : t("notice.deliveryIncompleteContinue")}</span>
              </button>
            ) : null}
            {showVerification ? (
              <button className="btn btn--small" type="button" onClick={() => item.completionSummary && onOpenVerification?.(item.completionSummary)}>
                <ClipboardCheck size={13} aria-hidden="true" />
                <span>{t("notice.completionViewVerification")}</span>
              </button>
            ) : null}
            ) : null}
            {onAccept ? (
              <button className="btn btn--small" type="button" onClick={onAccept} disabled={actionDisabled}>
                <CheckCheck size={13} aria-hidden="true" />
                <span>{t("notice.deliveryIncompleteAccept")}</span>
              </button>
            ) : null}
          </div>
        ) : null}
        {item.detail ? (
          <details className="notice-line__details">
            <summary>{t("notice.details")}</summary>
            <div>{item.detail}</div>
          </details>
        ) : null}
      </div>
    </div>
  );
}

export function CompactionCard({ item }: { item: CompactionItem }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const beginUserResize = useTranscriptUserResizeIntent();
  if (item.pending) {
    return <div className="compaction compaction--pending" data-entrance={item.id} data-transcript-layout-variant="static"><ProcessCompactIcon size={12} /><span>{t("compaction.working")}</span></div>;
  }
  return (
    <div className="compaction" data-entrance={item.id} data-transcript-layout-variant={open ? "compaction-expanded" : "compaction-collapsed"}>
      <button type="button" className="compaction__head" onClick={() => { beginUserResize(); setOpen((v) => !v); }} aria-expanded={open}>
        <ProcessCompactIcon size={12} />
        <span>{t("compaction.title")}</span>
        <span className="compaction__meta">{t("compaction.messages", { n: item.messages })}{item.trigger ? ` · ${item.trigger}` : ""}</span>
        <ChevronRight className={open ? "compaction__chevron--open" : ""} size={12} />
      </button>
      {open && <pre className="compaction__body">{item.summary}</pre>}
    </div>
  );
}
