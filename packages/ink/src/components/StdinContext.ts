import { createContext } from 'react';
import { EventEmitter } from '../events/emitter.js';
import type { TerminalQuerier } from '../terminal-querier.js';

export type Props = {
  readonly stdin: NodeJS.ReadStream;

  /** Reference-counted raw-mode toggle. */
  readonly setRawMode: (value: boolean) => void;

  /** Whether the configured stdin can enter raw mode (i.e. */
  readonly isRawModeSupported: boolean;

  readonly internal_exitOnCtrlC: boolean;

  readonly internal_eventEmitter: EventEmitter;

  readonly internal_querier: TerminalQuerier | null;
};

const StdinContext = createContext<Props>({
  stdin: process.stdin,

  internal_eventEmitter: new EventEmitter(),
  setRawMode() {},
  isRawModeSupported: false,

  internal_exitOnCtrlC: true,
  internal_querier: null,
});

// eslint-disable-next-line custom-rules/no-top-level-side-effects
StdinContext.displayName = 'InternalStdinContext';

export default StdinContext;
