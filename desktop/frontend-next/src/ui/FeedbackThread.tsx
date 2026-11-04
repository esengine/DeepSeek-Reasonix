import { useCallback, useEffect, useId, useMemo, useRef, useState, type ClipboardEvent } from "react";
import { current, t } from "../i18n";
import { FEEDBACK_CODE, FEEDBACK_REPO_ISSUES, canReplyTo, isUnderReview, type FeedbackItem, type FeedbackLimits, type FeedbackReply } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { replyFailure, type FeedbackFailure } from "./feedbackfailure";
import { FeedbackShot } from "./FeedbackShot";
import { admit, megabytes, payload, readShot, REFUSED_LABEL, type Refused, type Shot } from "./feedbackshots";
import { LevelBadge } from "./LevelBadge";
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
  // The reporter's own level, shown beside their label; null or absent shows none.
  level?: number | null;
  fresh: number;
  onView?: (receipt: string, all: boolean | null) => void;
}

// Replies are plain text by contract: they render as text nodes, never linked,
// formatted or interpreted, whoever wrote them.
export function FeedbackThread({ item, level = null, fresh, onView }: ThreadProps) {
  const [all, setAll] = useState(false);
  const [expanded, setExpanded] = useState(item.needsInput);
  const id = useId();
  useEffect(() => { if (item.needsInput) onView?.(item.receipt, false); }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const replies = item.replies;
  const hidden = all ? 0 : Math.max(0, replies.length - SHOWN);
  const shown = replies.slice(hidden);
  const newIds = useMemo(() => new Set(fresh > 0 ? replies.filter((r) => r.author === "maintainer").slice(-fresh).map((r) => r.id) : []), [replies, fresh]);
  const isNew = (r: FeedbackReply) => newIds.has(r.id);
  if (replies.length === 0) return null;
  return (
    <section className="fbk-thread" aria-label={t("feedback.conversation")}>
      <button type="button" className="btn sm fbk-thread-toggle" data-action="feedback.thread.toggle" data-target={item.receipt} aria-expanded={expanded} aria-controls={id} onClick={() => {
        const next = !expanded;
        setExpanded(next);
        onView?.(item.receipt, next ? all : null);
      }}>
        <StudioIcon name={expanded ? "down" : "chevron"} />
        <span>{t(expanded ? "feedback.collapse_conversation" : "feedback.expand_conversation")}</span>
        <span className="fbk-meta">{t("feedback.n_replies", { n: replies.length })}</span>
        {fresh > 0 && <span className="fbk-thread-new">{t("feedback.n_new_replies", { n: fresh })}</span>}
      </button>
      <div id={id} className="fbk-thread-body" hidden={!expanded}>
        {hidden > 0 && (
          <button type="button" className="btn sm" data-action="feedback.thread.more" onClick={() => { setAll(true); onView?.(item.receipt, true); }}>
            {t("feedback.show_n_earlier_messages", { n: hidden })}
          </button>
        )}
        <ol>
          {shown.map((r) => (
            <li key={r.id} data-author={r.author} data-new={isNew(r) ? "" : undefined}>
              <div className="fbk-msg-hd">
                <b>{t(AUTHOR_LABEL[r.author])}</b>
                {r.author === "user" && level !== null && (
                  <>
                    <LevelBadge level={level} decorative size={14} />
                    <span className="sr-only">{t("feedback.level_n", { n: level })}</span>
                  </>
                )}
                {isNew(r) && <span className="fbk-new">{t("common.new")}</span>}
                <time dateTime={r.createdAt}>{when(r.createdAt)}</time>
              </div>
              <p className="fbk-msg" dir="auto">{r.body}</p>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

export function FeedbackReviewNote({ item }: { item: FeedbackItem }) {
  if (!isUnderReview(item)) return null;
  return (
    <p className="fbk-hint fbk-review" data-review="" role="status">
      {t("feedback.a_maintainer_is_reading_this")}
    </p>
  );
}

interface ReplyProps {
  port: AgentPort;
  item: FeedbackItem;
  limit: number | null;
  limits?: Pick<FeedbackLimits, "images" | "uploadBytes"> | null;
  offline: boolean;
  onSent: (receipt: string) => void;
  onFile: (url: string) => void;
  onStale: () => void;
}

export function FeedbackReplyBox({ port, item, limit, limits = null, offline, onSent, onFile, onStale }: ReplyProps) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [failure, setFailure] = useState<FeedbackFailure | null>(null);
  const opener = useRef<HTMLButtonElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const [shots, setShots] = useState<Shot[]>([]);
  const [refused, setRefused] = useState<Refused[]>([]);
  const held = useRef(shots);
  held.current = shots;
  const add = useCallback(
    async (files: File[]) => {
      if (!limits || files.length === 0) return;
      const { taken, refused: no } = admit(held.current.length, files, limits);
      setRefused(no);
      const read = await Promise.allSettled(taken.map(readShot));
      const ok = read.flatMap((r) => (r.status === "fulfilled" ? [r.value] : []));
      if (ok.length > 0) setShots((prev) => [...prev, ...ok].slice(0, limits.images));
    },
    [limits],
  );
  const replyable = canReplyTo(item);
  if (!replyable) return null;
  const reopens = item.status === "fixed";
  if (offline) return <p className="fbk-hint">{t("feedback.you_cannot_reply_while_offline")}</p>;

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
      await (shots.length > 0 ? port.replyFeedback(item.receipt, text.trim(), payload(shots)) : port.replyFeedback(item.receipt, text.trim()));
      setText("");
      setShots([]);
      setRefused([]);
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
        {t(reopens ? "feedback.still_not_fixed_reply" : "feedback.reply")}
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
      onPaste={(e: ClipboardEvent<HTMLFormElement>) => {
        const files = [...e.clipboardData.files].filter((f) => f.type.startsWith("image/"));
        if (files.length === 0 || !limits) return;
        e.preventDefault();
        void add(files);
      }}
      data-action-submit="feedback.reply.send"
      onSubmit={(e) => { e.preventDefault(); void send(); }}
    >
      <label htmlFor={id} className="fbk-label">{item.needsInput ? t("feedback.your_reply") : t(reopens ? "feedback.tell_us_what_is_still" : "feedback.reply_to_the_maintainers")}</label>
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
          {reopens && t("feedback.sending_this_reopens_the_report")}{" "}
          {item.issueNumber
            ? t("feedback.this_report_already_has_a", { n: item.issueNumber })
            : t("feedback.sent_to_the_maintainers_only")}
        </span>
        <span id={`${id}-count`} className="fbk-count" data-over={over ? "" : undefined}>
          {t("feedback.n_max_bytes", { n: bytes.toLocaleString(), max: max.toLocaleString() })}
        </span>
      </div>
      {limits && (
        <div className="fbk-reply-shots">
          {shots.length > 0 && (
            <ul className="fbk-shots">
              {shots.map((s) => (
                <FeedbackShot key={s.id} shot={s} disabled={sending} onRemove={() => setShots((prev) => prev.filter((x) => x.id !== s.id))} />
              ))}
            </ul>
          )}
          <div className="fbk-drop-line">
            <button type="button" className="btn sm" data-action="feedback.shot.pick" disabled={sending || shots.length >= limits.images} onClick={() => picker.current?.click()}>
              <StudioIcon name="plus" />
              {t("feedback.add_screenshots")}
            </button>
            <span className="fbk-hint">
              {t("feedback.you_can_also_paste_png", { n: limits.images, size: megabytes(limits.uploadBytes) })}{" "}
              {t("feedback.screenshots_go_to_the_maintainers")}
            </span>
          </div>
          <input ref={picker} className="sr-only" type="file" accept="image/png,image/jpeg" multiple tabIndex={-1} aria-label={t("feedback.choose_screenshot_files")} data-action="feedback.shot.pick" onChange={(e) => { void add([...(e.target.files ?? [])]); e.target.value = ""; }} />
          {refused.length > 0 && (
            <ul className="fbk-refused" role="status">
              {refused.map((r, i) => (
                <li key={i}>{t(REFUSED_LABEL[r.why], { name: r.name, size: megabytes(limits.uploadBytes), n: limits.images })}</li>
              ))}
            </ul>
          )}
        </div>
      )}
      {failure && (
        <div className="fbk-note" role="alert" data-tone="error" data-code={failure.code}>
          <StudioIcon name="warning" />
          <span>{failure.message}</span>
          {failure.code === FEEDBACK_CODE.unavailable && (
            <button type="button" className="btn sm" data-action="feedback.link" onClick={() => onFile(FEEDBACK_REPO_ISSUES + "new/choose")}>
              {t("feedback.go_to_github")}
            </button>
          )}
        </div>
      )}
      <div className="fbk-acts">
        <button className="act" type="submit" data-primary disabled={blocked}>{t(sending ? "common.sending" : "feedback.send_reply")}</button>
        {!item.needsInput && (
          <button className="act" type="button" data-action="feedback.reply.cancel" disabled={sending} onClick={() => { setOpen(false); setFailure(null); requestAnimationFrame(() => opener.current?.focus()); }}>
            {t("common.cancel")}
          </button>
        )}
      </div>
    </form>
  );
}
