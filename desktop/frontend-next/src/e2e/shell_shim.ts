// The window the client expects, installed before anything imports it. Only the
// stream is real: the probe plays the part the server plays, forwarding the
// kernel's own frames onto a transport that never reconnects — which is the
// one the bootstrap was written for.
const listeners = new Set<(raw: string) => void>();

export function deliver(raw: string) {
  for (const cb of listeners) cb(raw);
}

class ShimEventSource {
  onmessage: ((m: { data: string }) => void) | null = null;
  private readonly feed = (raw: string) => this.onmessage?.({ data: raw });

  constructor() {
    listeners.add(this.feed);
  }

  close() {
    listeners.delete(this.feed);
  }
}

(globalThis as { window?: unknown }).window = {};
(globalThis as { EventSource?: unknown }).EventSource = ShimEventSource;
