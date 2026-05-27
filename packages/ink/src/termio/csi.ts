
import { ESC, ESC_TYPE, SEP } from './ansi.js'

export const CSI_PREFIX = ESC + String.fromCharCode(ESC_TYPE.CSI)

export const CSI_RANGE = {
  PARAM_START: 0x30,
  PARAM_END: 0x3f,
  INTERMEDIATE_START: 0x20,
  INTERMEDIATE_END: 0x2f,
  FINAL_START: 0x40,
  FINAL_END: 0x7e,
} as const

/** True for CSI parameter bytes (`0–9 : ; < = > ?`). */
export function isCSIParam(byte: number): boolean {
  return byte >= CSI_RANGE.PARAM_START && byte <= CSI_RANGE.PARAM_END
}

/** True for CSI intermediate bytes (`SP ! " # $ % & ' ( ) * + , - . /`). */
export function isCSIIntermediate(byte: number): boolean {
  return (
    byte >= CSI_RANGE.INTERMEDIATE_START && byte <= CSI_RANGE.INTERMEDIATE_END
  )
}

/** True for the single CSI final byte (`@` through `~`). */
export function isCSIFinal(byte: number): boolean {
  return byte >= CSI_RANGE.FINAL_START && byte <= CSI_RANGE.FINAL_END
}

/** Build a CSI sequence: `ESC [ p1;p2;…;pN final`. */
export function csi(...args: (string | number)[]): string {
  if (args.length === 0) return CSI_PREFIX
  if (args.length === 1) return `${CSI_PREFIX}${args[0]}`
  const params = args.slice(0, -1)
  const final = args[args.length - 1]
  return `${CSI_PREFIX}${params.join(SEP)}${final}`
}

/** CSI final bytes — the command identifier portion of every CSI sequence. */
export const CSI = {
  // Cursor movement
  CUU: 0x41, // A — Cursor Up
  CUD: 0x42, // B — Cursor Down
  CUF: 0x43, // C — Cursor Forward
  CUB: 0x44, // D — Cursor Back
  CNL: 0x45, // E — Cursor Next Line
  CPL: 0x46, // F — Cursor Previous Line
  CHA: 0x47, // G — Cursor Horizontal Absolute
  CUP: 0x48, // H — Cursor Position
  CHT: 0x49, // I — Cursor Horizontal Tab
  VPA: 0x64, // d — Vertical Position Absolute
  HVP: 0x66, // f — Horizontal Vertical Position

  // Erase
  ED: 0x4a, // J — Erase in Display
  EL: 0x4b, // K — Erase in Line
  ECH: 0x58, // X — Erase Character

  // Insert / Delete
  IL: 0x4c, // L — Insert Lines
  DL: 0x4d, // M — Delete Lines
  ICH: 0x40, // @ — Insert Characters
  DCH: 0x50, // P — Delete Characters

  // Scroll
  SU: 0x53, // S — Scroll Up
  SD: 0x54, // T — Scroll Down

  // Modes
  SM: 0x68, // h — Set Mode
  RM: 0x6c, // l — Reset Mode

  // SGR
  SGR: 0x6d, // m — Select Graphic Rendition

  // Other
  DSR: 0x6e, // n — Device Status Report
  DECSCUSR: 0x71, // q — Set Cursor Style (needs space intermediate)
  DECSTBM: 0x72, // r — Set Top and Bottom Margins
  SCOSC: 0x73, // s — Save Cursor Position
  SCORC: 0x75, // u — Restore Cursor Position
  CBT: 0x5a, // Z — Cursor Backward Tabulation
} as const

/** Erase-in-Display regions, indexed by the ED parameter (0/1/2/3). */
export const ERASE_DISPLAY = ['toEnd', 'toStart', 'all', 'scrollback'] as const

/** Erase-in-Line regions, indexed by the EL parameter (0/1/2). */
export const ERASE_LINE_REGION = ['toEnd', 'toStart', 'all'] as const

/** Cursor shape kinds reported via DECSCUSR. */
export type CursorStyle = 'block' | 'underline' | 'bar'

/** DECSCUSR parameter → (shape, blinking) mapping. */
export const CURSOR_STYLES: Array<{ style: CursorStyle; blinking: boolean }> = [
  { style: 'block', blinking: true }, // 0 — default
  { style: 'block', blinking: true }, // 1
  { style: 'block', blinking: false }, // 2
  { style: 'underline', blinking: true }, // 3
  { style: 'underline', blinking: false }, // 4
  { style: 'bar', blinking: true }, // 5
  { style: 'bar', blinking: false }, // 6
]

// ---------------------------------------------------------------------------
// Cursor movement generators
// ---------------------------------------------------------------------------

// All `cursor*` helpers short-circuit on n === 0 because emitting `CSI 0 A`
// is not the same as a no-op — some terminals interpret 0 as 1, so the
// caller would see a spurious one-cell move.

/** Move cursor up n lines (CSI n A). */
export function cursorUp(n = 1): string {
  return n === 0 ? '' : csi(n, 'A')
}

/** Move cursor down n lines (CSI n B). */
export function cursorDown(n = 1): string {
  return n === 0 ? '' : csi(n, 'B')
}

/** Move cursor forward n columns (CSI n C). */
export function cursorForward(n = 1): string {
  return n === 0 ? '' : csi(n, 'C')
}

/** Move cursor back n columns (CSI n D). */
export function cursorBack(n = 1): string {
  return n === 0 ? '' : csi(n, 'D')
}

/** Move cursor to column n (1-indexed) (CSI n G). */
export function cursorTo(col: number): string {
  return csi(col, 'G')
}

/** Move cursor to column 1 (CSI G). */
export const CURSOR_LEFT = csi('G')

/** Move cursor to (row, col), both 1-indexed (CSI row ; col H). */
export function cursorPosition(row: number, col: number): string {
  return csi(row, col, 'H')
}

/** Move cursor to home (1, 1) (CSI H). */
export const CURSOR_HOME = csi('H')

/** Relative cursor move. */
export function cursorMove(x: number, y: number): string {
  let result = ''
  if (x < 0) {
    result += cursorBack(-x)
  } else if (x > 0) {
    result += cursorForward(x)
  }
  if (y < 0) {
    result += cursorUp(-y)
  } else if (y > 0) {
    result += cursorDown(y)
  }
  return result
}

// ---------------------------------------------------------------------------
// Save / restore cursor
// ---------------------------------------------------------------------------

/** Save cursor position (CSI s — SCOSC). */
export const CURSOR_SAVE = csi('s')

/** Restore cursor position (CSI u — SCORC). */
export const CURSOR_RESTORE = csi('u')

// ---------------------------------------------------------------------------
// Erase generators
// ---------------------------------------------------------------------------

/** Erase from cursor to end of line (CSI K). */
export function eraseToEndOfLine(): string {
  return csi('K')
}

/** Erase from cursor to start of line (CSI 1 K). */
export function eraseToStartOfLine(): string {
  return csi(1, 'K')
}

/** Erase entire line (CSI 2 K). */
export function eraseLine(): string {
  return csi(2, 'K')
}

/** Pre-built constant form of {@link eraseLine}. */
export const ERASE_LINE = csi(2, 'K')

/** Erase from cursor to end of screen (CSI J). */
export function eraseToEndOfScreen(): string {
  return csi('J')
}

/** Erase from cursor to start of screen (CSI 1 J). */
export function eraseToStartOfScreen(): string {
  return csi(1, 'J')
}

/** Erase entire screen (CSI 2 J). */
export function eraseScreen(): string {
  return csi(2, 'J')
}

/** Pre-built constant form of {@link eraseScreen}. */
export const ERASE_SCREEN = csi(2, 'J')

/** Erase scrollback buffer (CSI 3 J — xterm extension). */
export const ERASE_SCROLLBACK = csi(3, 'J')

export function eraseLines(n: number): string {
  if (n <= 0) return ''
  let result = ''
  for (let i = 0; i < n; i++) {
    result += ERASE_LINE
    if (i < n - 1) {
      result += cursorUp(1)
    }
  }
  result += CURSOR_LEFT
  return result
}

// ---------------------------------------------------------------------------
// Scroll
// ---------------------------------------------------------------------------

/** Scroll up n lines (CSI n S). */
export function scrollUp(n = 1): string {
  return n === 0 ? '' : csi(n, 'S')
}

/** Scroll down n lines (CSI n T). */
export function scrollDown(n = 1): string {
  return n === 0 ? '' : csi(n, 'T')
}

/** Set scrolling region (DECSTBM, CSI top;bottom r) — 1-indexed, inclusive. */
export function setScrollRegion(top: number, bottom: number): string {
  return csi(top, bottom, 'r')
}

/** Reset scrolling region to full screen (DECSTBM, CSI r). Also homes the cursor. */
export const RESET_SCROLL_REGION = csi('r')

// ---------------------------------------------------------------------------
// Input markers — sequences the terminal sends TO us, not the other way around
// ---------------------------------------------------------------------------

// Bracketed paste delimiters: when DEC mode 2004 is enabled the terminal
// wraps every pasted blob in PASTE_START / PASTE_END so we can distinguish
// keystrokes from clipboard content.
/** Terminal emits before pasted content (CSI 200 ~). */
export const PASTE_START = csi('200~')

/** Terminal emits after pasted content (CSI 201 ~). */
export const PASTE_END = csi('201~')

// Focus events: when DEC mode 1004 is enabled the terminal pings us on
// focus-in / focus-out so the app can pause animation, clear hover state,
// etc.
/** Terminal emits when it gains focus (CSI I). */
export const FOCUS_IN = csi('I')

/** Terminal emits when it loses focus (CSI O). */
export const FOCUS_OUT = csi('O')

// ---------------------------------------------------------------------------
// Enhanced keyboard reporting
// ---------------------------------------------------------------------------

export const ENABLE_KITTY_KEYBOARD = csi('>1u')

/** Pop the most recent Kitty keyboard mode off the terminal's stack. */
export const DISABLE_KITTY_KEYBOARD = csi('<u')

/** Enable xterm `modifyOtherKeys` level 2. */
export const ENABLE_MODIFY_OTHER_KEYS = csi('>4;2m')

/** Reset xterm modifyOtherKeys to default. */
export const DISABLE_MODIFY_OTHER_KEYS = csi('>4m')
