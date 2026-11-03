import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { HeldPeerMessage } from "./HeldPeerMessage";
import { peerLabel } from "./peerKeys";
import { PeerSettings } from "./PeerSettings";
import { usePeers } from "./usePeers";
import type { PeersSettingsStore } from "./usePeersSettings";

const POLL_MS = 2000;

export function PeerMessages({ port, settings }: { port: AgentPort; settings: PeersSettingsStore }) {
  const { state, failed, refresh } = usePeers(port, POLL_MS);
  const [to, setTo] = useState("");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [sent, setSent] = useState("");

  const switches = <PeerSettings port={port} settings={settings} onSaved={() => void refresh()} />;
  if (failed && !state.enabled) return <>{switches}<div className="empty">{t("peerMessages.couldNotReadTheState")}</div></>;
  if (!state.enabled) return switches;
  const target = to || state.peers[0]?.key || "";
  const send = async () => {
    setBusy(true);
    setError("");
    setSent("");
    try {
      const receipt = await port.sendPeerMessage(target, text);
      setSent(t("peerMessages.deliveredToItIsRead", { to: peerLabel(receipt.to) }));
      setText("");
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
    {switches}
    <div className="box">
      {state.held.map((h) => (
        <HeldPeerMessage key={h.id} port={port} held={h} onDone={() => void refresh()} />
      ))}
      {state.peers.length === 0 ? (
        <div className="peerhead">{t("peerMessages.noOtherSessionOnThis")}</div>
      ) : (
        <div className="peerform">
          <label className="peerhead" htmlFor="peer-to">{t("peerMessages.sendTo")}</label>
          <select id="peer-to" className="peerfield" data-action-change="peers.target" aria-label={t("peerMessages.sendTo")} value={target} onChange={(e) => setTo(e.target.value)}>
            {state.peers.map((p) => (
              <option key={p.key} value={p.key}>{peerLabel(p)}</option>
            ))}
          </select>
          <textarea
            className="peerfield"
            data-action-change="peers.text"
            aria-label={t("peerMessages.message")}
            rows={3}
            value={text}
            disabled={busy}
            placeholder={t("peerMessages.writeWhatToTellThe")}
            onChange={(e) => setText(e.target.value)}
          />
          {error && <div className="why">{error}</div>}
          {sent && <div className="peerhead">{sent}</div>}
          <div className="acts">
            <button className="act" data-action="peers.send" data-primary disabled={busy || !text.trim() || !target} onClick={() => void send()}>
              {t(busy ? "peerMessages.sending" : "common.send")}
            </button>
          </div>
        </div>
      )}
    </div>
    </>
  );
}
