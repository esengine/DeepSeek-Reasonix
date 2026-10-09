import { useEffect, useRef } from "react";
import type { AgentPort, Checkpoint } from "../port/port";

const RETRY_MS = 1000;

// The kernel opens a turn's checkpoint when the turn starts and fills it while
// it runs, so both turn edges change the list, and so does a move to another
// known session. The first status naming the session the mount read already
// covered changes nothing. No later edge is owed to a failed read, so it is
// tried once more; a read a newer one has superseded is dropped.
export function useCheckpointRefresh(port: AgentPort, sessionPath: string | undefined, running: boolean, set: (cps: Checkpoint[]) => void) {
  const seen = useRef<{ port: AgentPort; path: string | undefined; running: boolean } | null>(null);
  const latest = useRef(0);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    const was = seen.current;
    seen.current = { port, path: sessionPath, running };
    if (was && was.port === port && (was.path === undefined || was.path === sessionPath) && was.running === running) return;
    const mine = ++latest.current;
    const current = () => alive.current && latest.current === mine;
    const read = (retry: boolean) => {
      port.checkpoints().then(
        (cps) => current() && set(cps),
        () => retry && current() && setTimeout(() => current() && read(false), RETRY_MS),
      );
    };
    read(true);
  }, [port, sessionPath, running, set]);
}
