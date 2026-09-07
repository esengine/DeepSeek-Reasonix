import { createOperationOwner, type OperationTarget } from "./operationOwner";

export type PollClock = { setTimeout(callback: () => void, delay: number): unknown; clearTimeout(handle: unknown): void };
type PollInput<T> = {
  target: OperationTarget; periodMs: number; clock: PollClock;
  read(): Promise<T>; publish(value: T): void; failed(error: unknown): void;
};
type PollState<T> = {
  input?: PollInput<T>; owner: ReturnType<typeof createOperationOwner>;
  epoch: number; timer?: unknown; pending?: Promise<void>;
};

async function sample<T>(state: PollState<T>): Promise<void> {
  if (!state.input) return;
  const identity = state.owner.begin(state.input.target, undefined, "poll");
  const read = state.input.read;
  let status: "completed" | "failed" = "completed";
  try {
    const value = await read();
    if (!state.owner.owns(identity)) return;
    state.input?.publish(value);
  } catch (error) {
    status = "failed";
    if (!state.owner.owns(identity)) return;
    state.input?.failed(error);
  } finally { state.owner.finish(identity, status); }
}
function bindRefresh<T>(state: PollState<T>): () => Promise<void> {
  const refresh = (): Promise<void> => {
    if (!state.input) return Promise.resolve();
    if (state.pending) return state.pending;
    if (state.timer !== undefined) { state.input.clock.clearTimeout(state.timer); state.timer = undefined; }
    const pending = sample(state).finally(() => {
      if (state.pending !== pending) return;
      state.pending = undefined;
      if (state.input) state.timer = state.input.clock.setTimeout(() => { state.timer = undefined; void refresh(); }, state.input.periodMs);
    });
    state.pending = pending;
    return pending;
  };
  return refresh;
}

/** Single-flight polling. Disposal releases sinks and cancels queued delivery synchronously. */
export function createPollingOwner<T>(input: PollInput<T>, track?: (delta: 1 | -1) => void) {
  const owner = createOperationOwner(track);
  const state: PollState<T> = { input, owner, epoch: owner.mount() };
  const refresh = bindRefresh(state);
  return {
    refresh,
    dispose() {
      if (!state.input) return;
      if (state.timer !== undefined) state.input.clock.clearTimeout(state.timer);
      state.timer = undefined;
      state.input = undefined;
      state.owner.unmount(state.epoch);
    },
  };
}
