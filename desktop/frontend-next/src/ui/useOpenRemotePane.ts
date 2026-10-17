import { useCallback, useRef, type Dispatch, type SetStateAction } from "react";
import type { HubPort, RuntimeView } from "../port/hub";
import type { SessionSwitch } from "./sessionswitch";

export function useOpenRemotePane({ hub, reloadPanes, reloadRemotes, sw, setActive, setOpening }: {
  hub: HubPort;
  reloadPanes: () => Promise<void>;
  reloadRemotes: () => Promise<void>;
  sw: SessionSwitch;
  setActive: (id: string) => void;
  setOpening: Dispatch<SetStateAction<number>>;
}) {
  const flights = useRef(new Map<string, Promise<RuntimeView>>());
  return useCallback(async (host: string, workspace?: string, sessionPath?: string) => {
    const key = JSON.stringify(["remote", host, workspace, sessionPath]);
    if (sessionPath && sw.opening(key)) return;
    const ticket = sw.begin(key);
    setOpening((n) => n + 1);
    try {
      let flight = sessionPath ? flights.current.get(key) : undefined;
      if (!flight) {
        flight = (async () => {
          const view = await hub.openRemote({ host, workspace, sessionPath });
          await reloadPanes();
          return view;
        })();
        if (sessionPath) {
          const started = flight;
          flights.current.set(key, started);
          const forget = () => { if (flights.current.get(key) === started) flights.current.delete(key); };
          started.then(forget, forget);
        }
      }
      const view = await flight;
      if (sw.current(ticket)) setActive(view.id);
    } finally {
      sw.end(ticket);
      setOpening((n) => n - 1);
      void reloadRemotes();
    }
  }, [hub, reloadPanes, reloadRemotes, sw.opening, sw.begin, sw.current, sw.end, setActive, setOpening]);
}
