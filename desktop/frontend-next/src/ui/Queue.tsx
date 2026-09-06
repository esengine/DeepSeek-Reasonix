import { useCallback, useEffect, useRef, useState } from "react";
import type { Queue as QueueSnapshot, QueueItem } from "../port/port";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { Overflow } from "./Overflow";

interface Props {
  queue: QueueSnapshot | null;
  onRead: (id: string) => Promise<string>;
  onEdit: (id: string, text: string) => void;
  onMove: (id: string, to: number) => void;
  onCancel: (id: string) => void;
  onRetry: (id: string) => void;
  onRefresh: (id: string) => void;
  onPause: (paused: boolean) => void;
}

// What each state means for the row's own affordances. A consumed entry is
// history the kernel has not swept yet: it is past taking back.
const settled = (s: QueueItem["state"]) => s === "steer_consumed" || s === "running";

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
    case "steer_consumed":
      return t("已送入");
    case "running":
      return t("进行中");
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
    case "running":
      return "accent";
    case "steer_consumed":
      return "ok";
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

export function Queue({ queue, onRead, onEdit, onMove, onCancel, onRetry, onRefresh, onPause }: Props) {
  const [editing, setEditing] = useState("");
  const [draft, setDraft] = useState("");
  // Which row could not be read back, and why. Not a draft: the editor stays
  // shut, because the row's own text is the only thing that may fill it.
  const [unread, setUnread] = useState<{ id: string; why: string } | null>(null);
  const box = useRef<HTMLTextAreaElement>(null);

  // The body arrives after the click, so focus waits for the field to exist.
  useEffect(() => {
    if (editing) box.current?.focus();
  }, [editing]);

  const open = useCallback(
    async (id: string) => {
      // Opening on the preview would put a cut-off line in the box and save it
      // back as the whole instruction.
      setUnread(null);
      let body: string;
      try {
        body = await onRead(id);
      } catch (e) {
        // A read that failed is not an empty instruction. Filling the box with
        // "" would have the user retype a line they never saw, and the save
        // replaces the whole entry with it.
        setUnread({ id, why: reason(e) });
        return;
      }
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

  if (!queue || (queue.items.length === 0 && !queue.paused)) return null;
  const items = queue.items;
  const cap = queue.capacity;
  const fullItems = cap.maxItems > 0 && cap.items >= cap.maxItems;
  const fullBytes = cap.maxBytes > 0 && cap.bytes >= cap.maxBytes;

  return (
    <div className="queue" role="group" aria-label={t("待送达")}>
      <div className="qhd">
        <span className="lb">{t("待送达")}</span>
        {queue.paused && <span className="qflag" data-hold="">{t("已暂停")}</span>}
        {queue.readonly && <span className="qflag" data-ro="">{t("只读")}</span>}
        {/* The kernel pauses itself when it recovers entries: something was
            said that no one has since re-confirmed. */}
        {!!queue.recoveredCount && <span className="qflag" data-warn="">{t("恢复了 {n} 条", { n: queue.recoveredCount })}</span>}
        {/* Both limits refuse on their own, so a header that showed one of them
            would be wrong about the other exactly when it bites. */}
        <span className="qcap">
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
        </span>
        <button className="qhold" onClick={() => onPause(!queue.paused)} disabled={queue.readonly}>
          {queue.paused ? t("继续派发") : t("暂停派发")}
        </button>
      </div>

      <div className="qitems">
        {items.map((it, i) => {
          const live = !settled(it.state) && !queue.readonly && editing !== it.id;
          return (
            <div key={it.id} className="qi" data-state={it.state}>
              {/* The chip is the answer to "did that land". Its wording says
                  which turn the line goes into; the title says when. */}
              <span className="pill" data-tone={tone(it.state)} title={it.state === "steer_accepted" ? t("下个工具边界送入") : undefined}>
                {label(it)}
              </span>
              {editing === it.id ? (
                <textarea
                  ref={box}
                  className="qedit"
                  value={draft}
                  rows={Math.min(6, draft.split("\n").length)}
                  onChange={(e) => setDraft(e.target.value)}
                  onBlur={commit}
                  onKeyDown={(e) => {
                    if (e.key === "Escape") setEditing("");
                    // Enter commits; the line is one instruction, and a queue
                    // row is not where a paragraph gets composed.
                    if (e.key === "Enter" && !e.shiftKey) {
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
              {it.blockReason && <span className="qwhy">{it.blockReason}</span>}
              {unread?.id === it.id && (
                <span className="qwhy" data-err="" role="alert">
                  {t("读不回这一条的正文，没有打开编辑：{why}", { why: unread.why })}
                </span>
              )}
              {live && (
                <span className="qacts">
                  <button onClick={() => onMove(it.id, i - 1)} disabled={i === 0} title={t("上移")}>
                    ↑
                  </button>
                  <button onClick={() => onMove(it.id, i + 1)} disabled={i === items.length - 1} title={t("下移")}>
                    ↓
                  </button>
                  <button onClick={() => void open(it.id)} title={t("编辑")}>
                    {t("改")}
                  </button>
                  {it.state === "blocked" && (
                    <button onClick={() => onRetry(it.id)} title={t("重试")}>
                      {t("重试")}
                    </button>
                  )}
                  {/* Only an entry that quoted files has anything to re-freeze. */}
                  {!!it.refs?.length && (
                    <button onClick={() => onRefresh(it.id)} title={t("重新冻结引用的文件")}>
                      {t("刷新")}
                    </button>
                  )}
                </span>
              )}
              {live && (
                <button className="x" onClick={() => onCancel(it.id)} title={t("取回")}>
                  ×
                </button>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
