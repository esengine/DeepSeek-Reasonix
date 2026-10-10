import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { MAX, search, stepped, type Hit } from "../state/find";
import type { Item } from "../state/session";
import { useShowsReceipt } from "../state/foldpref";

const NO_HITS: Hit[] = [];

export interface Finding {
  open: boolean;
  query: string;
  index: number;
  focus: number;
  total: number;
  capped: boolean;
  // n moves on every step, so landing on the same occurrence twice is asked for.
  at: { id: string; nth: number; n: number } | null;
  ask: (query: string) => void;
  step: (by: 1 | -1) => void;
  close: () => void;
}

/** The find bar's whole state. pulse is the window asking; only the pane that
 *  has focus answers it, so one chord does not open a bar in every session. */
export function useFind(items: Item[], pulse: number, active: boolean, onOpen: () => void): Finding {
  const [held, setHeld] = useState<{ q: string; i: number; focus: number } | null>(null);
  const receipts = useShowsReceipt();
  const hits = useMemo(() => (held ? search(items, held.q) : NO_HITS), [items, held, receipts]);
  const i = held ? Math.min(held.i, hits.length - 1) : -1;
  const at = i >= 0 ? hits[i] : null;
  // Hits come in reading order, so a row's occurrences sit together.
  let nth = 0;
  while (at && i - nth > 0 && hits[i - nth - 1].id === at.id) nth++;

  const [nonce, setNonce] = useState(0);
  useEffect(() => {
    if (at) setNonce((n) => n + 1);
  }, [at?.id, at?.at]);

  const seen = useRef(0);
  useEffect(() => {
    if (pulse === seen.current) return;
    seen.current = pulse;
    if (!pulse || !active) return;
    onOpen();
    setHeld((was) => (was ? { ...was, focus: was.focus + 1 } : { q: "", i: 0, focus: 1 }));
  }, [pulse, active, onOpen]);

  const total = hits.length;
  const ask = useCallback((q: string) => setHeld((was) => ({ q, i: 0, focus: was?.focus ?? 0 })), []);
  const step = useCallback((by: 1 | -1) => setHeld((was) => (was ? { ...was, i: stepped(was.i, total, by) } : was)), [total]);
  const close = useCallback(() => setHeld(null), []);

  return {
    open: held !== null,
    query: held?.q ?? "",
    index: held?.i ?? 0,
    focus: held?.focus ?? 0,
    total,
    capped: total >= MAX,
    at: at ? { id: at.id, nth, n: nonce } : null,
    ask,
    step,
    close,
  };
}
