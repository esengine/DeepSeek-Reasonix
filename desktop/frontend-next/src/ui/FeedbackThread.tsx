import { useCallback, useMemo, useRef, useState, type ClipboardEvent } from "react";
import { t } from "../i18n";
import { FEEDBACK_CODE, FEEDBACK_REPO_ISSUES, canReplyTo, isUnderReview, type FeedbackItem, type FeedbackLimits, type FeedbackReply } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { replyFailure, type FeedbackFailure } from "./feedbackfailure";
import { FeedbackShot } from "./FeedbackShot";
import { admit, megabytes, payload, readShot, REFUSED_LABEL, type Refused, type Shot } from "./feedbackshots";
import { LevelBadge } from "./LevelBadge";
import { stamp } from "./feedbackstatus";
import { StudioIcon } from "./StudioIcon";

const encoder = new TextEncoder();

// Used only when the limit could not be read; the kernel still refuses an oversized reply.
const REPLY_BYTES_FALLBACK = 4096;

const AUTHOR_LABEL = { maintainer: "feedback.maintainers", user: "chat.you" } as const;

interface ThreadProps {
  item: FeedbackItem;
  // The reporter's own level, shown beside their label; null or absent shows none.
  level?: number | null;
  fresh: number;
}

// Replies are plain text by contract: they render as text nodes, never linked,
// formatted or interpreted, whoever wrote them.
export function FeedbackThread({ item, level = null, fresh }: ThreadProps) {
  const replies = item.replies;
  const newIds = useMemo(() => new Set(fresh > 0 ? replies.filter((r) => r.author === "maintainer").slice(-fresh).map((r) => r.id) : []), [replies, fresh]);
  const isNew = (r: FeedbackReply) => newIds.has(r.id);
  if (replies.length === 0) return null;
  return (
    <ol className="fbk-thread" aria-label={t("feedback.conversation")}>
      {replies.map((r) => (
        <li key={r.id} data-author={r.author} data-new={isNew(r) ? "" : undefined}>
          <div className="fbk-msg-hd">
            <b>{t(AUTHOR_LABEL[r.author])}</b>
            {r.author === "user" && level !== null && (
              <>
                <LevelBadge level={level} decorative size={14} />
                <span className="sr-only">{t("feedback.level_n", { n: level })}</span>
              </>
            )}
            <span aria-hidden="true">·</span>
            <time dateTime={r.createdAt}>{stamp(r.createdAt)}</time>
            {isNew(r) && <span className="fbk-new">{t("common.new")}</span>}
          </div>
          <p className="fbk-msg" dir="auto">{r.body}</p>
        </li>
      ))}
    </ol>
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
  // Unsent text per report, so moving to another report and back keeps it.
  drafts?: Map<string, string>;
}

export function FeedbackReplyBox({ port, item, limit, limits = null, offline, onSent, onFile, onStale, drafts }: ReplyProps) {
  const [text, setTyped] = useState(() => drafts?.get(item.receipt) ?? "");
  const setText = (next: string) => {
    setTyped(next);
    if (next) drafts?.set(item.receipt, next);
    else drafts?.delete(item.receipt);
  };
  const [sending, setSending] = useState(false);
  const [failure, setFailure] = useState<FeedbackFailure | null>(null);
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
      onSent(item.receipt);
    } catch (e) {
      const f = replyFailure(e);
      setFailure(f);
      if (f.code === FEEDBACK_CODE.notReplyable) onStale();
    } finally {
      setSending(false);
    }
  };

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
      <label htmlFor={id} className="sr-only">{item.needsInput ? t("feedback.your_reply") : t(reopens ? "feedback.tell_us_what_is_still" : "feedback.reply_to_the_maintainers")}</label>
      <div className="fbk-reply-box">
        <textarea
          id={id}
          ref={field}
          data-action="feedback.reply.body"
          value={text}
          rows={2}
          disabled={sending}
          placeholder={t(reopens ? "feedback.tell_us_what_is_still" : "feedback.reply_placeholder")}
          aria-invalid={over ? true : undefined}
          aria-describedby={`${id}-hint ${id}-count`}
          onChange={(e) => setText(e.target.value)}
        />
        {limits && shots.length > 0 && (
          <ul className="fbk-shots">
            {shots.map((s) => (
              <FeedbackShot key={s.id} shot={s} disabled={sending} onRemove={() => setShots((prev) => prev.filter((x) => x.id !== s.id))} />
            ))}
          </ul>
        )}
      </div>
      <p id={`${id}-hint`} className="fbk-hint fbk-reply-hint">
        {reopens && t("feedback.sending_this_reopens_the_report")}{" "}
        {item.issueNumber
          ? t("feedback.this_report_already_has_a", { n: item.issueNumber })
          : t("feedback.sent_to_the_maintainers_only")}
        {limits && <>{" "}{t("feedback.screenshots_go_to_the_maintainers")}</>}
      </p>
      {limits && refused.length > 0 && (
        <ul className="fbk-refused" role="status">
          {refused.map((r, i) => (
            <li key={i}>{t(REFUSED_LABEL[r.why], { name: r.name, size: megabytes(limits.uploadBytes), n: limits.images })}</li>
          ))}
        </ul>
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
      <div className="fbk-reply-foot">
        {limits && (
          <>
            <button type="button" className="fbk-attach" data-action="feedback.shot.pick" disabled={sending || shots.length >= limits.images}
              title={t("feedback.you_can_also_paste_png", { n: limits.images, size: megabytes(limits.uploadBytes) })} onClick={() => picker.current?.click()}>
              <StudioIcon name="image" />
              {t("feedback.attach_image")}
            </button>
            <input ref={picker} className="sr-only" type="file" accept="image/png,image/jpeg" multiple tabIndex={-1} aria-label={t("feedback.choose_screenshot_files")} data-action="feedback.shot.pick" onChange={(e) => { void add([...(e.target.files ?? [])]); e.target.value = ""; }} />
          </>
        )}
        <span id={`${id}-count`} className="fbk-count" data-over={over ? "" : undefined}>
          {t("feedback.n_max_bytes", { n: bytes.toLocaleString(), max: max.toLocaleString() })}
        </span>
        <button className="fbk-send" type="submit" disabled={blocked}>
          <StudioIcon name="send" />
          {t(sending ? "common.sending" : "common.send")}
        </button>
      </div>
    </form>
  );
}
