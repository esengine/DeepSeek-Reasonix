import { t } from "../i18n";
import type { FeedbackTab } from "./Feedback";

/** Every entry to feedback lands on the list when something wants attention. */
export const feedbackEntryTab = (unread: number): FeedbackTab => (unread > 0 ? "mine" : "send");

export function FeedbackBadge({ unread }: { unread: number }) {
  if (unread <= 0) return null;
  return (
    <>
      <i className="fbk-badge" aria-hidden="true" title={t("common.n_to_review", { n: unread })}>{unread > 9 ? "9+" : unread}</i>
      <span className="sr-only">{t("common.n_to_review", { n: unread })}</span>
    </>
  );
}
