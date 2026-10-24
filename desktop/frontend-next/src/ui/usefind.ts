import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { MAX, search, stepped, type FindMode, type Hit } from "../state/find";
import type { Item } from "../state/session";
import { useShowsReceipt } from "../state/foldpref";
import type { ReceiptPref } from "../state/receiptpref";

const NO_HITS: Hit[] = [];

export interface Finding {
  open: boolean;
  query: string;
  index: number;
  focus: number;
  total: number;
  capped: boolean;
  // n moves on every step, so landing on the same occurrence twice is asked for.
  at: { id: string; nth: number; n: number; mode: FindMode } | null;
  ask: (query: string) => void;
  step: (by: 1 | -1) => void;
  close: () => void;
  /** How the query is read. Off is the plain substring search. */
  regex: boolean;
  caseSensitive: boolean;
  setMode: (mode: Partial<FindMode>) => void;
}

/** The find bar's whole state. pulse is the window asking; only the pane that
 *  has focus answers it, so one chord does not open a bar in every session. */
export function useFind(items: Item[], pulse: number, active: boolean, onOpen: () => void, pref?: ReceiptPref, togglePulse = 0): Finding {
  const [held, setHeld] = useState<{ q: string; i: number; focus: number; re?: boolean; cs?: boolean } | null>(null);
  const receipts = useShowsReceipt(pref);
  const hits = useMemo(
    () => (held ? search(items, held.q, receipts, { regex: held.re, caseSensitive: held.cs }) : NO_HITS),
    [items, held, receipts],
  );
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

  // The title bar button is a switch: the same press that opens the bar closes it.
  const seenToggle = useRef(0);
  useEffect(() => {
    if (togglePulse === seenToggle.current) return;
    seenToggle.current = togglePulse;
    if (!togglePulse || !active) return;
    if (held) {
      setHeld(null);
      return;
    }
    onOpen();
    setHeld({ q: "", i: 0, focus: 1 });
  }, [togglePulse, active, held, onOpen]);

  const total = hits.length;
  const ask = useCallback((q: string) => setHeld((was) => ({ q, i: 0, focus: was?.focus ?? 0 })), []);
  const step = useCallback((by: 1 | -1) => setHeld((was) => (was ? { ...was, i: stepped(was.i, total, by) } : was)), [total]);
  const close = useCallback(() => setHeld(null), []);
  // Changing how the query is read restarts from the first hit: the old index
  // counted hits the new reading may no longer have.
  const setMode = useCallback(
    (mode: Partial<FindMode>) => setHeld((was) => (was ? { ...was, ...(mode.regex !== undefined ? { re: mode.regex } : {}), ...(mode.caseSensitive !== undefined ? { cs: mode.caseSensitive } : {}), i: 0 } : was)),
    [],
  );

  return {
    open: held !== null,
    query: held?.q ?? "",
    index: held?.i ?? 0,
    focus: held?.focus ?? 0,
    total,
    capped: total >= MAX,
    at: at ? { id: at.id, nth, n: nonce, mode: { regex: held?.re ?? false, caseSensitive: held?.cs ?? false } } : null,
    ask,
    step,
    close,
    regex: held?.re ?? false,
    caseSensitive: held?.cs ?? false,
    setMode,
  };
}
