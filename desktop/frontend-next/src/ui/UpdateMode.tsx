import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, UpdateMode as Mode } from "../port/port";
import { refreshUpdateWatch } from "../state/updatewatch";

// How the window treats new releases on its own. No choice installs or restarts
// anything; the choice lives in the user's file and no project can change it.
export function UpdateMode({ port }: { port: AgentPort }) {
  const [mode, setMode] = useState<Mode | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port.updateMode().then(setMode).catch(() => setMode(null));
  }, [port]);

  if (!mode) return null;

  const choose = async (next: Mode) => {
    if (next === mode || busy) return;
    setBusy(true);
    setError("");
    try {
      setMode(await port.setUpdateMode(next));
      void refreshUpdateWatch();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const modes: { id: Mode; name: string; says: string }[] = [
    { id: "manual", name: t("common.manual"), says: t("updateMode.nothingIsCheckedInThe") },
    { id: "notify", name: t("common.notify"), says: t("updateMode.readsTheVersionCatalogOnce") },
    { id: "download", name: t("common.download2"), says: t("updateMode.onTopOfNotifyDownloads") },
  ];
  const says = modes.find((m) => m.id === mode)?.says ?? "";
  return (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("updateMode.checkForNewVersionsIn")}</span>
          <span className="ds">{says}</span>
        </span>
        <div className="seg" data-text role="radiogroup" aria-label={t("common.updateMode")}>
          {modes.map((m) => (
            <button key={m.id} role="radio" data-action="update.mode" data-value={m.id} aria-checked={mode === m.id} disabled={busy} onClick={() => void choose(m.id)}>
              {m.name}
            </button>
          ))}
        </div>
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );
}
