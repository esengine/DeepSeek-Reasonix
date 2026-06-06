/** Shimmed stdout 'resize' to one real listener + a subscriber Set. */

import type { WriteStream } from "node:tty";

type Listener = (...args: unknown[]) => void;
type Registrar = (event: string | symbol, listener: Listener) => WriteStream;

interface State {
  stream: WriteStream;
  subscribers: Set<Listener>;
}

let state: State | null = null;

export function installResizeBroadcaster(stream: WriteStream = process.stdout): void {
  if (state) return;
  if (typeof stream.on !== "function" || typeof stream.off !== "function") return;

  const subscribers = new Set<Listener>();
  const realOn = stream.on.bind(stream) as Registrar;
  const realOff = stream.off.bind(stream) as Registrar;

  const broadcast: Listener = (...args) => {
    for (const l of subscribers) l(...args);
  };
  realOn("resize", broadcast);

  const shimOn: Registrar = (event, listener) => {
    if (event === "resize") {
      subscribers.add(listener);
      return stream;
    }
    return realOn(event, listener);
  };
  const shimOff: Registrar = (event, listener) => {
    if (event === "resize") {
      subscribers.delete(listener);
      return stream;
    }
    return realOff(event, listener);
  };

  stream.on = shimOn as WriteStream["on"];
  stream.addListener = shimOn as WriteStream["addListener"];
  stream.off = shimOff as WriteStream["off"];
  stream.removeListener = shimOff as WriteStream["removeListener"];

  state = { stream, subscribers };
}

export function _uninstallResizeBroadcaster(): void {
  state = null;
}

export function _resizeSubscriberCount(): number {
  return state ? state.subscribers.size : 0;
}
