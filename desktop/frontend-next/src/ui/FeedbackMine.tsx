import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { t } from "../i18n";
import { FEEDBACK_REPO_ISSUES, type FeedbackItem, type FeedbackLimits, type FeedbackMine as Mine } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { FeedbackDetail } from "./FeedbackDetail";
import { FeedbackFilter } from "./FeedbackFilter";
import { inGroup, type FeedbackGroup } from "./feedbackgroups";
import { feedbackFailure, type FeedbackFailure } from "./feedbackfailure";
import { CATEGORY_LABEL, shortDay, snippet, StatusChips } from "./feedbackstatus";
import { FeedbackStanding } from "./FeedbackStanding";
import { StudioIcon } from "./StudioIcon";

// What this window last read, per port, so opening the page again draws the list
// it already has and refreshes behind it instead of showing one waiting line.
const remembered = new WeakMap<object, Mine>();

const newest = (i: FeedbackItem) => Math.max(0, ...i.replies.map((r) => r.id));

interface Props {
  port: AgentPort;
  onFile: (url: string) => void;
  // How many reports still want attention once this page has shown them.
  onUnread?: (n: number) => void;
  // Where the title bar keeps the level line; absent, it is drawn above the list.
  slot?: HTMLElement | null;
}

// The list on the left, one report on the right. A report's replies are read once
// it is the one on the right; its "new" mark stays for this visit.
export function FeedbackMine({ port, onFile, onUnread, slot = null }: Props) {
  const [mine, setMine] = useState<Mine | null>(() => remembered.get(port) ?? null);
  const [failure, setFailure] = useState<FeedbackFailure | null>(null);
  const [loading, setLoading] = useState(true);
  const [fresh, setFresh] = useState<Record<string, number>>({});
  const [group, setGroup] = useState<FeedbackGroup>("all");
  const [chosen, setChosen] = useState<string | null>(null);
  const [limits, setLimits] = useState<FeedbackLimits | null>(null);
  const [said, setSaid] = useState("");
  const drafts = useRef(new Map<string, string>()).current;
  const report = useRef(onUnread);
  report.current = onUnread;
  const itemsRef = useRef<Mine["items"]>([]);
  const pending = useRef(new Set<string>());
  const tell = useCallback(() => report.current?.(itemsRef.current.filter((i) => i.needsInput || pending.current.has(i.receipt)).length), []);
  const markSeen = useCallback(async (item: FeedbackItem) => {
    const upTo = newest(item);
    await port.feedbackSeen(item.receipt, upTo);
    const current = itemsRef.current.find((i) => i.receipt === item.receipt);
    if (current && newest(current) <= upTo) pending.current.delete(item.receipt);
    tell();
  }, [port, tell]);

  const load = useCallback(() => {
    setLoading(true);
    port
      .myFeedback()
      .then((m) => {
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
        tell();
        const fresher = m.items.filter((i) => i.unreadReplies > 0).length;
        if (fresher > 0) setSaid(t("feedback.n_of_your_reports_have", { n: fresher }));
      })
      .catch((e) => setFailure(feedbackFailure(e)))
      .finally(() => setLoading(false));
  }, [port, tell]);

  useEffect(load, [load]);

  useEffect(() => {
    let live = true;
    port.feedbackEnv(document.documentElement.lang).then((e) => live && setLimits(e.limits)).catch(() => {});
    return () => {
      live = false;
    };
  }, [port]);

  // A question waiting on the reporter comes first; otherwise the service's order.
  const items = useMemo(() => (mine ? [...mine.items].sort((a, b) => Number(b.needsInput) - Number(a.needsInput)) : []), [mine]);
  const visible = items.filter((i) => inGroup(i, group, fresh[i.receipt] ?? 0));
  const shown = visible.find((i) => i.receipt === chosen) ?? visible[0];

  useEffect(() => {
    if (shown && pending.current.has(shown.receipt)) void markSeen(shown).catch(() => {});
  }, [shown, markSeen]);

  const sent = (receipt: string) => {
    setSaid(t("feedback.reply_sent"));
    setChosen(receipt);
    load();
  };

  const issue = (n: number): ReactNode => {
    if (!Number.isSafeInteger(n) || n <= 0) return `#${n}`;
    const url = FEEDBACK_REPO_ISSUES + String(n);
    return (
      <a href={url} rel="noopener noreferrer" data-action="feedback.link" onClick={(e) => { e.preventDefault(); onFile(url); }}>#{n}</a>
    );
  };

  return (
    <div className="fbk-mine" aria-busy={loading} data-empty={mine && items.length === 0 ? "" : undefined}>
      <p className="sr-only" role="status" aria-live="polite">{said}</p>
      {mine && <FeedbackStanding port={port} mine={mine} slot={slot} />}
      <div className="fbk-side">
        {items.length > 1 && <FeedbackFilter items={items} fresh={fresh} group={group} onPick={setGroup} />}

        {mine?.offline && (
          <div className="fbk-note" role="status" data-tone="info">
            <StudioIcon name="warning" />
            <span>{t("feedback.cannot_reach_the_feedback_service")}</span>
          </div>
        )}

        {failure && (
          <div className="fbk-note" role="alert" data-tone="error">
            <StudioIcon name="warning" />
            <span>{failure.message}</span>
            <button type="button" className="btn sm" data-action="feedback.retry-mine" onClick={load}>{t("common.retry")}</button>
          </div>
        )}

        {!mine && !failure && <p className="fbk-empty" role="status">{t("feedback.loading_your_feedback")}</p>}

        {items.length > 1 && visible.length === 0 && <p className="fbk-empty">{t("feedback.no_reports_in_this_group")}</p>}

        {items.length > 0 && (
          <ul className="fbk-list">
            {items.map((item) => (
              <li key={item.receipt} className="fbk-item" hidden={!inGroup(item, group, fresh[item.receipt] ?? 0)} data-receipt={item.receipt}
                data-status={item.status} data-stale={item.statusUnavailable ? "" : undefined} data-fresh={(fresh[item.receipt] ?? 0) > 0 ? "" : undefined}>
                <button type="button" className="fbk-row" data-action="feedback.pick" data-target={item.receipt}
                  aria-current={item === shown ? "true" : undefined} aria-label={`${item.receipt} ${snippet(item.titleSnippet)}`}
                  onClick={() => setChosen(item.receipt)}>
                  <span className="fbk-row-title" dir="auto">{snippet(item.titleSnippet)}</span>
                  <span className="fbk-row-meta">{t(CATEGORY_LABEL[item.category])} · {shortDay(item.createdAt)}</span>
                  <span className="fbk-row-chips"><StatusChips item={item} /></span>
                </button>
              </li>
            ))}
          </ul>
        )}

        <div className="fbk-footnotes">
          <p className="fbk-hint">
            {t("feedback.refreshed_each_time_you_open")}{" "}
            <button type="button" className="fbk-linkish" data-action="feedback.refresh" disabled={loading} onClick={load}>{t("feedback.refresh_list")}</button>
          </p>
          {items.some((i) => i.otherInstall) && <p className="fbk-hint">{t("feedback.read_marks_are_kept_on")}</p>}
          {items.some((i) => i.statusUnavailable) && <p className="fbk-hint" role="status">{t("feedback.some_reports_were_sent_under")}</p>}
        </div>
      </div>
      <div className="fbk-main">
        {shown ? (
          <FeedbackDetail port={port} item={shown} level={mine?.profile?.level ?? null} fresh={fresh[shown.receipt] ?? 0} limits={limits}
            offline={mine?.offline ?? false} issue={issue} onFile={onFile} onSent={sent} drafts={drafts}
            onWithdrawn={() => { setSaid(t("feedback.the_report_was_withdrawn")); load(); }} onStale={load} />
        ) : mine && items.length === 0 ? (
          <p className="fbk-empty">{t("feedback.no_feedback_sent_yet_once")}</p>
        ) : null}
      </div>
    </div>
  );
}
