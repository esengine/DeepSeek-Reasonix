import { createContext } from 'react';

export type TerminalSize = {
  columns: number;
  rows: number;
};

/** Live terminal viewport dimensions, updated on every SIGWINCH. */
export const TerminalSizeContext = createContext<TerminalSize | null>(null);
