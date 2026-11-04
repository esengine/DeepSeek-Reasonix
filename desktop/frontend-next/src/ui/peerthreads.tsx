import { createContext, useCallback, useContext, useEffect, useMemo, type MouseEvent, type ReactNode } from "react";
import { t } from "../i18n";
import type { PeerThread, PeerThreadState } from "../port/boundary";
import type { AgentPort } from "../port/port";
import { StudioIcon, type StudioIconName } from "./StudioIcon";
import { usePeers } from "./usePeers";

interface Threads {
  by: ReadonlyMap<string, PeerThread>;
  open: (path: string) => void;
}

const NONE: Threads = { by: new Map(), open: () => {} };
const PeerThreadsContext = createContext<Threads>(NONE);

/** The sessions this one has written to, keyed by the peer's transcript path.
 *  The kernel's ledger decides each state; the tree-change event that already
 *  moves held messages is what asks for a new reading. */
export function PeerThreadsProvider({ port, hub, onOpen, children }: {
  port: AgentPort | null;
  hub: { onTreeChanged?: (listener: () => void) => () => void };
  onOpen: (path: string) => void;
  children: ReactNode;
}) {
  const { state, refresh } = usePeers(port, 0);
  useEffect(() => hub.onTreeChanged?.(() => void refresh()), [hub, refresh]);
  const by = useMemo(() => {
    const map = new Map<string, PeerThread>();
    if (state.enabled) for (const th of state.threads ?? []) if (th.sessionPath) map.set(th.sessionPath, th);
    return map;
  }, [state]);
  const value = useMemo(() => ({ by, open: onOpen }), [by, onOpen]);
  return <PeerThreadsContext.Provider value={value}>{children}</PeerThreadsContext.Provider>;
}

const ICON: Record<PeerThreadState, StudioIconName> = {
  waiting: "clock",
  working: "refresh",
  replied: "chat",
  declined: "close",
};

function wording(th: PeerThread): string {
  const p = { title: th.title, project: th.project };
  switch (th.state) {
    case "waiting": return t("remote.sent_to_title_project_waiting", p);
    case "working": return t("remote.title_project_accepted_it_and", p);
    case "replied": return t("remote.title_project_replied_to_you", p);
    case "declined": return t("remote.the_user_of_title_project", p);
  }
}

/** One small icon on the row or tab of a session this one has written to. */
export function PeerThreadMark({ path }: { path?: string }) {
  const { by, open } = useContext(PeerThreadsContext);
  const th = path ? by.get(path) : undefined;
  const go = useCallback((ev: MouseEvent) => {
    ev.stopPropagation();
    if (th?.sessionPath) open(th.sessionPath);
  }, [th, open]);
  if (!th) return null;
  const label = wording(th);
  return (
    <button type="button" className="peerthread" data-state={th.state} data-action="peer.thread-open" title={label} aria-label={label} onClick={go}>
      <StudioIcon name={ICON[th.state]} />
    </button>
  );
}
