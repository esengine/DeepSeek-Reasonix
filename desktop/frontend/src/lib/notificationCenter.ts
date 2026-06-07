// notificationCenter is the in-app side of the desktop's notification
// surface. Long-running agent events (idle, plan approval needed,
// update available, plugin hot-add, job done) deserve a visible
// signal, and the OS notification center is the right home for them,
// but the kernel doesn't yet expose a Notify() binding. This module
// gives the rest of the app a single store for in-app notifications
// so when the binding lands, the only change is "also dispatch
// runtime.MessageDialog" alongside the in-app emit.
//
// The store is a tiny pub-sub on top of a plain array. We avoid
// pulling in a state library (Zustand, Jotai) for a 60-line feature.
// Subscribers are React components: the NotificationCenter drawer
// subscribes on mount and unsubscribes on unmount.
//
// The store caps at MAX (50 by default). New notifications push
// older ones out; the cap is high enough that a busy day of agent
// runs (maybe 30 notifications) stays in-memory. We don't persist
// across reloads — reloads are rare and the user can re-trigger
// the source if they need to.

export type NotificationLevel = "info" | "warn" | "error";

export interface Notification {
  id: string;
  level: NotificationLevel;
  title: string;
  body: string;
  // at is the wall-clock ms; used to display '3h ago' in the drawer.
  at: number;
  // source is a short label (e.g. 'agent', 'update', 'plugin') the
  // drawer uses to group / filter. Optional.
  source?: string;
  // read is flipped the first time the user opens the drawer; the
  // unread count in the topbar uses this to render a small dot.
  read: boolean;
}

type Listener = (list: Notification[]) => void;

const MAX = 50;
let store: Notification[] = [];
const listeners = new Set<Listener>();

function emit() {
  for (const l of listeners) l(store);
}

// push adds a notification to the store. Identical-`id` events (the
// agent emits "agent:idle" once per turn) are deduped — the existing
// entry's at is refreshed and read flipped back to false, so the
// user gets a "fresh" signal but the list doesn't grow.
export function push(n: Omit<Notification, "id" | "at" | "read"> & { id?: string }): void {
  const id = n.id ?? `${n.source ?? "app"}-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
  const existing = store.find((x) => x.id === id);
  if (existing) {
    existing.at = Date.now();
    existing.read = false;
    existing.body = n.body;
  } else {
    store = [{ ...n, id, at: Date.now(), read: false }, ...store];
    if (store.length > MAX) store.length = MAX;
  }
  emit();
}

// markAllRead flips every entry's read flag; called when the user
// opens the drawer.
export function markAllRead(): void {
  let changed = false;
  for (const n of store) {
    if (!n.read) {
      n.read = true;
      changed = true;
    }
  }
  if (changed) emit();
}

// clear wipes the store. Useful for a 'Clear all' button in the
// drawer; also handy for tests.
export function clear(): void {
  store = [];
  emit();
}

// subscribe registers a listener. Returns the unsubscribe function.
export function subscribe(l: Listener): () => void {
  listeners.add(l);
  l(store);
  return () => {
    listeners.delete(l);
  };
}

// unread returns the count of unread entries. Cheap O(n) over at
// most 50 items.
export function unread(): number {
  let c = 0;
  for (const n of store) if (!n.read) c++;
  return c;
}

// snapshot is a defensive copy; used by the drawer on each render.
export function snapshot(): Notification[] {
  return store.slice();
}
