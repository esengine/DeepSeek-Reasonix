// useIntake.ts — the conversation pane is the drop target, not the forty
// pixels of textarea inside it.
//
// Aiming at a one-row input is aiming at 40px, so the whole pane lights up and
// the composer says what letting go will do. Which pane a drop belongs to, and
// whether the payload is a path or bytes, is filedrop.ts's answer; what is left
// here is meaning — and saying it while the drag is still in the air.

import { useCallback, useEffect, useRef, useState } from "react";
import { useFileDrop, type Dropped } from "./filedrop";
import { planIntake, planIsEmpty, planTone, type IntakeItem, type IntakePlan } from "./intake";

interface Options {
  root?: string;
  onReceive: (d: Dropped) => void;
}

export function useIntake({ root, onReceive }: Options): {
  plan: IntakePlan | null;
  ref: (node: HTMLElement | null) => (() => void) | void;
} {
  const [plan, setPlan] = useState<IntakePlan | null>(null);
  // The pane behind the composer, marked so the transcript can recede. Held as
  // a ref rather than looked up per render: the zone node is what the drop
  // system hands over, and the pane is that node's ancestor.
  const pane = useRef<HTMLElement | null>(null);
  const state = useRef({ root, onReceive });
  state.current = { root, onReceive };

  const zone = useFileDrop(
    (d) => {
      setPlan(null);
      state.current.onReceive(d);
    },
    (over, dt) => setPlan(over ? preview(dt, state.current.root) : null),
  );

  const ref = useCallback(
    (node: HTMLElement | null) => {
      pane.current = node?.closest<HTMLElement>(".pane") ?? null;
      const off = zone(node);
      return () => {
        pane.current?.removeAttribute("data-intake");
        pane.current = null;
        off?.();
      };
    },
    [zone],
  );

  useEffect(() => {
    const el = pane.current;
    if (!el) return;
    if (plan && !planIsEmpty(plan)) el.setAttribute("data-intake", planTone(plan));
    else el.removeAttribute("data-intake");
  }, [plan]);

  return { plan: plan && !planIsEmpty(plan) ? plan : null, ref };
}

// preview reads what a drag in the air will say: kinds and MIME types, never a
// path and never bytes. Everything it produces is marked pending.
function preview(dt: DataTransfer | null, root?: string): IntakePlan | null {
  if (!dt) return null;
  const items: IntakeItem[] = [];
  for (const item of Array.from(dt.items ?? [])) {
    if (item.kind === "file") items.push({ kind: "file", mime: item.type, pending: true });
  }
  if (items.length === 0 && carriesText(dt)) items.push({ kind: "text", text: " ", pending: true });
  return planIntake(items, root);
}

function carriesText(dt: DataTransfer): boolean {
  return Array.from(dt.types ?? []).some((type) => type === "text/plain" || type === "text/uri-list");
}
