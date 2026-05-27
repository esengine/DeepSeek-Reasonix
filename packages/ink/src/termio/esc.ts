/** Parser for short ESC sequences — i.e. */

import type { Action } from './types.js'

/** Interpret the body of an ESC sequence. */
export function parseEsc(chars: string): Action | null {
  if (chars.length === 0) return null

  const first = chars[0]!

  // RIS — full terminal reset (ESC c). The renderer treats this as a hint
  // to drop its cached state, since the terminal will have done the same.
  if (first === 'c') {
    return { type: 'reset' }
  }

  // DECSC — save cursor (ESC 7). The newer SCOSC (CSI s) does the same job
  // but legacy apps and ssh wrappers still emit ESC 7.
  if (first === '7') {
    return { type: 'cursor', action: { type: 'save' } }
  }

  // DECRC — restore cursor (ESC 8). Pairs with ESC 7.
  if (first === '8') {
    return { type: 'cursor', action: { type: 'restore' } }
  }

  // IND — index, move cursor down one line (ESC D). Scrolls if at bottom.
  if (first === 'D') {
    return {
      type: 'cursor',
      action: { type: 'move', direction: 'down', count: 1 },
    }
  }

  // RI — reverse index, move cursor up one line (ESC M). Scrolls if at top.
  if (first === 'M') {
    return {
      type: 'cursor',
      action: { type: 'move', direction: 'up', count: 1 },
    }
  }

  // NEL — next line (ESC E). Equivalent to CR+LF.
  if (first === 'E') {
    return { type: 'cursor', action: { type: 'nextLine', count: 1 } }
  }

  // HTS — set horizontal tab stop at current column (ESC H). We don't
  // model tab stops, so swallow it rather than reporting it as unknown.
  if (first === 'H') {
    return null
  }

  // Charset designators (ESC ( X, ESC ) X, etc.) — these select G0/G1
  // character sets. Modern apps don't drive these and we render UTF-8, so
  // silently drop the two-byte sequence.
  if ('()'.includes(first) && chars.length >= 2) {
    return null
  }

  return { type: 'unknown', sequence: `\x1b${chars}` }
}
