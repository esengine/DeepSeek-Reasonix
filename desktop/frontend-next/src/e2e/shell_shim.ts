// The window the client expects, installed before anything imports it. Only the
// bus is real: the probe plays the part the desktop shell plays, forwarding the
// kernel's own frames onto a channel that has no reconnect — which is the
// transport the bootstrap was written for.
const listeners = new Set<(raw: string) => void>();

export function deliver(raw: string) {
  for (const cb of listeners) cb(raw);
}

(globalThis as { window?: unknown }).window = {
  runtime: {
    EventsOn(_name: string, cb: (raw: string) => void) {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
  },
};
