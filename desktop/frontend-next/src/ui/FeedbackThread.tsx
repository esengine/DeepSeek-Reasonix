import { useMemo, useRef, useState } from "react";
import { current, t } from "../i18n";
import { FEEDBACK_CODE, FEEDBACK_REPO_ISSUES, FEEDBACK_REPLYABLE, type FeedbackItem, type FeedbackReply } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { replyFailure, type FeedbackFailure } from "./feedbackfailure";
import { StudioIcon } from "./StudioIcon";

const SHOWN = 4;

// Unread replies sit at the end of the thread; any that fall before the folded
// boundary have not been shown yet.
export function foldedUnread(item: FeedbackItem): boolean {
  const firstUnread = item.replies.filter((r) => r.author === "maintainer").slice(-item.unreadReplies)[0];
  return firstUnread !== undefined && item.replies.indexOf(firstUnread) < item.replies.length - SHOWN;
}
const encoder = new TextEncoder();

// Used only when the limit could not be read; the kernel still refuses an oversized reply.
const REPLY_BYTES_FALLBACK = 4096;

const AUTHOR_LABEL = { maintainer: "维护者", user: "你" } as const;

function when(iso: string): string {
  return new Date(iso).toLocaleString(current() === "zh" ? "zh-CN" : "en", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

interface ThreadProps {
  item: FeedbackItem;
  fresh: number;
  onShowAll?: (receipt: string) => void;
}

// Replies are plain text by contract: they render as text nodes, never linked,
// formatted or interpreted, whoever wrote them.
export function FeedbackThread({ item, fresh, onShowAll }: ThreadProps) {
  const [all, setAll] = useState(false);
  const replies = item.replies;
  const hidden = all ? 0 : Math.max(0, replies.length - SHOWN);
  const shown = replies.slice(hidden);
  const newIds = useMemo(() => new Set(fresh > 0 ? replies.filter((r) => r.author === "maintainer").slice(-fresh).map((r) => r.id) : []), [replies, fresh]);
  const isNew = (r: FeedbackReply) => newIds.has(r.id);
  if (replies.length === 0) return null;
  return (
    <section className="fbk-thread" aria-label={t("对话")}>
      {hidden > 0 && (
        <button type="button" className="btn sm" data-action="feedback.thread.more" onClick={() => { setAll(true); onShowAll?.(item.receipt); }}>
          {t("显示更早的 {n} 条", { n: hidden })}
        </button>
      )}
      <ol>
        {shown.map((r) => (
          <li key={r.id} data-author={r.author} data-new={isNew(r) ? "" : undefined}>
            <div className="fbk-msg-hd">
              <b>{t(AUTHOR_LABEL[r.author])}</b>
              {isNew(r) && <span className="fbk-new">{t("新")}</span>}
              <time dateTime={r.createdAt}>{when(r.createdAt)}</time>
            </div>
            <p className="fbk-msg" dir="auto">{r.body}</p>
          </li>
        ))}
      </ol>
    </section>
  );
}

interface ReplyProps {
  port: AgentPort;
  item: FeedbackItem;
  limit: number | null;
  offline: boolean;
  onSent: (receipt: string) => void;
  onFile: (url: string) => void;
  onStale: () => void;
}

export function FeedbackReplyBox({ port, item, limit, offline, onSent, onFile, onStale }: ReplyProps) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [failure, setFailure] = useState<FeedbackFailure | null>(null);
  const opener = useRef<HTMLButtonElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const replyable = !item.statusUnavailable && FEEDBACK_REPLYABLE.includes(item.status);
  if (!replyable) return null;
  if (offline) return <p className="fbk-hint">{t("离线时暂时不能回复。")}</p>;

  const expanded = open || item.needsInput;
  const bytes = encoder.encode(text).length;
  const max = limit ?? REPLY_BYTES_FALLBACK;
  const over = bytes > max;
  const blocked = sending || text.trim() === "" || over;

  const send = async () => {
    if (blocked) return;
    setSending(true);
    setFailure(null);
    try {
      await port.replyFeedback(item.receipt, text.trim());
      setText("");
      setOpen(false);
      onSent(item.receipt);
    } catch (e) {
      const f = replyFailure(e);
      setFailure(f);
      if (f.code === FEEDBACK_CODE.notReplyable) onStale();
    } finally {
      setSending(false);
    }
  };

  if (!expanded) {
    return (
      <button type="button" className="btn sm fbk-reply-open" ref={opener} data-action="feedback.reply.open" onClick={() => { setOpen(true); requestAnimationFrame(() => field.current?.focus()); }}>
        <StudioIcon name="edit" />
        {t("回复")}
      </button>
    );
  }

  const id = `fbk-reply-${item.receipt}`;
  return (
    <form
      className="fbk-reply"
      data-needs={item.needsInput ? "" : undefined}
      aria-busy={sending}
      noValidate
      data-action-submit="feedback.reply.send"
      onSubmit={(e) => { e.preventDefault(); void send(); }}
    >
      <label htmlFor={id} className="fbk-label">{item.needsInput ? t("你的回复") : t("回复维护者")}</label>
      <textarea
        id={id}
        ref={field}
        data-action="feedback.reply.body"
        value={text}
        rows={3}
        disabled={sending}
        aria-invalid={over ? true : undefined}
        aria-describedby={`${id}-hint ${id}-count`}
        onChange={(e) => setText(e.target.value)}
      />
      <div className="fbk-reply-foot">
        <span id={`${id}-hint`} className="fbk-hint">
          {item.issueNumber
            ? t("这份反馈已有公开议题 #{n}：你的回复会被公开转发到那个议题里。请不要写入密钥、密码或私有内容。", { n: item.issueNumber })
            : t("只发给维护者，不会公开。请不要写入密钥、密码或私有代码。")}
        </span>
        <span id={`${id}-count`} className="fbk-count" data-over={over ? "" : undefined}>
          {t("{n} / {max} 字节", { n: bytes.toLocaleString(), max: max.toLocaleString() })}
        </span>
      </div>
      {failure && (
        <div className="fbk-note" role="alert" data-tone="error" data-code={failure.code}>
          <StudioIcon name="warning" />
          <span>{failure.message}</span>
          {failure.code === FEEDBACK_CODE.unavailable && (
            <button type="button" className="btn sm" data-action="feedback.link" onClick={() => onFile(FEEDBACK_REPO_ISSUES + "new/choose")}>
              {t("去 GitHub")}
            </button>
          )}
        </div>
      )}
      <div className="fbk-acts">
        <button className="act" type="submit" data-primary disabled={blocked}>{t(sending ? "正在发送…" : "发送回复")}</button>
        {!item.needsInput && (
          <button className="act" type="button" data-action="feedback.reply.cancel" disabled={sending} onClick={() => { setOpen(false); setFailure(null); requestAnimationFrame(() => opener.current?.focus()); }}>
            {t("取消")}
          </button>
        )}
      </div>
    </form>
  );
}
