import type { ReactNode } from "react";
import { current, t } from "../i18n";
import { tx } from "../i18n/rich";
import { FEEDBACK_NEXT_VERSION, isUnderReview, type FeedbackItem, type FeedbackStatus } from "../port/feedback";

export const STATUS_LABEL: Record<FeedbackStatus, string> = {
  received: "feedback.received",
  needs_info: "feedback.needs_more_info",
  answered: "feedback.answered_by_us",
  closed: "feedback.closed_2",
  withdrawn: "chat.withdrawn",
  recorded: "feedback.recorded",
  in_progress: "feedback.in_progress",
  fixed: "feedback.fixed",
  wontfix: "feedback.wont_fix",
  duplicate: "feedback.duplicate",
};

const SNIPPET_CUT = 80;

/** The title as the service sent it, marked when the service cut it. */
export function snippet(text: string): string {
  return [...text].length >= SNIPPET_CUT ? `${text.trimEnd()}…` : text;
}

export const REVIEW_LABEL = "feedback.in_review";

export const CATEGORY_LABEL = { bug: "feedback.bug", idea: "feedback.idea", question: "feedback.question", other: "ext.other" } as const;

type StepState = "done" | "current" | "todo";

export const STEP_STATE: Record<StepState, string> = { done: "feedback.done", current: "feedback.current", todo: "feedback.not_yet" };

export interface Step {
  id: string;
  label: ReactNode;
  state: StepState;
  neutral?: boolean;
}

const locale = () => (current() === "zh" ? "zh-CN" : "en");
const sameDay = (a: Date, b: Date) => a.toDateString() === b.toDateString();
const pad = (n: number) => String(n).padStart(2, "0");

/** A date as the list reads it: today, else month and day. */
export function shortDay(iso: string, now = new Date()): string {
  const d = new Date(iso);
  if (sameDay(d, now)) return t("feedback.today");
  return current() === "zh" ? `${pad(d.getMonth() + 1)}-${pad(d.getDate())}` : d.toLocaleDateString(locale(), { month: "short", day: "numeric" });
}

/** A message's time: its day as the list reads it, then the clock. */
export function stamp(iso: string, now = new Date()): string {
  const d = new Date(iso);
  return `${shortDay(iso, now)} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

const fixedWord = (item: FeedbackItem) => (item.resolvedVersion && item.resolvedVersion !== FEEDBACK_NEXT_VERSION
  ? t("feedback.fixed_in_version", { version: item.resolvedVersion })
  : t("feedback.fixed_next_release"));

/** Where a report stands, in one word or two. */
export function statusWord(item: FeedbackItem): string {
  if (item.statusUnavailable) return t("feedback.status_no_longer_trackable");
  if (isUnderReview(item)) return t(REVIEW_LABEL);
  switch (item.status) {
    case "recorded": return item.issueNumber ? t("feedback.recorded_n", { n: item.issueNumber }) : t(STATUS_LABEL.recorded);
    case "fixed": return fixedWord(item);
    case "duplicate": return item.duplicateOf ? t("feedback.duplicate_of_n", { n: item.duplicateOf }) : t(STATUS_LABEL.duplicate);
    case "needs_info": return t(item.needsInput ? STATUS_LABEL.received : STATUS_LABEL.needs_info);
    default: return t(STATUS_LABEL[item.status]);
  }
}

const ORDER: Record<FeedbackStatus, number> = {
  received: 0, needs_info: 0, answered: 0, closed: 0, withdrawn: 0, recorded: 1, in_progress: 2, fixed: 3, wontfix: 3, duplicate: 3,
};

// Outcomes that never reach GitHub: the timeline is the receipt and what became of it.
function plainSteps(item: FeedbackItem, received: Step): Step[] | null {
  switch (item.status) {
    case "needs_info":
      return [received, { id: "needs_info", label: t(item.needsInput ? "feedback.waiting_for_your_information" : STATUS_LABEL.needs_info), state: "current" }];
    case "answered":
    case "closed":
    case "withdrawn":
      return [received, { id: item.status, label: t(STATUS_LABEL[item.status]), state: "done", neutral: item.status !== "answered" }];
    default:
      return null;
  }
}

export function steps(item: FeedbackItem, issue: (n: number) => ReactNode): Step[] {
  const review = isUnderReview(item);
  const at = ORDER[item.status];
  const terminal = at === 3;
  const state = (i: number): StepState => (i < at || (terminal && i === at) ? "done" : i === at ? "current" : "todo");
  const received: Step = { id: "received", label: review ? t(REVIEW_LABEL) : t("feedback.received_on", { day: shortDay(item.createdAt) }), state: state(0) };
  const plain = plainSteps(item, { ...received, state: "done" });
  if (plain) return plain;
  const recorded = item.issueNumber ? tx("feedback.recorded_as_issue", { issue: issue(item.issueNumber) }) : t(STATUS_LABEL.recorded);
  const end: ReactNode =
    item.status === "fixed" ? fixedWord(item)
      : item.status === "wontfix" ? t(STATUS_LABEL.wontfix)
        : item.status === "duplicate"
          ? item.duplicateOf ? tx("feedback.duplicate_of_issue", { issue: issue(item.duplicateOf) }) : t(STATUS_LABEL.duplicate)
          : t("feedback.ship_fix");
  const out: Step[] = [received, { id: "recorded", label: recorded, state: state(1) }];
  if (item.status === "wontfix" || item.status === "duplicate") return [...out, { id: item.status, label: end, state: "done" }];
  return [...out, { id: "in_progress", label: t(STATUS_LABEL.in_progress), state: state(2) }, { id: terminal ? item.status : "resolved", label: end, state: state(3) }];
}

/** The chips a report carries: a waiting question first, then where it stands. */
export function StatusChips({ item }: { item: FeedbackItem }) {
  const status = item.statusUnavailable ? "unavailable" : item.status;
  return (
    <>
      {item.needsInput && !item.statusUnavailable && <span className="fbk-chip" data-input="">{t("feedback.needs_your_reply")}</span>}
      <span className="fbk-chip" data-status={status} data-review={isUnderReview(item) ? "" : undefined}>{statusWord(item)}</span>
    </>
  );
}
