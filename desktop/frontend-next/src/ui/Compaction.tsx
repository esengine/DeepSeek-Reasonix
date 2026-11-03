import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, CompactionSettings } from "../port/port";

const tokens = (n: number) => (n >= 1000 ? `${Math.round(n / 1000)}k` : String(n));

export function Compaction({ port }: { port: AgentPort }) {
  const [box, setBox] = useState<CompactionSettings | null>(null);
  const [used, setUsed] = useState<number | null>(null);
  useEffect(() => {
    port
      .compaction()
      .then(setBox)
      .catch(() => setBox(null));
    // The threshold is a number until it is next to the session's own usage,
    // and then it is a distance. A gauge that fails to load costs the reader
    // the distance, not the setting.
    port.context().then((c) => setUsed(c.used)).catch(() => setUsed(null));
  }, [port]);

  if (!box) return <div className="empty">{t("compaction.theCompactionSettingsCouldNot")}</div>;

  const win = box.context_window;
  const capacity = Math.round(win * box.ratio);
  const off = win === 0;
  const pct = off || !used ? 0 : Math.min((used / box.trigger) * 100, 100);

  return (
    <>
      <div className="cmp">
        {off ? (
          <p className="note">{t("compaction.thisSourceDeclaresNoContext")}</p>
        ) : (
          <>
            <div className="cmpg">
              <div className="r">
                <span className="k">{t("common.now")}</span>
                <b>{used === null ? "—" : tokens(used)}</b>
              </div>
              <div className="r">
                <span className="k">{t("compaction.nextMaintenance")}</span>
                <b>{tokens(box.trigger)}</b>
              </div>
              <div className="r">
                <span className="k">{t("compaction.modelWindow")}</span>
                <b>{tokens(win)}</b>
              </div>
            </div>
            {used !== null && (
              <div className="cmpbar" role="presentation">
                <i style={{ width: `${pct}%` }} />
              </div>
            )}
            <p className="note">{t("compaction.thisSessionShouldTidyIts", { n: tokens(box.trigger) })}</p>
          </>
        )}
      </div>

      <div className="lrow">
            <span className="tx">
              <span className="lb">{t("compaction.capacityGuard")}</span>
              <span className="ds">
                {t("compaction.compactsAtOfTheModel", { p: String(Math.round(box.ratio * 100)) })}
              </span>
            </span>
            <span className="sc">{off ? "—" : tokens(capacity)}</span>
      </div>
    </>
  );
}
