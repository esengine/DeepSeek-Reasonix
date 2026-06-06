import React, { createContext, type PropsWithChildren, useMemo, useSyncExternalStore } from 'react';
import {
  getTerminalFocused,
  getTerminalFocusState,
  subscribeTerminalFocus,
  type TerminalFocusState,
} from '../terminal-focus-state.js';

export type { TerminalFocusState };

export type TerminalFocusContextProps = {
  readonly isTerminalFocused: boolean;
  readonly terminalFocusState: TerminalFocusState;
};

/** Exposes the terminal-window focus state (DECSET 1004) to descendants. */
const TerminalFocusContext = createContext<TerminalFocusContextProps>({
  isTerminalFocused: true,
  terminalFocusState: 'unknown',
});

// eslint-disable-next-line custom-rules/no-top-level-side-effects
TerminalFocusContext.displayName = 'TerminalFocusContext';

export function TerminalFocusProvider({ children }: PropsWithChildren) {
  const isTerminalFocused = useSyncExternalStore(subscribeTerminalFocus, getTerminalFocused);
  const terminalFocusState = useSyncExternalStore(subscribeTerminalFocus, getTerminalFocusState);

  const value = useMemo(
    () => ({ isTerminalFocused, terminalFocusState }),
    [isTerminalFocused, terminalFocusState],
  );

  return <TerminalFocusContext.Provider value={value}>{children}</TerminalFocusContext.Provider>;
}

export default TerminalFocusContext;
