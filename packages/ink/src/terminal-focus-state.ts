/** Tri-state focus tracking driven by DECSET 1004 (xterm focus reporting). */
export type TerminalFocusState = 'focused' | 'blurred' | 'unknown';

let focusState: TerminalFocusState = 'unknown';
const subscribers: Set<() => void> = new Set();
const blurResolvers: Set<() => void> = new Set();

/** Apply a focus-reporting update from the terminal stream. */
export function setTerminalFocused(focused: boolean): void {
  focusState = focused ? 'focused' : 'blurred';

  for (const notify of subscribers) {
    notify();
  }

  if (!focused) {
    for (const resolve of blurResolvers) {
      resolve();
    }
    blurResolvers.clear();
  }
}

export function getTerminalFocused(): boolean {
  return focusState !== 'blurred';
}

export function getTerminalFocusState(): TerminalFocusState {
  return focusState;
}

/** `useSyncExternalStore` `subscribe` argument. */
export function subscribeTerminalFocus(notify: () => void): () => void {
  subscribers.add(notify);
  return () => {
    subscribers.delete(notify);
  };
}

/** Reset focus state to `unknown`. */
export function resetTerminalFocusState(): void {
  focusState = 'unknown';
  for (const notify of subscribers) {
    notify();
  }
}
