// What picking the other door on an account actually does.
//
// Two different acts wear one control. On an account nothing is running on, it
// only decides whose model list is shown. On the running one it is a real
// switch: the same model on the other protocol is a different endpoint, so it
// has to go through setModel. The third case is the one that read as a bug —
// the running model has no counterpart on that door, so the runtime cannot
// follow, and moving the control anyway showed a door the session was not on
// until the next reload derived it back.

import type { ModelEntry } from "../port/port";

interface Doors {
  byKind: Record<string, ModelEntry[]>;
}

export type ProtocolSwitch =
  | { do: "show" }
  | { do: "switch"; ref: string }
  | { do: "stay"; model: string };

export function planProtocolSwitch(
  v: Doors | undefined,
  models: ModelEntry[],
  modelRef: string | undefined,
  kind: string,
): ProtocolSwitch {
  if (!v || !Object.values(v.byKind).flat().some((m) => m.ref === modelRef)) return { do: "show" };
  const here = models.find((m) => m.ref === modelRef);
  const same = (v.byKind[kind] ?? []).find((m) => m.model === here?.model);
  if (!same) return { do: "stay", model: here?.model ?? "" };
  return same.ref === modelRef ? { do: "show" } : { do: "switch", ref: same.ref };
}
