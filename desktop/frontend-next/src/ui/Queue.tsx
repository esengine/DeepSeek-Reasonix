import type { Queue as QueueSnapshot, QueueItem } from "../port/port";
import { t } from "../i18n";
import { BLOCK_WHY } from "../i18n/queue_why";

interface Props {
  queue: QueueSnapshot | null;
  // A turn holding the session is what these lines wait on, and what "send now"
  // has to interrupt. Without it the action is meaningless: an idle queue
  // dispatches on its own.
  running: boolean;
  // Entries whose guidance already stands in the transcript as its own bubble.
  shown?: ReadonlySet<string>;
  onSendNow: (item: QueueItem) => void;
  onToSteer: (item: QueueItem) => void;
  onCancel: (id: string) => void | Promise<void>;
  onRetry: (id: string) => void;
  onPause: (paused: boolean) => void;
}

// The kernel has taken the line: it is in the transcript now, and the entry is
// swept only when the whole turn ends. This strip is what has not happened yet,
// so a taken entry leaves it rather than standing there inert as a second copy.
const taken = (s: QueueItem["state"]) => s === "steer_consumed" || s === "running";

// A host continuation — a finished background job asking the session to pick
// the work back up — is not the person's and belongs with the work it came
// from. It appears here only when it needs a decision.
const theirs = (it: QueueItem) => it.origin !== "host" || it.state === "blocked" || it.state === "uncertain";

// Codes whose entry the kernel knows never reached the model, so sending it
// again cannot repeat an effect. Every other uncertain code may have run.
const RETRY_SAFE = new Set(["steer_unapplied"]);
const retryable = (it: QueueItem) =>
  it.state === "blocked" || (it.state === "uncertain" && !!it.blockCode && RETRY_SAFE.has(it.blockCode));
// Only plain text can become guidance: a picture or a frozen file reference
// travels with a whole turn.
const steerable = (it: QueueItem) =>
  it.origin !== "host" && it.state === "queued" && it.intent === "followup" && !it.images && !it.refs?.length;

/** What the strip draws: the lines nobody has acted on yet. The header's count
 *  of guidance in flight reads this too, so it can never name a line the strip
 *  is not showing. */
export const waiting = (queue: QueueSnapshot | null): QueueItem[] =>
  queue ? queue.items.filter((it) => !taken(it.state) && theirs(it)) : [];

// Each arm calls t() with its own literal: the catalogue is built by reading
// these call sites, and a table of strings looked up later is invisible to it —
// which is how a row would render Chinese inside an English window.
function label(it: QueueItem, n: number): string {
  if (it.origin === "host") {
    if (it.state === "blocked") return t("composer.background_blocked");
    if (it.state === "uncertain") return t("composer.background_uncertain");
    return t("composer.background_job_follow_on");
  }
  switch (it.state) {
    case "steer_accepted":
      return t("composer.steer_accepted");
    case "blocked":
      return t("composer.blocked");
    case "uncertain":
      return t("composer.uncertain");
    default:
      if (it.images && it.state === "queued") return t("composer.has_an_image_sent_when");
      if (it.intent === "steer") return t("composer.steer_queued");
      return `${t("common.queued")} ${n}`;
  }
}

export function Queue({ queue, running, shown, onSendNow, onToSteer, onCancel, onRetry, onPause }: Props) {
  if (!queue) return null;
  const items = waiting(queue).filter((it) => !shown?.has(it.id));
  if (items.length === 0) return null;
  const sendable = running && !queue.paused;

  return (
    <div className="queue" role="group" aria-label={t("composer.pending")}>
      {items.map((it, i) => {
        const coded = it.blockCode ? BLOCK_WHY[it.blockCode] : undefined;
        const why = coded ? t(coded) : it.blockReason;
        const held = it.state === "blocked" || it.state === "uncertain";
        return (
          <div key={it.id} className="qi" data-state={it.state} title={why || undefined}>
            <span className="ql" data-tone={held ? "warn" : undefined} title={it.state === "steer_accepted" ? t("composer.goes_in_at_the_next") : it.origin === "host" ? t("composer.background_job_follow_on") : undefined}>
              {label(it, i + 1)}
            </span>
            <span className="pv">{it.preview}</span>
            {held && why && <span className="qwhy" title={why}>{why}</span>}
            <span className="qacts">
              {queue.paused && i === 0 && !queue.readonly && (
                <button className="qb" data-action="queue.pause" onClick={() => onPause(false)}>{t("composer.release")}</button>
              )}
              {sendable && steerable(it) && (
                <button className="qb" data-action="queue.to-steer" data-target={it.id} onClick={() => onToSteer(it)}>
                  {t("composer.make_it_a_steer")}
                </button>
              )}
              {sendable && (it.state === "queued" || it.state === "steer_accepted") && (
                <button className="qb" data-action="queue.send-now" data-target={it.id} title={t("composer.stop_the_current_response_and")} onClick={() => onSendNow(it)}>
                  {t("composer.send_now")}
                </button>
              )}
              {retryable(it) && (
                <button className="qb" data-action="queue.retry" data-target={it.id} title={t("common.retry")} onClick={() => onRetry(it.id)}>
                  {t("common.retry")}
                </button>
              )}
              {!queue.readonly && (
                <button className="qb" data-action="queue.cancel" data-target={it.id} title={t("composer.take_it_back")} onClick={() => onCancel(it.id)}>
                  {t("composer.withdraw")}
                </button>
              )}
            </span>
          </div>
        );
      })}
    </div>
  );
}
