import { useSyncExternalStore } from "react";

// The small notices a failed action leaves in the window's corner. They do not
// time out: only the person who was told decides they were read. A fourth one
// pushes the oldest out, so the corner never grows past what fits beside the work.
export interface Toast {
  id: number;
  text: string;
  // A note tells the person what the window did on its own; it is not a failure.
  tone?: "note";
  // A way out the notice offers; running it dismisses the notice.
  action?: { label: string; run: () => void };
}

export const MAX_TOASTS = 3;

let toasts: readonly Toast[] = [];
let serial = 0;
const listeners = new Set<() => void>();

const publish = (next: readonly Toast[]) => {
  toasts = next;
  for (const listener of listeners) listener();
};

/** Adds a notice, unless the same words are already showing. */
export function pushToast(text: string): void {
  if (!text.trim() || toasts.some((toast) => toast.text === text)) return;
  publish([...toasts, { id: ++serial, text }].slice(-MAX_TOASTS));
}

/** Adds a note about something the window did by itself: drawn calmly, no report button, never takes focus. */
export function pushNote(text: string): void {
  if (!text.trim() || toasts.some((toast) => toast.text === text)) return;
  publish([...toasts, { id: ++serial, text, tone: "note" as const }].slice(-MAX_TOASTS));
}

/** A note with one button that does what the note offers. */
export function pushOffer(text: string, action: { label: string; run: () => void }): void {
  if (!text.trim() || toasts.some((toast) => toast.text === text)) return;
  publish([...toasts, { id: ++serial, text, tone: "note" as const, action }].slice(-MAX_TOASTS));
}

export function dismissToast(id: number): void {
  publish(toasts.filter((toast) => toast.id !== id));
}

export function clearToasts(): void {
  publish([]);
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => void listeners.delete(listener);
};

export const readToasts = (): readonly Toast[] => toasts;

export function useToasts(): readonly Toast[] {
  return useSyncExternalStore(subscribe, readToasts, readToasts);
}
