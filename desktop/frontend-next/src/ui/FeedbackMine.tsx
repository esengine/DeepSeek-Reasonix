import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { current, t } from "../i18n";
import { tx } from "../i18n/rich";
import { FEEDBACK_NEXT_VERSION, FEEDBACK_REPO_ISSUES, isUnderReview, type FeedbackItem, type FeedbackMine as Mine, type FeedbackStatus } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { CopyButton } from "./CopyButton";
import { FeedbackLevel } from "./FeedbackLevel";
import { feedbackFailure, type FeedbackFailure } from "./feedbackfailure";
import { FeedbackReplyBox, FeedbackReviewNote, FeedbackThread, foldedUnread } from "./FeedbackThread";
import { StudioIcon, type StudioIconName } from "./StudioIcon";

export const STATUS_LABEL: Record<FeedbackStatus, string> = {
  received: "已收到",
  needs_info: "需要补充信息",
  answered: "维护者已回复",
  closed: "已关闭此反馈",
  recorded: "已登记",
  in_progress: "处理中",
  fixed: "已修复",
  wontfix: "不予修复",
  duplicate: "重复",
};

const STATUS_ICON: Record<FeedbackStatus, StudioIconName> = {
  received: "clock",
  needs_info: "warning",
  answered: "check",
  closed: "close",
  recorded: "list",
  in_progress: "refresh",
  fixed: "check",
  wontfix: "close",
  duplicate: "copy",
};

export const REVIEW_LABEL = "审核中";

const CATEGORY_LABEL = { bug: "问题", idea: "建议", question: "疑问", other: "其他" } as const;

type StepState = "done" | "current" | "todo";

const STEP_STATE: Record<StepState, string> = { done: "已完成：", current: "当前：", todo: "尚未开始：" };

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
  received: 0, needs_info: 0, answered: 0, closed: 0, recorded: 1, in_progress: 2, fixed: 3, wontfix: 3, duplicate: 3,
};

// Outcomes that never reach GitHub: the timeline is the receipt and what became of it.
function plainSteps(item: FeedbackItem): Step[] | null {
  const received: Step = { id: "received", label: t(STATUS_LABEL.received), state: "done" };
  switch (item.status) {
    case "needs_info":
      return [received, { id: "needs_info", label: t(item.needsInput ? "等你补充信息" : STATUS_LABEL.needs_info), state: "current" }];
    case "answered":
    case "closed":
      return [received, { id: item.status, label: t(STATUS_LABEL[item.status]), state: "done", neutral: item.status === "closed" }];
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
  const recorded = item.issueNumber ? tx("已登记为 {issue}", { issue: issue(item.issueNumber) }) : t(STATUS_LABEL.recorded);
  const end: ReactNode =
    item.status === "fixed"
      ? item.resolvedVersion && item.resolvedVersion !== FEEDBACK_NEXT_VERSION
        ? t("已在 {version} 修复", { version: item.resolvedVersion })
        : t("已修复，将随下个版本发布")
      : item.status === "wontfix"
        ? t(STATUS_LABEL.wontfix)
        : item.status === "duplicate"
          ? item.duplicateOf
            ? tx("与 {issue} 重复", { issue: issue(item.duplicateOf) })
            : t(STATUS_LABEL.duplicate)
          : t("已解决");
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
  const [mine, setMine] = useState<Mine | null>(null);
  const [failure, setFailure] = useState<FeedbackFailure | null>(null);
  const [loading, setLoading] = useState(true);
  const [fresh, setFresh] = useState<Record<string, number>>({});
  const [replyBytes, setReplyBytes] = useState<number | null>(null);
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
        if (fresher > 0) setSaid(t("有 {n} 份反馈收到了新回复。", { n: fresher }));
      })
      .catch((e) => setFailure(feedbackFailure(e)))
      .finally(() => setLoading(false));
  }, [port, markSeen, tell]);

  useEffect(load, [load]);

  useEffect(() => {
    let live = true;
    port.feedbackEnv(document.documentElement.lang).then((e) => live && setReplyBytes(e.limits.replyBytes)).catch(() => {});
    return () => {
      live = false;
    };
  }, [port]);

  const sent = (receipt: string) => {
    setSaid(t("回复已发送。"));
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
        <span className="fbk-hint">{t("每次打开这一页时刷新。状态和回复来自维护者，已登记的反馈会跟随对应的 GitHub 议题。")}</span>
        <button type="button" className="btn sm" data-action="feedback.refresh" disabled={loading} onClick={load}>
          <StudioIcon name="refresh" />
          {t("刷新列表")}
        </button>
      </div>

      {mine?.profile ? (
        <FeedbackLevel profile={mine.profile} offline={mine.offline} />
      ) : (
        mine !== null && mine.items.length > 0 && <p className="fbk-hint fbk-level-gone">{t("等级暂时无法显示。")}</p>
      )}

      {mine?.offline && (
        <div className="fbk-note" role="status" data-tone="info">
          <StudioIcon name="warning" />
          <span>{t("暂时连不上反馈服务。下面是保存在本机的记录，状态可能不是最新的。")}</span>
        </div>
      )}

      {mine?.items.some((i) => i.statusUnavailable) && (
        <div className="fbk-note" role="status" data-tone="info">
          <StudioIcon name="warning" />
          <span>{t("有些反馈是用这台电脑以前的身份发出的，已经查不到它们的最新状态。")}</span>
        </div>
      )}

      {failure && (
        <div className="fbk-note" role="alert" data-tone="error">
          <StudioIcon name="warning" />
          <span>{failure.message}</span>
          <button type="button" className="btn sm" data-action="feedback.retry-mine" onClick={load}>{t("重试")}</button>
        </div>
      )}

      {!mine && !failure && <p className="fbk-empty" role="status">{t("正在读取你的反馈…")}</p>}

      {mine && mine.items.length === 0 && (
        <p className="fbk-empty">{t("还没有提交过反馈。发出第一条之后，它的进展会出现在这里。")}</p>
      )}

      {mine && mine.items.length > 0 && (
        <ul className="fbk-list" ref={list}>
          {mine.items.map((item) => (
            <li key={item.receipt} className="fbk-item" tabIndex={-1} aria-label={`${item.receipt} ${snippet(item.titleSnippet)}`} data-receipt={item.receipt} data-status={item.status} data-stale={item.statusUnavailable ? "" : undefined}>
              <div className="fbk-item-hd">
                <code className="fbk-code">{item.receipt}</code>
                <CopyButton iconOnly text={item.receipt} label={t("复制回执号")} />
                <span className="fbk-meta">{t(CATEGORY_LABEL[item.category])} · {day(item.createdAt)}</span>
                {item.statusUnavailable ? (
                  <span className="fbk-chip" data-status="unavailable">{t("状态已无法追踪")}</span>
                ) : item.needsInput ? (
                  <span className="fbk-chip" data-status="needs_info" data-input="">
                    <StudioIcon name={STATUS_ICON.needs_info} />
                    {t("需要你回复")}
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
              {!item.statusUnavailable && <ol className="fbk-tl" aria-label={t("处理进展")}>
                {steps(item, issue).map((s) => (
                  <li key={s.id} data-state={s.state} data-tone={s.neutral ? "neutral" : undefined} aria-current={s.state === "current" ? "step" : undefined}>
                    <i aria-hidden="true" />
                    <span className="sr-only">{t(STEP_STATE[s.state])}</span>
                    <span>{s.label}</span>
                  </li>
                ))}
              </ol>}
              <FeedbackReviewNote item={item} />
              <FeedbackThread item={item} fresh={fresh[item.receipt] ?? 0} onView={view} />
              <FeedbackReplyBox port={port} item={item} limit={replyBytes} offline={mine.offline} onSent={sent} onFile={onFile} onStale={load} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
