import { useCallback, useMemo, useRef, useState } from "react";

export interface SessionSwitch {
  // The row being opened; empty when nothing is, or when what opens has no row yet.
  path: string;
  // True until the opened pane has taken focus; the selection outlives it.
  pending: boolean;
  opening: (path: string) => boolean;
  begin: (path: string) => number;
  current: (ticket: number) => boolean;
  landed: (ticket: number) => void;
  end: (ticket: number) => void;
  cancel: () => void;
}

interface Held {
  path: string;
  pending: boolean;
}

// Owns "which session is being opened". Selection follows the click and a
// ticket orders concurrent opens, so only the last one decides focus and a
// refusal from a superseded open leaves the newer selection alone.
export function useSessionSwitch(): SessionSwitch {
  const [held, setHeld] = useState<Held | null>(null);
  const latest = useRef(0);
  const path = useRef<string | null>(null);

  const begin = useCallback((next: string) => {
    latest.current += 1;
    path.current = next;
    setHeld({ path: next, pending: true });
    return latest.current;
  }, []);
  const opening = useCallback((candidate: string) => candidate !== "" && path.current === candidate, []);
  const current = useCallback((ticket: number) => ticket === latest.current, []);
  const landed = useCallback((ticket: number) => {
    if (ticket === latest.current) setHeld((cur) => (cur ? { ...cur, pending: false } : cur));
  }, []);
  const end = useCallback((ticket: number) => {
    if (ticket !== latest.current) return;
    path.current = null;
    setHeld(null);
  }, []);
  const cancel = useCallback(() => {
    latest.current += 1;
    path.current = null;
    setHeld(null);
  }, []);

  return useMemo(
    () => ({ path: held?.path ?? "", pending: held?.pending ?? false, opening, begin, current, landed, end, cancel }),
    [held, opening, begin, current, landed, end, cancel],
  );
}
