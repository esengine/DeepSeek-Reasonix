import type { ReactNode } from "react";

/** A named group of gauges. Nine equal blocks read as one unfilled form: every
 *  label the same size, no answer to "which of these belong together". A group
 *  states the question its rows answer, which is what lets a glance skip six
 *  of them. */
// id names the block the way the panels that build their own frame already do,
// so what the rail put where can be read off the document rather than guessed
// from the words inside it.
export function Grp({ id, name, aside, children }: { id: string; name: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <section className="dgrp" data-b={id}>
      <h3 className="dgrp-h">
        {name}
        {aside != null && <span className="a">{aside}</span>}
      </h3>
      {children}
    </section>
  );
}

export function Row({ k, v, tone }: { k: ReactNode; v: ReactNode; tone?: string }) {
  return (
    <div className="drow" data-tone={tone}>
      <span className="k">{k}</span>
      <span className="v">{v}</span>
    </div>
  );
}

/** Drawn only where a real denominator exists. A bar without one invents a
 *  ceiling out of whatever the largest value happened to be, and the reader
 *  believes it — so token totals get a number and no bar. */
export function Bar({ pct, tone }: { pct: number; tone?: string }) {
  return (
    <div className="gbar" data-tone={tone}>
      <i style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} />
    </div>
  );
}
