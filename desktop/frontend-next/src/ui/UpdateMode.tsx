import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, UpdateMode as Mode } from "../port/port";
import { refreshUpdateWatch } from "../state/updatewatch";
import { SegChoice, SettingRow } from "./SettingRow";

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

  const modes: [Mode, string][] = [["manual", t("about.manual")], ["notify", t("about.notify")], ["download", t("about.download")]];
  return (
    <SettingRow id="update-mode" title={t("about.update_mode")} desc={t("about.whether_to_check_for_and")}>
      <SegChoice<Mode> label={t("about.update_mode")} value={mode} disabled={busy} action="update.mode" onPick={(next) => void choose(next)} options={modes} />
      {error && <span className="why">{error}</span>}
    </SettingRow>
  );
}
