import { createContext, useContext } from "react";

export type ExtPicked = { kind: "package" | "server" | "skill"; name: string };

interface Pick {
  picked: ExtPicked | null;
  pick: (next: ExtPicked) => void;
}

// Present only on the extensions page, where a row is chosen into the detail
// pane rather than unfolded in place.
export const ExtPick = createContext<Pick | null>(null);

/** The row's part in choosing, or null where rows unfold in place. */
export function usePickRow(kind: ExtPicked["kind"], name: string) {
  const c = useContext(ExtPick);
  if (!c) return null;
  return { on: c.picked?.kind === kind && c.picked.name === name, pick: () => c.pick({ kind, name }) };
}

/** A press on the row itself, not on a control drawn inside it. */
export const onRowItself = (target: EventTarget | null) => !(target instanceof Element && target.closest("button, input, a, select, [role=switch]"));
