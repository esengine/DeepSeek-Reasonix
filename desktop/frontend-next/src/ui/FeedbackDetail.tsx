import { useState, type ReactNode } from "react";
import { t } from "../i18n";
import { canWithdraw, type FeedbackItem, type FeedbackLimits } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { CopyButton } from "./CopyButton";
import { snippet, STEP_STATE, StatusChips, steps } from "./feedbackstatus";
import { FeedbackReplyBox, FeedbackReviewNote, FeedbackThread } from "./FeedbackThread";
import { FeedbackWithdraw } from "./FeedbackWithdraw";
import { StudioIcon } from "./StudioIcon";

interface Props {
  port: AgentPort;
  item: FeedbackItem;
  level: number | null;
  fresh: number;
  limits: FeedbackLimits | null;
  offline: boolean;
  issue: (n: number) => ReactNode;
  onFile: (url: string) => void;
  onSent: (receipt: string) => void;
  onWithdrawn: () => void;
  onStale: () => void;
  drafts: Map<string, string>;
}

/** One report on its own page: where it stands, the whole exchange, and the box to answer in. */
export function FeedbackDetail({ port, item, level, fresh, limits, offline, issue, onFile, onSent, onWithdrawn, onStale, drafts }: Props) {
  const [more, setMore] = useState(false);
  return (
    <section className="fbk-detail" aria-label={item.titleSnippet} data-receipt={item.receipt} data-status={item.status} data-stale={item.statusUnavailable ? "" : undefined}>
      <header className="fbk-detail-hd">
        <div className="fbk-detail-line">
          <h3 className="fbk-detail-title" dir="auto">{snippet(item.titleSnippet)}</h3>
          <StatusChips item={item} />
          {item.otherInstall && <span className="fbk-chip" data-device="other">{t("feedback.from_another_device")}</span>}
          <span className="fbk-detail-fill" />
          <code className="fbk-code">{item.receipt}</code>
          <CopyButton iconOnly text={item.receipt} label={t("feedback.copy_receipt_number")} />
          {canWithdraw(item) && (
            <span className="fbk-more">
              <button type="button" className="fbk-icon" data-action="feedback.more" aria-expanded={more} aria-label={t("feedback.more_actions")} title={t("feedback.more_actions")} onClick={() => setMore(!more)}>
                <StudioIcon name="more" />
              </button>
              {more && (
                <div className="fbk-more-menu">
                  <FeedbackWithdraw port={port} item={item} onDone={onWithdrawn} onStale={onStale} />
                </div>
              )}
            </span>
          )}
        </div>
        {!item.statusUnavailable && (
          <ol className="fbk-tl" aria-label={t("feedback.progress")}>
            {steps(item, issue).map((s) => (
              <li key={s.id} data-state={s.state} data-tone={s.neutral ? "neutral" : undefined} aria-current={s.state === "current" ? "step" : undefined}>
                <i aria-hidden="true" />
                <span className="sr-only">{t(STEP_STATE[s.state])}</span>
                <span>{s.label}</span>
              </li>
            ))}
          </ol>
        )}
      </header>
      <div className="fbk-detail-body">
        <FeedbackReviewNote item={item} />
        <FeedbackThread item={item} level={level} fresh={fresh} />
      </div>
      <FeedbackReplyBox key={item.receipt} port={port} item={item} limit={limits?.replyBytes ?? null} limits={limits} offline={offline} onSent={onSent} onFile={onFile} onStale={onStale} drafts={drafts} />
    </section>
  );
}
