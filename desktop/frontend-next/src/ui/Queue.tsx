import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { Queue as QueueSnapshot, QueueItem } from "../port/port";
import { t } from "../i18n";
import { useFitHeight } from "./fitHeight";
import { reason } from "../i18n/kernel";
import { BLOCK_WHY } from "../i18n/queue_why";

import { Overflow } from "./Overflow";
import { StudioIcon } from "./StudioIcon";
import { touchKeyboard } from "./touchKeyboard";

interface Props {
  queue: QueueSnapshot | null;
  // A turn holding the session is the whole reason these are waiting, and it is
  // what "send now" has to interrupt. Without it the action is meaningless: an
  // idle queue dispatches on its own.
  running: boolean;
  onRead: (id: string) => Promise<string>;
  onEdit: (id: string, text: string) => void;
  onMove: (id: string, to: number) => void;
  onCancel: (id: string) => void | Promise<void>;
  onSendNow: (item: QueueItem) => void;
  onRetry: (id: string) => void;
  onRefresh: (id: string) => void;
  onPause: (paused: boolean) => void;
}

// The kernel has taken the line: it is in the transcript now, and the entry is
// swept only when the whole turn ends. This panel is what has not happened yet,
// so a taken entry leaves it rather than standing there inert as a second copy.
const taken = (s: QueueItem["state"]) => s === "steer_consumed" || s === "running";

// "To send" is what the person said and has not sent yet. A host continuation —
// a finished background job asking the session to pick the work back up — is
// not theirs and belongs with the work it came from. It appears here only when
// it needs a decision, which is the one case nobody else can make for them.
const theirs = (it: QueueItem) => it.origin !== "host" || it.state === "blocked" || it.state === "uncertain";

// Codes whose entry the kernel knows never reached the model, so sending it
// again cannot repeat an effect. Every other uncertain code may have run.
const RETRY_SAFE = new Set(["steer_unapplied"]);
const retryable = (it: QueueItem) =>
  it.state === "blocked" || (it.state === "uncertain" && !!it.blockCode && RETRY_SAFE.has(it.blockCode));

/** What the panel draws: the lines nobody has acted on yet. The header's count
 *  of guidance in flight reads this too, so it can never name a line the panel
 *  is not showing. */
export const waiting = (queue: QueueSnapshot | null): QueueItem[] =>
  queue ? queue.items.filter((it) => !taken(it.state) && theirs(it)) : [];

// Each arm calls t() with its own literal: the catalogue is built by reading
// these call sites, and a table of strings looked up later is invisible to it —
// which is how a row would render Chinese inside an English window.
function label(it: QueueItem): string {
  if (it.origin === "host" && it.state !== "blocked" && it.state !== "uncertain") {
    return t("后台任务续接");
  }
  switch (it.state) {
    case "steer_accepted":
      return t("插话已收");
    case "blocked":
      return t("受阻");
    case "uncertain":
      return t("状态不明");
    default:
      // A steer the kernel could not admit is converted to a follow-up, so an
      // entry still calling itself one is waiting on a held queue, not refused.
      return it.intent === "steer" ? t("插话排队") : t("排队");
  }
}

function tone(state: QueueItem["state"]): string | undefined {
  switch (state) {
    case "steer_accepted":
      return "accent";
    case "blocked":
    case "uncertain":
      return "warn";
    default:
      return undefined;
  }
}

// Each side of the ratio picks its own unit. The ceiling is 64 MiB and what a
// queue actually holds is a few typed lines, so a shared unit would print every
// real queue as 0 — the bar is what carries the proportion.
function size(n: number): string {
  if (n < 1024) return `${n}B`;
  if (n < 1 << 20) return `${n < 10240 ? (n / 1024).toFixed(1) : Math.round(n / 1024)}k`;
  const m = n / (1 << 20);
  return `${m < 10 ? m.toFixed(1) : Math.round(m)}M`;
}
const fill = (n: number, max: number) => (max > 0 ? `${Math.min(100, Math.round((n / max) * 100))}%` : "0%");

// The list scrolls inside a height `.composeaux` caps, so an editor taller than
// what the list shows is cut off with no way to reach its edge.
function listRoom(el: HTMLTextAreaElement): number {
  const list = el.closest(".qitems");
  const row = el.closest(".qi");
  return list && row ? list.clientHeight - (row.getBoundingClientRect().height - el.offsetHeight) : Infinity;
}

export function Queue({ queue, running, onRead, onEdit, onMove, onCancel, onSendNow, onRetry, onRefresh, onPause }: Props) {
  const [editing, setEditing] = useState("");
  const [draft, setDraft] = useState("");
  // Which row could not be read back, and why. Not a draft: the editor stays
  // shut, because the row's own text is the only thing that may fill it.
  const [unread, setUnread] = useState<{ id: string; why: string } | null>(null);
  const box = useRef<HTMLTextAreaElement>(null);
  const readEpoch = useRef(0);
  useFitHeight(box, editing ? draft : "", undefined, listRoom);
  useLayoutEffect(() => {
    const row = box.current?.closest<HTMLElement>(".qi");
    const list = row?.closest<HTMLElement>(".qitems");
    if (!row || !list) return;
    const over = row.getBoundingClientRect().bottom - list.getBoundingClientRect().bottom;
    const under = list.getBoundingClientRect().top - row.getBoundingClientRect().top;
    list.scrollTop += under > 0 ? -under : Math.max(0, over);
  }, [editing, draft]);

  // The body arrives after the click, so focus waits for the field to exist.
  useEffect(() => {
    if (editing) box.current?.focus();
  }, [editing]);

  const open = useCallback(
    async (id: string) => {
      // Opening on the preview would put a cut-off line in the box and save it
      // back as the whole instruction.
      const epoch = ++readEpoch.current;
      setUnread(null);
      let body: string;
      try {
        body = await onRead(id);
      } catch (e) {
        if (epoch !== readEpoch.current) return;
        // A read that failed is not an empty instruction. Filling the box with
        // "" would have the user retype a line they never saw, and the save
        // replaces the whole entry with it.
        setUnread({ id, why: reason(e) });
        return;
      }
      if (epoch !== readEpoch.current) return;
      setDraft(body);
      setEditing(id);
    },
    [onRead],
  );

  const commit = useCallback(() => {
    const text = draft.trim();
    if (editing && text) onEdit(editing, text);
    setEditing("");
  }, [draft, editing, onEdit]);

  // No pending work means no pending UI. A persisted pause is an internal
  // queue policy; drawing a full strip for it above an empty composer makes it
  // look like a stuck task. If a new item arrives while held, the strip returns
  // with both the item and the action needed to release it.
  if (!queue) return null;
  const items = waiting(queue);
  if (items.length === 0) return null;
  const cap = queue.capacity;
  const fullItems = cap.maxItems > 0 && cap.items >= cap.maxItems;
  const fullBytes = cap.maxBytes > 0 && cap.bytes >= cap.maxBytes;
  const capacityPressure = fullItems || fullBytes ||
    (cap.maxItems > 0 && cap.items / cap.maxItems >= 0.75) ||
    (cap.maxBytes > 0 && cap.bytes / cap.maxBytes >= 0.75);
  const ordinary = (it: QueueItem) => it.origin !== "host" && it.state === "queued" && it.intent === "followup";

  return (
    <div className="queue" role="group" aria-label={t("待送达")}>
      <div className="qhd">
        <span className="lb">{t("待发送")}</span>
        <span className="qcount" aria-label={t("{n} 条待发送", { n: items.length })}>{items.length}</span>
        <span className="qsummary">
          {queue.readonly
            ? t("会话暂时只读，消息已为你保留")
            : queue.paused
              ? t("发送已暂停")
              : running
                ? t("尚未送达，可以改，也可以取回")
                : t("正在送出…")}
        </span>
        {queue.paused && <span className="qflag" data-hold="">{t("已暂停")}</span>}
        {queue.readonly && <span className="qflag" data-ro="">{t("只读")}</span>}
        {/* The kernel pauses itself when it recovers entries: something was
            said that no one has since re-confirmed. */}
        {!!queue.recoveredCount && <span className="qflag" data-warn="">{t("恢复了 {n} 条", { n: queue.recoveredCount })}</span>}
        {/* Both limits refuse on their own, so a header that showed one of them
            would be wrong about the other exactly when it bites. */}
        {capacityPressure && <span className="qcap" aria-label={t("队列容量")}>
          <span data-full={fullItems || undefined}>
            {t("条目")} {cap.items}/{cap.maxItems}
            <span className="mtr">
              <i style={{ width: fill(cap.items, cap.maxItems) }} />
            </span>
          </span>
          <span data-full={fullBytes || undefined}>
            {t("字节")} {size(cap.bytes)}/{size(cap.maxBytes)}
            <span className="mtr">
              <i style={{ width: fill(cap.bytes, cap.maxBytes) }} />
            </span>
          </span>
        </span>}
        <button className="qhold" data-action="queue.pause" onClick={() => onPause(!queue.paused)} disabled={queue.readonly}>
          {queue.paused ? t("继续派发") : t("暂停派发")}
        </button>
      </div>

      <div className="qitems">
        {items.map((it, i) => {
          const live = !queue.readonly && editing !== it.id;
          const coded = it.blockCode ? BLOCK_WHY[it.blockCode] : undefined;
          const why = coded ? t(coded) : it.blockReason;
          return (
            <div key={it.id} className="qi" data-state={it.state}>
              {/* The chip is the answer to "did that land". Its wording says
                  which turn the line goes into; the title says when. */}
              {ordinary(it) ? (
                <span className="qdot" aria-hidden="true" />
              ) : (
                <span className="pill" data-tone={tone(it.state)} title={it.state === "steer_accepted" ? t("下个工具边界送入") : undefined}>
                  {label(it)}
                </span>
              )}
              {editing === it.id ? (
                <textarea
                    data-action-keydown="queue.edit"
                  ref={box}
                  className="qedit"
                  value={draft}
                  rows={1}
                  onChange={(e) => setDraft(e.target.value)}
                  onBlur={commit}
                  onKeyDown={(e) => {
                    if (e.key === "Escape") {
                      e.preventDefault();
                      setEditing("");
                    }
                    // Enter commits; the line is one instruction, and a queue
                    // row is not where a paragraph gets composed.
                    if (e.key === "Enter" && !e.shiftKey && !touchKeyboard() && !e.nativeEvent.isComposing) {
                      e.preventDefault();
                      commit();
                    }
                  }}
                />
              ) : (
                // 「改」走 onRead 读回整条正文，但已结算的行没有那个按钮。
                // 被截断时看得见全文，是每一行都该有的，而不是可编辑的那些。
                <Overflow className="pv" text={it.preview} />
              )}
              {!!it.refs?.length && <span className="rf">{t("冻结 {n} 文件", { n: it.refs.length })}</span>}
              {why && <span className="qwhy">{why}</span>}
              {unread?.id === it.id && (
                <span className="qwhy" data-err="" role="alert">
                  {t("无法读取该条的正文，未打开编辑：{why}", { why: unread.why })}
                </span>
              )}
              {live && (
                <span className="qacts">
                  {items.length > 1 && <>
                    <button data-action="queue.move" data-target={it.id} data-value="up" onClick={() => onMove(it.id, i - 1)} disabled={i === 0} title={t("上移")} aria-label={t("上移")}>
                      <StudioIcon name="arrow" />
                    </button>
                    <button data-action="queue.move" data-target={it.id} data-value="down" onClick={() => onMove(it.id, i + 1)} disabled={i === items.length - 1} title={t("下移")} aria-label={t("下移")}>
                      <StudioIcon name="arrow" data-flip="" />
                    </button>
                  </>}
                  <button data-action="queue.edit" data-target={it.id} onClick={() => void open(it.id)} title={t("编辑")}>
                    {t("改")}
                  </button>
                  {/* Waiting is the normal case and needs no button. This is the
                      other one: the turn in front of this line is not worth
                      finishing, and nothing else can end it. */}
                  {running && !queue.paused && (it.state === "queued" || it.state === "steer_accepted") && (
                    <button
                      className="qnow"
                      data-action="queue.send-now"
                      data-target={it.id}
                      onClick={() => onSendNow(it)}
                      title={t("停止当前回复，把这条作为新一轮立即发出")}
                    >
                      {t("立即发送")}
                    </button>
                  )}
                  {retryable(it) && (
                    <button data-action="queue.retry" data-target={it.id} onClick={() => onRetry(it.id)} title={t("重试")}>
                      {t("重试")}
                    </button>
                  )}
                  {/* Only an entry that quoted files has anything to re-freeze. */}
                  {!!it.refs?.length && (
                    <button data-action="queue.refresh" data-target={it.id} onClick={() => onRefresh(it.id)} title={t("重新冻结引用的文件")}>
                      {t("刷新")}
                    </button>
                  )}
                </span>
              )}
              {live && (
                <button className="x" data-action="queue.cancel" data-target={it.id} onClick={() => onCancel(it.id)} title={t("取回")}>
                  {t("取回")}
                </button>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
