import type { ReactNode } from "react";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

/** One turn's intermediate work under a single line. The body is always
 *  rendered, closed or not: find, copy and the graph's landing reach into it,
 *  and opening it must not remount what was already drawn. */
export function WorkFold({ open, onOpen, calls, failures, words, children }: {
  open: boolean;
  onOpen: (next: boolean) => void;
  calls: number;
  failures: number;
  words: number;
  children: ReactNode;
}) {
  return (
    <details className="activity-group work-fold" data-failed={failures ? "" : undefined} open={open} onToggle={(e) => e.currentTarget.open !== open && onOpen(e.currentTarget.open)}>
      <summary>
        <StudioIcon name={failures ? "warning" : "check"} className="activity-status-icon" />
        <span className="activity-title">{t("chat.work_in_between")}</span>
        {failures > 0 && <span className="activity-errors">{t("chat.n_failed", { n: failures })}</span>}
        <span className="activity-count">{calls > 0 ? t("chat.count_actions", { count: calls }) : t("chat.thinking_and_notes")}</span>
        {words > 0 && calls > 0 && <span className="activity-count">{t("chat.notes_count", { count: words })}</span>}
        <StudioIcon name="down" className="activity-fold-icon" />
      </summary>
      <div className="activity-body work-fold-body">{children}</div>
    </details>
  );
}
