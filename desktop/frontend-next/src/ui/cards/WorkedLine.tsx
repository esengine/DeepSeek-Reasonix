import { t } from "../../i18n";
import { clock, elapsed } from "../../i18n/format";
import { StudioIcon } from "../StudioIcon";

/** The turn's close: a tick and how long it ran, at the right edge under the
 *  reply. When the turn began is the hover, since the message above carries it. */
export function WorkedLine({ ms, startedAt }: { ms: number; startedAt?: number }) {
  return (
    <div className="rc" data-worked="">
      <div className="rc-ok" title={startedAt ? t("chat.started_at_t", { at: clock(startedAt) }) : undefined}>
        <StudioIcon name="check" className="rc-tick" />
        <span className="rc-t">{t("chat.done_in_t", { t: elapsed(ms) })}</span>
      </div>
    </div>
  );
}
