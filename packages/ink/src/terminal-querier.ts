/** Send queries to the terminal and resolve replies without per-query timeouts. */

import type { TerminalResponse } from './parse-keypress.js';
import { csi } from './termio/csi.js';
import { osc } from './termio/osc.js';

export type TerminalQuery<T extends TerminalResponse = TerminalResponse> = {
  /** Sequence to write to stdout. */
  request: string;
  /** Returns true when `r` is this query's reply. */
  match: (r: TerminalResponse) => r is T;
};

type DecrpmResponse = Extract<TerminalResponse, { type: 'decrpm' }>;
type Da1Response = Extract<TerminalResponse, { type: 'da1' }>;
type Da2Response = Extract<TerminalResponse, { type: 'da2' }>;
type KittyResponse = Extract<TerminalResponse, { type: 'kittyKeyboard' }>;
type CursorPosResponse = Extract<TerminalResponse, { type: 'cursorPosition' }>;
type OscResponse = Extract<TerminalResponse, { type: 'osc' }>;
type XtversionResponse = Extract<TerminalResponse, { type: 'xtversion' }>;

// --- Query builders -------------------------------------------------------

/** DECRQM — request DEC private mode status (CSI ? mode $ p). */
export function decrqm(mode: number): TerminalQuery<DecrpmResponse> {
  return {
    request: csi(`?${mode}$p`),
    match: (r): r is DecrpmResponse => r.type === 'decrpm' && r.mode === mode,
  };
}

/** DA1 — Primary Device Attributes (CSI c). */
export function da1(): TerminalQuery<Da1Response> {
  return {
    request: csi('c'),
    match: (r): r is Da1Response => r.type === 'da1',
  };
}

/** DA2 — Secondary Device Attributes (CSI > c). Returns terminal version. */
export function da2(): TerminalQuery<Da2Response> {
  return {
    request: csi('>c'),
    match: (r): r is Da2Response => r.type === 'da2',
  };
}

/** Query the current Kitty keyboard protocol flags (CSI ? u). */
export function kittyKeyboard(): TerminalQuery<KittyResponse> {
  return {
    request: csi('?u'),
    match: (r): r is KittyResponse => r.type === 'kittyKeyboard',
  };
}

/** DECXCPR — cursor position with the DEC-private marker (CSI ? 6 n). */
export function cursorPosition(): TerminalQuery<CursorPosResponse> {
  return {
    request: csi('?6n'),
    match: (r): r is CursorPosResponse => r.type === 'cursorPosition',
  };
}

export function oscColor(code: number): TerminalQuery<OscResponse> {
  return {
    request: osc(code, '?'),
    match: (r): r is OscResponse => r.type === 'osc' && r.code === code,
  };
}

/** XTVERSION — terminal name + version (CSI > 0 q). */
export function xtversion(): TerminalQuery<XtversionResponse> {
  return {
    request: csi('>0q'),
    match: (r): r is XtversionResponse => r.type === 'xtversion',
  };
}

// --- Querier --------------------------------------------------------------

/** Internal DA1 byte sequence used by `flush()` as a universal barrier. */
const SENTINEL = csi('c');

type PendingQuery = {
  kind: 'query';
  match: (r: TerminalResponse) => boolean;
  resolve: (r: TerminalResponse | undefined) => void;
};

type PendingSentinel = {
  kind: 'sentinel';
  resolve: () => void;
};

type Pending = PendingQuery | PendingSentinel;

export class TerminalQuerier {
  /** Interleaved queue of queries and sentinels in send order. */
  private queue: Pending[] = [];

  constructor(private stdout: NodeJS.WriteStream) {}

  /** Write a query to stdout and wait for its reply. */
  send<T extends TerminalResponse>(
    query: TerminalQuery<T>,
  ): Promise<T | undefined> {
    return new Promise(resolve => {
      this.queue.push({
        kind: 'query',
        match: query.match,
        resolve: r => resolve(r as T | undefined),
      });
      this.stdout.write(query.request);
    });
  }

  /** Write the DA1 sentinel and wait for its reply. */
  flush(): Promise<void> {
    return new Promise(resolve => {
      this.queue.push({ kind: 'sentinel', resolve });
      this.stdout.write(SENTINEL);
    });
  }

  /** Hand a parsed terminal response to the querier. */
  onResponse(r: TerminalResponse): void {
    const idx = this.queue.findIndex(p => p.kind === 'query' && p.match(r));
    if (idx !== -1) {
      const [q] = this.queue.splice(idx, 1);
      if (q?.kind === 'query') q.resolve(r);
      return;
    }

    if (r.type === 'da1') {
      const s = this.queue.findIndex(p => p.kind === 'sentinel');
      if (s === -1) return;
      for (const p of this.queue.splice(0, s + 1)) {
        if (p.kind === 'query') p.resolve(undefined);
        else p.resolve();
      }
    }
  }
}
