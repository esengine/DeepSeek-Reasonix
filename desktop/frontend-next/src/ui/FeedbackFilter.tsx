import { t } from "../i18n";
import type { FeedbackItem } from "../port/feedback";
import { FEEDBACK_GROUPS, inGroup, type FeedbackGroup } from "./feedbackgroups";

const LABEL: Record<FeedbackGroup, string> = {
  all: "common.all", fresh: "feedback.new_replies", input: "feedback.needs_your_reply", open: "feedback.open", done: "feedback.closed",
};

/** One chip per group, each carrying how many reports it holds. */
export function FeedbackFilter({ items, fresh, group, onPick }: {
  items: FeedbackItem[];
  fresh: Record<string, number>;
  group: FeedbackGroup;
  onPick: (to: FeedbackGroup) => void;
}) {
  return (
    <div className="fbk-filter" role="group" aria-label={t("feedback.filter_by_progress")}>
      {FEEDBACK_GROUPS.map((g) => {
        const n = items.filter((i) => inGroup(i, g, fresh[i.receipt] ?? 0)).length;
        return (
          <button key={g} type="button" className="fbk-filter-chip" data-action="feedback.filter" data-value={g}
            data-live={g === "fresh" && n > 0 ? "" : undefined} aria-pressed={group === g} onClick={() => onPick(g)}>
            {t(LABEL[g])}<small>{n}</small>
          </button>
        );
      })}
    </div>
  );
}
