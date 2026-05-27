import { coerce } from 'semver';
import type { Writable } from 'stream';
import { env } from './_internal/env.js';
import { gte } from './_internal/semver.js';
import { getClearTerminalSequence } from './clearTerminal.js';
import type { Diff } from './frame.js';
import { cursorMove, cursorTo, eraseLines } from './termio/csi.js';
import { BSU, ESU, HIDE_CURSOR, SHOW_CURSOR } from './termio/dec.js';
import { link } from './termio/osc.js';

export type Progress = {
  state: 'running' | 'completed' | 'error' | 'indeterminate';
  percentage?: number;
};

/** Whether the host terminal interprets OSC 9;4 as a progress indicator. */
export function isProgressReportingAvailable(): boolean {
  if (!process.stdout.isTTY) {
    return false;
  }

  // Windows Terminal hijacks OSC 9;4 for notifications — sending progress
  // there is actively user-hostile.
  if (process.env.WT_SESSION) {
    return false;
  }

  if (
    process.env.ConEmuANSI ||
    process.env.ConEmuPID ||
    process.env.ConEmuTask
  ) {
    return true;
  }

  const version = coerce(process.env.TERM_PROGRAM_VERSION);
  if (!version) {
    return false;
  }

  if (process.env.TERM_PROGRAM === 'ghostty') {
    return gte(version.version, '1.2.0');
  }

  if (process.env.TERM_PROGRAM === 'iTerm.app') {
    return gte(version.version, '3.6.6');
  }

  return false;
}

/** Whether DEC mode 2026 (synchronized output) is honoured end-to-end. */
export function isSynchronizedOutputSupported(): boolean {
  // tmux parses every byte but doesn't preserve atomicity — the BSU/ESU
  // pair passes through to the outer terminal, but tmux has already broken
  // up the frame on its own buffer boundaries. Skip the 16 bytes/frame +
  // parser work since they buy nothing.
  if (process.env.TMUX) return false;

  const termProgram = process.env.TERM_PROGRAM;
  const term = process.env.TERM;

  if (
    termProgram === 'iTerm.app' ||
    termProgram === 'WezTerm' ||
    termProgram === 'WarpTerminal' ||
    termProgram === 'ghostty' ||
    termProgram === 'contour' ||
    termProgram === 'vscode' ||
    termProgram === 'alacritty'
  ) {
    return true;
  }

  // kitty advertises itself through TERM or its session env var.
  if (term?.includes('kitty') || process.env.KITTY_WINDOW_ID) return true;

  // Ghostty sometimes sets TERM without TERM_PROGRAM (e.g. over SSH).
  if (term === 'xterm-ghostty') return true;

  // foot ships under TERM=foot or foot-extra.
  if (term?.startsWith('foot')) return true;

  if (term?.includes('alacritty')) return true;

  // Zed embeds alacritty_terminal, which implements DEC 2026.
  if (process.env.ZED_TERM) return true;

  if (process.env.WT_SESSION) return true;

  // VTE-based terminals (GNOME Terminal, Tilix, Terminator, ...) gained
  // DEC 2026 support in VTE 0.68. VTE_VERSION is exposed as YYMMDD-style
  // integer with 6800 == 0.68.00.
  const vteVersion = process.env.VTE_VERSION;
  if (vteVersion) {
    const version = parseInt(vteVersion, 10);
    if (version >= 6800) return true;
  }

  return false;
}

// --- XTVERSION-based terminal identification ------------------------------
//
// TERM_PROGRAM doesn't survive SSH, so env-based detection misses the case
// where we're running remotely inside (say) a VS Code integrated terminal.
// XTVERSION (CSI > 0 q -> DCS > | <name> ST) is asked through the pty, so
// the reply identifies the *client* terminal regardless of how the
// environment was forwarded. The reply is delivered asynchronously through
// stdin and parsed by the input pipeline, which feeds the name in here.

let xtversionName: string | undefined;

/** Record the name reported by an XTVERSION reply. */
export function setXtversionName(name: string): void {
  if (xtversionName === undefined) xtversionName = name;
}

/** Whether the terminal in front of the user is an xterm.js embedder. */
export function isXtermJs(): boolean {
  if (process.env.TERM_PROGRAM === 'vscode') return true;
  return xtversionName?.startsWith('xterm.js') ?? false;
}

// Terminals known to implement the Kitty keyboard protocol (CSI >1u) or
// xterm modifyOtherKeys (CSI >4;2m) correctly. The naive approach is to
// enable extended key reporting unconditionally and rely on terminals
// ignoring unknown CSI sequences — that breaks when a terminal *accepts*
// the enable but emits codepoints we can't decode, which we've observed
// over SSH and inside xterm.js. tmux is on the list because it accepts
// modifyOtherKeys at its layer without forwarding the kitty sequence to
// the outer terminal.
//
// Stored as a Set so the per-call membership test is O(1); the previous
// Array.includes() walk was negligible for one call per process, but the
// shape change costs nothing and is the more honest spelling of "set
// membership".
const EXTENDED_KEYS_TERMINALS = new Set<string>([
  'iTerm.app',
  'kitty',
  'WezTerm',
  'ghostty',
  'tmux',
  'windows-terminal',
]);

/** Whether to opt in to extended-key reporting (Kitty + modifyOtherKeys). */
export function supportsExtendedKeys(): boolean {
  return EXTENDED_KEYS_TERMINALS.has(env.terminal ?? '');
}

export function hasCursorUpViewportYankBug(): boolean {
  return process.platform === 'win32' || !!process.env.WT_SESSION;
}

// Snapshot once at module load — terminal capabilities don't change
// mid-session, and the synchronized-output decision is consulted on every
// frame. Re-exported so log-update can take the fast path when sync output
// is unavailable.
export const SYNC_OUTPUT_SUPPORTED = isSynchronizedOutputSupported();

export type Terminal = {
  stdout: Writable;
  stderr: Writable;
};

/** Serialise a Diff into bytes and ship it to the terminal. */
export function writeDiffToTerminal(
  terminal: Terminal,
  diff: Diff,
  skipSyncMarkers = false,
): void {
  if (diff.length === 0) {
    return;
  }

  const useSync = !skipSyncMarkers;

  let buffer = useSync ? BSU : '';

  for (const patch of diff) {
    switch (patch.type) {
      case 'stdout':
        buffer += patch.content;
        break;
      case 'clear':
        if (patch.count > 0) {
          buffer += eraseLines(patch.count);
        }
        break;
      case 'clearTerminal':
        buffer += getClearTerminalSequence();
        break;
      case 'cursorHide':
        buffer += HIDE_CURSOR;
        break;
      case 'cursorShow':
        buffer += SHOW_CURSOR;
        break;
      case 'cursorMove':
        buffer += cursorMove(patch.x, patch.y);
        break;
      case 'cursorTo':
        buffer += cursorTo(patch.col);
        break;
      case 'carriageReturn':
        buffer += '\r';
        break;
      case 'hyperlink':
        buffer += link(patch.uri);
        break;
      case 'styleStr':
        buffer += patch.str;
        break;
    }
  }

  if (useSync) buffer += ESU;
  terminal.stdout.write(buffer);
}
