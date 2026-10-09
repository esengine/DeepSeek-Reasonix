import { nextId } from "./ids";
import type { SessionState } from "./session_types";

export const QUEUE_HELD = "queue_paused_hold";


// The kernel answers a queued line with the id it queued it under. The row is
// already on screen by then: this gives it a name to be taken back by.
export function nameQueued(s: SessionState, id: string, itemId: string, queued: "steer" | "followup", paused = false): SessionState {
  // Guidance read at a tool boundary can be reported before its receipt lands
  // here. That report drew the delivered line, so this row is its duplicate and
  // naming it would re-open a line that already arrived.
  if (s.takenBack.includes(itemId)) {
    return { ...s, items: s.items.filter((i) => !(i.t === "user" && i.id === id)) };
  }
  if (s.items.some((i) => i.t === "user" && i.itemId === itemId && i.id !== id)) {
    return { ...s, items: s.items.filter((i) => !(i.t === "user" && i.id === id)) };
  }
  const named = s.items.map((i) => (i.t === "user" && i.id === id ? { ...i, itemId, queued, pending: true } : i));
  const said = !paused || s.runtime.some((n) => n.code === QUEUE_HELD);
  return {
    ...s,
    items: named,
    runtime: said ? s.runtime : [...s.runtime, { id: nextId(), level: "warn", code: QUEUE_HELD, text: "" }],
  };
}
