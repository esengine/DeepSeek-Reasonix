import { useCallback, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, PeersSettings, PeersSettingsEdit } from "../port/port";
import type { PeersSettingsStore } from "./usePeersSettings";
import { Switch } from "./Switch";

/** The switches behind session messages, which used to be hand-written config.
 *  Saved values only reach the model at the next turn boundary — the tool table
 *  changes with them — so a saved edit says exactly that until the kernel does. */
export function PeerSettings({ port, settings, onSaved }: { port: AgentPort; settings: PeersSettingsStore; onSaved?: (next: PeersSettings) => void }) {
  const { state, publish } = settings;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const save = useCallback(async (edit: PeersSettingsEdit) => {
    setBusy(true);
    setError("");
    try {
      const next = await port.savePeersSettings(edit);
      publish(next);
      onSaved?.(next);
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  }, [port, publish, onSaved]);

  const shownError = error || settings.loadError;
  if (!state) return shownError ? <div className="why" role="alert">{shownError}</div> : null;
  const off = busy || !state.available;

  return (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("peerSettings.turnOnSessionMessages")}</span>
          <span className="ds">{t("peerSettings.sessionsOnThisMachineSend")}</span>
        </span>
        <Switch data-action="peers.enable" on={state.enabled} busy={off} label={t("peerSettings.turnOnSessionMessages")} onClick={() => void save({ enabled: !state.enabled })} />
      </div>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("peerSettings.letTheModelHandA")}</span>
          <span className="ds">{state.enabled ? t("peerSettings.givesTheModelATool") : t("peerSettings.turnOnSessionMessagesFirst")}</span>
        </span>
        <Switch data-action="peers.dispatch" on={state.dispatch} busy={off || !state.enabled} label={t("peerSettings.letTheModelHandA")} onClick={() => void save({ dispatch: !state.dispatch })} />
      </div>
      {!state.available && <div className="peernote" role="status">{t("common.notSupportedInThisSession")}</div>}
      {state.pending && <div className="peernote" role="status">{t("common.savedTakesEffectFromThe")}</div>}
      <div className="peernote">{t("peerSettings.theseSwitchesAreKeptIn")}<code>{state.path}</code></div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
