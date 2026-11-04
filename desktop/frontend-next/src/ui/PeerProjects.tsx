import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort, TreeWorkspace } from "../port/hub";
import type { AgentPort } from "../port/port";
import type { PeersSettingsStore } from "./usePeersSettings";

// The projects whose sessions take a message from the user's other sessions
// without a card. Adding one is choosing from the projects the person has already
// added to this window; taking one back is always one press. The list is written
// whole, so it is read again just before each change: another window may have
// changed it since this one looked.
export function PeerProjects({ port, hub, settings }: { port: AgentPort; hub: Pick<HubPort, "tree">; settings: PeersSettingsStore }) {
  const { state, publish } = settings;
  const [known, setKnown] = useState<TreeWorkspace[]>([]);
  const [pick, setPick] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    hub.tree({ limit: 1 }).then((tree) => alive && setKnown(tree.filter((w) => w.remembered && w.root.length > 0)), () => alive && setKnown([]));
    return () => { alive = false; };
  }, [hub]);

  const write = useCallback(async (change: (current: string[]) => string[]) => {
    setBusy(true);
    setError("");
    try {
      const fresh = await port.peersSettings();
      publish(await port.savePeersSettings({ release_projects: change(fresh.release_projects.map((p) => p.path)) }));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  }, [port, publish]);

  const shownError = error || settings.loadError;
  if (!state) return shownError ? <div className="why" role="alert">{shownError}</div> : null;
  const released = new Set(state.release_projects.map((p) => p.path));
  const choices = known.filter((w) => !released.has(w.root));
  const target = choices.some((w) => w.root === pick) ? pick : choices[0]?.root ?? "";
  const off = busy || !state.available;

  return (
    <div className="box">
      {state.release_projects.length > 0 ? (
        <ul className="peerreleased" aria-label={t("remote.projects_released_for_good")}>
          {state.release_projects.map((p) => (
            <li key={p.path}>
              <span className="tx"><span className="lb">{p.name}</span><span className="ds">{p.path}</span></span>
              <button className="act" data-action="peers.release-revoke" data-target={p.path} disabled={off}
                aria-label={t("remote.revoke_release_name", { name: p.name })}
                onClick={() => void write((now) => now.filter((path) => path !== p.path))}>
                {t("remote.revoke")}
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <div className="peernote">{t("remote.no_project_is_allowed_through")}</div>
      )}
      <div className="peerform">
        {!state.enabled ? (
          <div className="peernote">{t("remote.turn_on_session_messages_first")}</div>
        ) : choices.length === 0 ? (
          <div className="peernote">{t("remote.there_is_no_project_to")}</div>
        ) : (
          <>
            <label className="peerhead" htmlFor="peer-release-pick">{t("remote.allow_this_project_through")}</label>
            <select id="peer-release-pick" className="peerfield" data-action-change="peers.release-pick" aria-label={t("remote.allow_this_project_through")} value={target} disabled={off} onChange={(e) => setPick(e.target.value)}>
              {choices.map((w) => <option key={w.root} value={w.root}>{`${w.name} — ${w.root}`}</option>)}
            </select>
            <div className="acts">
              <button className="act" data-action="peers.release-add" data-primary disabled={off || !target} onClick={() => void write((now) => [...now, target])}>{t("common.allow")}</button>
            </div>
          </>
        )}
      </div>
      {!state.available && <div className="peernote" role="status">{t("remote.not_supported_in_this_session")}</div>}
      {state.pending && <div className="peernote" role="status">{t("remote.saved_takes_effect_from_the")}</div>}
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
