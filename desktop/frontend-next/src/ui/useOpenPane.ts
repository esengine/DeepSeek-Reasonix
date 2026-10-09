import { type Dispatch, type RefObject, type SetStateAction, useCallback, useRef } from "react";
import type { HubPort, RuntimeView } from "../port/hub";
import type { AgentPort } from "../port/port";
import type { PaneReport } from "./Pane";
import type { SessionSwitch } from "./sessionswitch";

// Keep a small warm set for instant back-and-forth switching. Older settled
// panes are cheap to restore from disk and expensive to leave mounted: every
// hidden pane retains a transcript, observers and markdown tree.
const WARM_PANES = 4;

interface Placed {
  id: string;
  changed: boolean;
}

interface Deps {
  hub: HubPort;
  runtimes: RuntimeView[];
  panePorts: Map<string, AgentPort>;
  active: string;
  reportsRef: RefObject<Record<string, PaneReport>>;
  runsRef: RefObject<Record<string, { run: string; live: boolean }>>;
  setRuntimes: Dispatch<SetStateAction<RuntimeView[]>>;
  setActive: (id: string) => void;
  setTakeover: Dispatch<SetStateAction<Record<string, number>>>;
  reloadPanes: () => Promise<void>;
  sw: SessionSwitch;
}

// Opening a session is one path for every caller: the row, the new-session
// buttons and the quick switcher. The ticket in `sw` orders concurrent opens, so
// only the latest one moves focus and a failure of an older one changes nothing.
export function useOpenPane({ hub, runtimes, panePorts, active, reportsRef, runsRef, setRuntimes, setActive, setTakeover, reloadPanes, sw }: Deps) {
  const placePane = useCallback(
    async (req: { root?: string; sessionPath?: string }): Promise<Placed> => {
      // Blank means never written to and not working: a new session mid-turn has
      // no path in a stale pane list yet, and taking it over would hide its run.
      const blank = runtimes.find(
        (rt) => !rt.sessionPath && !reportsRef.current[rt.id]?.status?.sessionPath && !runsRef.current[rt.id]?.live,
      );
      // Asking for a new session when an unused one is already open in that
      // folder: it is the pane being asked for. Rebuilding it would cost a full
      // assembly to arrive back where we started.
      if (blank && !req.sessionPath && blank.root === req.root) {
        return { id: blank.id, changed: false };
      }
      // Same folder: the pane just rebinds, so nothing is torn down and a draft
      // in its composer survives. The kernel refuses a path from another
      // project's session dir, which is why the root has to match.
      if (blank && req.sessionPath && blank.root === req.root) {
        await panePorts.get(blank.id)?.resume(req.sessionPath);
        setTakeover((prev) => ({ ...prev, [blank.id]: (prev[blank.id] ?? 0) + 1 }));
        return { id: blank.id, changed: true };
      }
      // One visible pane plus live background work is the product model. Keep a
      // few settled panes warm for quick backtracking, then reuse the oldest
      // idle pane in the same workspace instead of accumulating full hidden
      // transcripts until the kernel refuses another open.
      const idle = runtimes.filter((rt) => rt.id !== active && !runsRef.current[rt.id]?.live);
      const atCapacity = runtimes.length >= WARM_PANES;
      const reusable = atCapacity && req.sessionPath
        ? idle.find((rt) => rt.root === req.root && panePorts.has(rt.id))
        : undefined;
      if (reusable && req.sessionPath) {
        await panePorts.get(reusable.id)?.resume(req.sessionPath);
        setTakeover((prev) => ({ ...prev, [reusable.id]: (prev[reusable.id] ?? 0) + 1 }));
        return { id: reusable.id, changed: true };
      }
      // A different workspace cannot be resumed into the same runtime. Retire
      // one settled background pane before opening so the row never reaches the
      // old "nothing happens" max-pane failure.
      let retired = "";
      if (atCapacity && idle[0]) {
        retired = idle[0].id;
        await hub.close(retired);
      }
      const rt = await hub.open(req);
      // Another folder needs its own runtime, so the blank one is retired
      // rather than left behind.
      if (blank && blank.id !== rt.id) await hub.close(blank.id);
      setRuntimes((prev) => [...prev.filter((pane) => pane.id !== rt.id && pane.id !== blank?.id && pane.id !== retired), rt]);
      return { id: rt.id, changed: true };
    },
    [hub, runtimes, panePorts, active, reportsRef, runsRef, setRuntimes, setTakeover],
  );

  const flights = useRef(new Map<string, Promise<Placed>>());
  const openPane = useCallback(
    async (req: { root?: string; sessionPath?: string }) => {
      const wanted = req.sessionPath ?? "";
      if (wanted && sw.opening(wanted)) return;
      const ticket = sw.begin(wanted);
      try {
        let flight = wanted ? flights.current.get(wanted) : undefined;
        if (!flight) {
          flight = placePane(req);
          if (wanted) {
            const started = flight;
            const forget = () => void (flights.current.get(wanted) === started && flights.current.delete(wanted));
            started.then(forget, forget);
            flights.current.set(wanted, started);
          }
        }
        const placed = await flight;
        if (sw.current(ticket)) {
          setActive(placed.id);
          sw.landed(ticket);
        }
        if (!placed.changed) return sw.end(ticket);
      } catch (e) {
        sw.end(ticket);
        throw e;
      }
      void reloadPanes().finally(() => sw.end(ticket));
    },
    [placePane, setActive, reloadPanes, sw.opening, sw.begin, sw.current, sw.landed, sw.end],
  );

  return openPane;
}
