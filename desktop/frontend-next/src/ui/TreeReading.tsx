import { useEffect, useState } from "react";
import { seconds } from "../i18n/format";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

// A first read that has not answered by now is slower than anything ordinary.
const STILL_READING_MS = 15_000;

// Display only, timed from this row's mount or from the last retry; whether the
// tree is read is `treeRead`. Past fifteen seconds it says so and, where the
// window can run one, offers a fresh request in place of waiting on the first.
export function TreeReading({ onRetry }: { onRetry?: () => void }) {
  const [since, setSince] = useState(() => Date.now());
  const [now, setNow] = useState(since);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const ms = now - since;
  const slow = ms >= STILL_READING_MS;
  return (
    <div className="ws-empty ws-reading" role="status" aria-busy="true">
      <StudioIcon name="clock" />
      <span>{slow ? t("workbench.still_reading_the_first_read") : t("workbench.reading_folders")}</span>
      {/* Outside the live region's speech: a status that re-announces itself every second is noise. */}
      {ms >= 1000 && <span className="ws-reading-t" aria-hidden="true">{seconds(ms, 0)}</span>}
      {slow && onRetry && (
        <button type="button" className="ws-reading-retry" data-action="tree.retry" onClick={() => { setSince(Date.now()); setNow(Date.now()); onRetry(); }}>
          {t("common.retry")}
        </button>
      )}
    </div>
  );
}

/** What a list with no rows in hand says: still reading while the rows belong to
 *  another view, and "no match" only once the kernel has answered for this one. */
export function TreeEmpty({ settled, onRetry }: { settled: boolean; onRetry?: () => void }) {
  return settled ? <div className="ws-empty">{t("common.nothing_matches")}</div> : <TreeReading onRetry={onRetry} />;
}
