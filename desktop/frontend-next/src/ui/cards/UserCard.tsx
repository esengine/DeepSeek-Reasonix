import type { Checkpoint, RewindPlan, RewindResult, RewindScope } from "../../port/port";
import type { Item } from "../../state/session";
import { RewindControl } from "./RewindControl";
import { t } from "../../i18n";

export function UserCard({
  item,
  cp,
  onCancelQueued,
  onPrepareRewind,
  onCommitRewind,
  onUndoRewind,
}: {
  item: Extract<Item, { t: "user" }>;
  cp?: Checkpoint;
  onCancelQueued?: (rowId: string, itemId: string) => void;
  onPrepareRewind?: (turn: number, scope: RewindScope) => Promise<RewindPlan>;
  onCommitRewind?: (planId: string) => Promise<RewindResult>;
  onUndoRewind?: (transactionId: string) => Promise<void>;
}) {
  // A queued line is the only thing on screen that has not happened yet, which
  // makes it the only thing that can still be taken back. The id is the
  // kernel's, and it goes away the moment the turn reads the line.
  const queued = item.pending ? item.itemId : undefined;
  return (
    // data-item is the rail's anchor: it jumps by block first, then finds the
    // message itself once that block has mounted.
    <div className="call" data-k="me" data-item={item.id} data-pending={item.pending ? "" : undefined}>
      <div className="g">
        <span className="sym">{t("你")}</span>
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("我")}</span>
          {item.pending && (
            <span className="pend">
              {item.queued === "followup" ? t("排队中 · 这一轮跑完就发") : t("排队中 · 下一个工具边界送达")}
            </span>
          )}
          {queued && onCancelQueued && (
            <button className="pcancel" data-action="queue.cancel" data-target={item.id} onClick={() => onCancelQueued(item.id, queued)}>
              {t("撤回")}
            </button>
          )}
          {/* The entry point lives on the turn it returns to, so there is no
              list to read and no turn number to match up by eye. */}
          {cp && onPrepareRewind && onCommitRewind && onUndoRewind && (
            <RewindControl cp={cp} onPrepare={onPrepareRewind} onCommit={onCommitRewind} onUndo={onUndoRewind} />
          )}
        </div>
        <div className="out">
          <div className="txt">{item.text}</div>
        </div>
      </div>
    </div>
  );
}
