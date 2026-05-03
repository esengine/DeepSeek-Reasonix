import { type Keystroke, useKeystroke } from "../input/index.js";

export interface InkKey {
  readonly upArrow: boolean;
  readonly downArrow: boolean;
  readonly leftArrow: boolean;
  readonly rightArrow: boolean;
  readonly pageDown: boolean;
  readonly pageUp: boolean;
  readonly return: boolean;
  readonly escape: boolean;
  readonly ctrl: boolean;
  readonly shift: boolean;
  readonly tab: boolean;
  readonly backspace: boolean;
  readonly delete: boolean;
  readonly meta: boolean;
}

export type InkInputHandler = (input: string, key: InkKey) => void;

export interface UseInputOptions {
  readonly isActive?: boolean;
}

export function useInput(handler: InkInputHandler, options: UseInputOptions = {}): void {
  const active = options.isActive !== false;
  useKeystroke((k: Keystroke) => {
    if (!active) return;
    handler(k.input ?? "", toInkKey(k));
  });
}

function toInkKey(k: Keystroke): InkKey {
  return {
    upArrow: k.upArrow,
    downArrow: k.downArrow,
    leftArrow: k.leftArrow,
    rightArrow: k.rightArrow,
    pageDown: k.pageDown,
    pageUp: k.pageUp,
    return: k.return,
    escape: k.escape,
    ctrl: k.ctrl,
    shift: k.shift,
    tab: k.tab,
    backspace: k.backspace,
    delete: k.delete,
    meta: k.meta,
  };
}
