import {
  CURSOR_HOME,
  csi,
  ERASE_SCREEN,
  ERASE_SCROLLBACK,
} from './termio/csi.js';


const CURSOR_HOME_WINDOWS = csi(0, 'f');

function isWindowsTerminal(): boolean {
  return process.platform === 'win32' && !!process.env.WT_SESSION;
}

function isMintty(): boolean {
  if (process.env.TERM_PROGRAM === 'mintty') return true;
  // GitBash, MSYS2, MINGW all use mintty under the hood and set $MSYSTEM.
  if (process.platform === 'win32' && process.env.MSYSTEM) return true;
  return false;
}

function isModernWindowsTerminal(): boolean {
  if (isWindowsTerminal()) return true;

  // VS Code's integrated terminal on Windows runs over ConPTY, which
  // forwards ED3 correctly. The TERM_PROGRAM_VERSION check rules out
  // ancient builds where ConPTY didn't ship.
  if (
    process.platform === 'win32' &&
    process.env.TERM_PROGRAM === 'vscode' &&
    process.env.TERM_PROGRAM_VERSION
  ) {
    return true;
  }

  if (isMintty()) return true;

  return false;
}

export function getClearTerminalSequence(): string {
  if (process.platform === 'win32') {
    if (isModernWindowsTerminal()) {
      return ERASE_SCREEN + ERASE_SCROLLBACK + CURSOR_HOME;
    }
    // Legacy Win32 console: no ED3, no DEC private modes; fall back to
    // an HVP cursor-home so output doesn't leak escape literals.
    return ERASE_SCREEN + CURSOR_HOME_WINDOWS;
  }
  return ERASE_SCREEN + ERASE_SCROLLBACK + CURSOR_HOME;
}

export const clearTerminal = getClearTerminalSequence();
