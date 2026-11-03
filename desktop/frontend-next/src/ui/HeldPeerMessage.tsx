import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, PeerHeld, PeerProject, PeerRemember } from "../port/port";
import { heldLabel } from "./peerKeys";

export function HeldPeerMessage({ port, held, project, onDone, compact }: { port: AgentPort; held: PeerHeld; project?: PeerProject; onDone: () => void; compact?: boolean }) {
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(!compact);
  const [error, setError] = useState("");
  const decide = async (approve: boolean, remember: PeerRemember = "none") => {
    setBusy(true);
    setError("");
    try {
      await port.resolvePeerMessage(held.id, approve, remember);
      onDone();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="peerheld" role="group" aria-label={t("heldPeerMessage.messageFrom", { from: heldLabel(held) })}>
      <div className="peerrow">
        <span className="peerhead" title={t("heldPeerMessage.textFromAnotherSessionYou", { from: heldLabel(held) })}>
          {compact ? t("heldPeerMessage.sentAMessageAndIs", { from: heldLabel(held) }) : t("heldPeerMessage.textFromAnotherSessionYou", { from: heldLabel(held) })}
        </span>
        <span className="acts">
          {compact && <button className="act" data-action="peers.expand" aria-expanded={open} onClick={() => setOpen(!open)}>{t(open ? "common.collapse" : "common.expand")}</button>}
          <button className="act" data-action="peers.deny" disabled={busy} onClick={() => void decide(false)}>{t("common.refuse")}</button>
          {project && <button className="act" data-action="peers.approve-project" disabled={busy} aria-describedby={`peer-remember-${held.id}`} onClick={() => void decide(true, "project")}>{t("heldPeerMessage.approveAndRememberProject", { name: project.name })}</button>}
          <button className="act" data-action="peers.approve" data-primary disabled={busy} onClick={() => void decide(true)}>{t("heldPeerMessage.approveAndGiveItTo")}</button>
        </span>
      </div>
      {project && <div className="apv-note" id={`peer-remember-${held.id}`}>{t("heldPeerMessage.rememberProjectRemembersTheProject", { name: project.name, path: project.path })}</div>}
      <pre className="peerbody" data-clamped={open ? undefined : ""}>{held.body}</pre>
      {error && <div className="why">{error}</div>}
    </div>
  );
}
