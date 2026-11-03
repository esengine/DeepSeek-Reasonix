import { t } from "../i18n";
import type { AgentPort, PeerProject } from "../port/port";
import { HeldPeerMessage } from "./HeldPeerMessage";
import { usePeers } from "./usePeers";

const POLL_MS = 3000;

export function PeerHeldBar({ port, project }: { port: AgentPort; project?: PeerProject }) {
  const { state, refresh } = usePeers(port, POLL_MS);
  if (!state.enabled || state.held.length === 0) return null;
  return (
    <section className="peerbar" aria-label={t("peerHeldBar.sessionMessagesWaitingForYour")}>
      {state.held.slice(0, 1).map((h) => (
        <HeldPeerMessage key={h.id} port={port} held={h} project={project} compact onDone={() => void refresh()} />
      ))}
      {state.held.length > 1 && <div className="peerhead">{t("peerHeldBar.moreWaitingForYourDecision", { n: state.held.length - 1 })}</div>}
    </section>
  );
}
