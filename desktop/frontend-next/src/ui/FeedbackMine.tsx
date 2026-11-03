import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { current, t } from "../i18n";
import { tx } from "../i18n/rich";
import { FEEDBACK_NEXT_VERSION, FEEDBACK_REPO_ISSUES, isUnderReview, type FeedbackItem, type FeedbackLimits, type FeedbackMine as Mine, type FeedbackStatus } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { CopyButton } from "./CopyButton";
import { FeedbackFilter } from "./FeedbackFilter";
import { inGroup, type FeedbackGroup } from "./feedbackgroups";
import { FeedbackLevel } from "./FeedbackLevel";
import { FeedbackLink } from "./FeedbackLink";
import { FeedbackBoard } from "./FeedbackBoard";
import { feedbackFailure, type FeedbackFailure } from "./feedbackfailure";
import { FeedbackWithdraw } from "./FeedbackWithdraw";
import { FeedbackReplyBox, FeedbackReviewNote, FeedbackThread, foldedUnread } from "./FeedbackThread";
import { StudioIcon, type StudioIconName } from "./StudioIcon";

export const STATUS_LABEL: Record<FeedbackStatus, string> = {
  received: "feedbackMine.received",
  needs_info: "feedbackMine.needsMoreInfo",
  answered: "feedbackMine.answeredByUs",
  closed: "feedbackMine.closed",
  withdrawn: "common.withdrawn",
  recorded: "feedbackMine.recorded",
  in_progress: "feedbackMine.inProgress",
  fixed: "feedbackMine.fixed",
  wontfix: "feedbackMine.wonTFix",
  duplicate: "feedbackMine.duplicate",
};

const STATUS_ICON: Record<FeedbackStatus, StudioIconName> = {
  received: "clock",
  needs_info: "warning",
  answered: "check",
  closed: "close",
  withdrawn: "close",
  recorded: "list",
  in_progress: "refresh",
  fixed: "check",
  wontfix: "close",
  duplicate: "copy",
};

export const REVIEW_LABEL = "common.inReview";

const CATEGORY_LABEL = { bug: "common.bug", idea: "common.idea", question: "common.question", other: "common.other" } as const;

type StepState = "done" | "current" | "todo";

const STEP_STATE: Record<StepState, string> = { done: "feedbackMine.done", current: "feedbackMine.current", todo: "feedbackMine.notYet" };

// What this window last read, per port, so opening the page again draws the list
// it already has and refreshes behind it instead of showing one waiting line.
const remembered = new WeakMap<object, Mine>();

const SNIPPET_CUT = 80;

function snippet(text: string): string {
  return [...text].length >= SNIPPET_CUT ? `${text.trimEnd()}…` : text;
}

interface Step {
  id: string;
  label: ReactNode;
  state: StepState;
  neutral?: boolean;
}

const ORDER: Record<FeedbackStatus, number> = {
  received: 0, needs_info: 0, answered: 0, closed: 0, withdrawn: 0, recorded: 1, in_progress: 2, fixed: 3, wontfix: 3, duplicate: 3,
};

// Outcomes that never reach GitHub: the timeline is the receipt and what became of it.
function plainSteps(item: FeedbackItem): Step[] | null {
  const received: Step = { id: "received", label: t(STATUS_LABEL.received), state: "done" };
  switch (item.status) {
    case "needs_info":
      return [received, { id: "needs_info", label: t(item.needsInput ? "feedbackMine.waitingForYourInformation" : STATUS_LABEL.needs_info), state: "current" }];
    case "answered":
    case "closed":
    case "withdrawn":
      return [received, { id: item.status, label: t(STATUS_LABEL[item.status]), state: "done", neutral: item.status !== "answered" }];
    default:
      return null;
  }
}

function steps(item: FeedbackItem, issue: (n: number) => ReactNode): Step[] {
  const plain = plainSteps(item);
  if (plain) return plain;
  const at = ORDER[item.status];
  const terminal = at === 3;
  const state = (i: number): StepState => (i < at || (terminal && i === at) ? "done" : i === at ? "current" : "todo");
  const recorded = item.issueNumber ? tx("feedbackMine.recordedAs", { issue: issue(item.issueNumber) }) : t(STATUS_LABEL.recorded);
  const end: ReactNode =
    item.status === "fixed"
      ? item.resolvedVersion && item.resolvedVersion !== FEEDBACK_NEXT_VERSION
        ? t("feedbackMine.fixedIn", { version: item.resolvedVersion })
        : t("feedbackMine.fixedShipsInTheNext")
      : item.status === "wontfix"
        ? t(STATUS_LABEL.wontfix)
        : item.status === "duplicate"
          ? item.duplicateOf
            ? tx("feedbackMine.duplicateOf", { issue: issue(item.duplicateOf) })
            : t(STATUS_LABEL.duplicate)
          : t("feedbackMine.resolved");
  const out: Step[] = [
    { id: "received", label: t(isUnderReview(item) ? REVIEW_LABEL : STATUS_LABEL.received), state: state(0) },
    { id: "recorded", label: recorded, state: state(1) },
  ];
  if (item.status === "wontfix" || item.status === "duplicate") return [...out, { id: item.status, label: end, state: "done" }];
  return [...out, { id: "in_progress", label: t(STATUS_LABEL.in_progress), state: state(2) }, { id: terminal ? item.status : "resolved", label: end, state: state(3) }];
}

const newest = (i: FeedbackItem) => Math.max(0, ...i.replies.map((r) => r.id));

interface Props {
  port: AgentPort;
  onFile: (url: string) => void;
  // How many reports still want attention once this page has shown them.
  onUnread?: (n: number) => void;
}

// A reply is read only in an expanded thread; its "new" mark stays for this visit.
export function FeedbackMine({ port, onFile, onUnread }: Props) {
  const [mine, setMine] = useState<Mine | null>(() => remembered.get(port) ?? null);
  const [account, setAccount] = useState(false);
  const [boardTick, setBoardTick] = useState(0);
  const [failure, setFailure] = useState<FeedbackFailure | null>(null);
  const [loading, setLoading] = useState(true);
  const [fresh, setFresh] = useState<Record<string, number>>({});
  const [group, setGroup] = useState<FeedbackGroup>("all");
  const [limits, setLimits] = useState<FeedbackLimits | null>(null);
  const list = useRef<HTMLUListElement>(null);
  const [said, setSaid] = useState("");
  const report = useRef(onUnread);
  report.current = onUnread;
  const itemsRef = useRef<Mine["items"]>([]);
  const pending = useRef(new Set<string>());
  const views = useRef(new Map<string, boolean>());
  const tell = useCallback(() => report.current?.(itemsRef.current.filter((i) => i.needsInput || pending.current.has(i.receipt)).length), []);
  const markSeen = useCallback(async (item: FeedbackItem) => {
    const upTo = newest(item);
    await port.feedbackSeen(item.receipt, upTo);
    const current = itemsRef.current.find((i) => i.receipt === item.receipt);
    if (current && newest(current) <= upTo) pending.current.delete(item.receipt);
    tell();
  }, [port, tell]);
  const view = (receipt: string, all: boolean | null) => {
    if (all === null) {
      views.current.delete(receipt);
      return;
    }
    views.current.set(receipt, all);
    const item = itemsRef.current.find((i) => i.receipt === receipt)!;
    if (pending.current.has(receipt) && (all || !foldedUnread(item))) void markSeen(item).catch(() => {});
  };

  const load = useCallback(() => {
    setLoading(true);
    port
      .myFeedback()
      .then(async (m) => {
        remembered.set(port, m);
        setMine(m);
        setFailure(null);
        setFresh((prev) => {
          const next = { ...prev };
          for (const i of m.items) if (i.unreadReplies > 0) next[i.receipt] = Math.max(next[i.receipt] ?? 0, i.unreadReplies);
          return next;
        });
        itemsRef.current = m.items;
        pending.current = new Set(m.items.filter((i) => i.unreadReplies > 0).map((i) => i.receipt));
        const shown = m.items.filter((i) => i.unreadReplies > 0 && views.current.has(i.receipt) && (views.current.get(i.receipt) || !foldedUnread(i)));
        await Promise.allSettled(shown.map(markSeen));
        tell();
        const fresher = m.items.filter((i) => i.unreadReplies > 0).length;
        if (fresher > 0) setSaid(t("feedbackMine.ofYourReportsHaveNew", { n: fresher }));
      })
      .catch((e) => setFailure(feedbackFailure(e)))
      .finally(() => setLoading(false));
  }, [port, markSeen, tell]);

  useEffect(load, [load]);

  useEffect(() => {
    let live = true;
    port.feedbackEnv(document.documentElement.lang).then((e) => live && setLimits(e.limits)).catch(() => {});
    return () => {
      live = false;
    };
  }, [port]);

  const pick = (to: FeedbackGroup) => {
    setGroup(to);
    requestAnimationFrame(() => list.current?.scrollIntoView({ block: "start" }));
  };

  const sent = (receipt: string) => {
    setSaid(t("feedbackMine.replySent"));
    load();
    requestAnimationFrame(() => list.current?.querySelector<HTMLElement>(`[data-receipt="${receipt}"]`)?.focus());
  };

  const issue = (n: number): ReactNode => {
    if (!Number.isSafeInteger(n) || n <= 0) return `#${n}`;
    const url = FEEDBACK_REPO_ISSUES + String(n);
    return (
      <a href={url} rel="noopener noreferrer" data-action="feedback.link" onClick={(e) => { e.preventDefault(); onFile(url); }}>#{n}</a>
    );
  };

  const day = (iso: string) => new Date(iso).toLocaleDateString(current() === "zh" ? "zh-CN" : "en", { year: "numeric", month: "short", day: "numeric" });

  return (
    <div className="fbk-mine" aria-busy={loading}>
      <p className="sr-only" role="status" aria-live="polite">{said}</p>
      <div className="fbk-mine-bar">
        <span className="fbk-hint">{t("feedbackMine.refreshedEachTimeYouOpen")}</span>
        <button type="button" className="btn sm" data-action="feedback.refresh" disabled={loading} onClick={load}>
          <StudioIcon name="refresh" />
          {t("feedbackMine.refreshList")}
        </button>
      </div>

      {mine && mine.items.length > 1 && <FeedbackFilter items={mine.items} fresh={fresh} group={group} onPick={pick} />}

      {mine && (
        <section className="fbk-account">
          <button type="button" className="btn sm" data-action="feedback.account-section" aria-expanded={account} onClick={() => setAccount((open) => !open)}>
            {t("feedbackMine.levelAccountAndLeaderboard")}
          </button>
          {account && (
            <>
              {mine.profile ? (
                <FeedbackLevel profile={mine.profile} offline={mine.offline} />
              ) : (
                mine.items.length > 0 && <p className="fbk-hint fbk-level-gone">{t("feedbackMine.yourLevelCannotBeShown")}</p>
              )}
              <FeedbackLink port={port} hasReport={mine.items.length > 0} onBoardChanged={() => setBoardTick((n) => n + 1)} />
              <FeedbackBoard port={port} reload={boardTick} />
            </>
          )}
        </section>
      )}

      {mine?.offline && (
        <div className="fbk-note" role="status" data-tone="info">
          <StudioIcon name="warning" />
          <span>{t("feedbackMine.cannotReachTheFeedbackService")}</span>
        </div>
      )}

      {mine?.items.some((i) => i.otherInstall) && <p className="fbk-hint">{t("feedbackMine.readMarksAreKeptOn")}</p>}

      {mine?.items.some((i) => i.statusUnavailable) && (
        <div className="fbk-note" role="status" data-tone="info">
          <StudioIcon name="warning" />
          <span>{t("feedbackMine.someReportsWereSentUnder")}</span>
        </div>
      )}

      {failure && (
        <div className="fbk-note" role="alert" data-tone="error">
          <StudioIcon name="warning" />
          <span>{failure.message}</span>
          <button type="button" className="btn sm" data-action="feedback.retry-mine" onClick={load}>{t("common.retry")}</button>
        </div>
      )}

      {!mine && !failure && <p className="fbk-empty" role="status">{t("feedbackMine.loadingYourFeedback")}</p>}

      {mine && mine.items.length === 0 && (
        <p className="fbk-empty">{t("feedbackMine.noFeedbackSentYetOnce")}</p>
      )}

      {mine && mine.items.length > 1 && !mine.items.some((i) => inGroup(i, group, fresh[i.receipt] ?? 0)) && (
        <p className="fbk-empty">{t("feedbackMine.noReportsInThisGroup")}</p>
      )}

      {mine && mine.items.length > 0 && (
        <ul className="fbk-list" ref={list}>
          {mine.items.map((item) => (
            <li key={item.receipt} className="fbk-item" hidden={!inGroup(item, group, fresh[item.receipt] ?? 0)} tabIndex={-1} aria-label={`${item.receipt} ${snippet(item.titleSnippet)}`} data-receipt={item.receipt} data-status={item.status} data-stale={item.statusUnavailable ? "" : undefined}>
              <div className="fbk-item-hd">
                <code className="fbk-code">{item.receipt}</code>
                <CopyButton iconOnly text={item.receipt} label={t("common.copyReceiptNumber")} />
                <span className="fbk-meta">{t(CATEGORY_LABEL[item.category])} · {day(item.createdAt)}</span>
                {item.otherInstall && <span className="fbk-chip" data-device="other">{t("feedbackMine.fromAnotherDevice")}</span>}
                {item.statusUnavailable ? (
                  <span className="fbk-chip" data-status="unavailable">{t("feedbackMine.statusNoLongerTrackable")}</span>
                ) : item.needsInput ? (
                  <span className="fbk-chip" data-status="needs_info" data-input="">
                    <StudioIcon name={STATUS_ICON.needs_info} />
                    {t("common.needsYourReply")}
                  </span>
                ) : isUnderReview(item) ? (
                  <span className="fbk-chip" data-status="received" data-review="">
                    <StudioIcon name="shield" />
                    {t(REVIEW_LABEL)}
                  </span>
                ) : (
                  <span className="fbk-chip" data-status={item.status}>
                    <StudioIcon name={STATUS_ICON[item.status]} />
                    {t(STATUS_LABEL[item.status])}
                  </span>
                )}
              </div>
              <p className="fbk-snippet">{snippet(item.titleSnippet)}</p>
              {!item.statusUnavailable && <ol className="fbk-tl" aria-label={t("feedbackMine.progress")}>
                {steps(item, issue).map((s) => (
                  <li key={s.id} data-state={s.state} data-tone={s.neutral ? "neutral" : undefined} aria-current={s.state === "current" ? "step" : undefined}>
                    <i aria-hidden="true" />
                    <span className="sr-only">{t(STEP_STATE[s.state])}</span>
                    <span>{s.label}</span>
                  </li>
                ))}
              </ol>}
              <FeedbackReviewNote item={item} />
              <FeedbackThread item={item} level={mine.profile?.level ?? null} fresh={fresh[item.receipt] ?? 0} onView={view} />
              <FeedbackWithdraw port={port} item={item} onDone={() => { setSaid(t("feedbackMine.theReportWasWithdrawn")); load(); }} onStale={load} />
              <FeedbackReplyBox port={port} item={item} limit={limits?.replyBytes ?? null} limits={limits} offline={mine.offline} onSent={sent} onFile={onFile} onStale={load} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
