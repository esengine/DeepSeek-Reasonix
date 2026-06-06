
import { C0, ESC_TYPE, isEscFinal } from './ansi.js'
import { isCSIFinal, isCSIIntermediate, isCSIParam } from './csi.js'

export type Token =
  | { type: 'text'; value: string }
  | { type: 'sequence'; value: string }

type State =
  | 'ground'
  | 'escape'
  | 'escapeIntermediate'
  | 'csi'
  | 'ss3'
  | 'osc'
  | 'dcs'
  | 'apc'

export type Tokenizer = {
  /** Feed input and return the tokens it completes. */
  feed(input: string): Token[]
  /** Force any buffered incomplete sequence out as a sequence token. */
  flush(): Token[]
  /** Drop all state — used between unrelated input chunks. */
  reset(): void
  /** Inspect the current incomplete sequence buffer. */
  buffer(): string
}

type TokenizerOptions = {
  x10Mouse?: boolean
}

/** Create a streaming tokenizer for terminal input. */
export function createTokenizer(options?: TokenizerOptions): Tokenizer {
  let currentState: State = 'ground'
  let currentBuffer = ''
  const x10Mouse = options?.x10Mouse ?? false

  return {
    feed(input: string): Token[] {
      const result = tokenize(
        input,
        currentState,
        currentBuffer,
        false,
        x10Mouse,
      )
      currentState = result.state.state
      currentBuffer = result.state.buffer
      return result.tokens
    },

    flush(): Token[] {
      const result = tokenize('', currentState, currentBuffer, true, x10Mouse)
      currentState = result.state.state
      currentBuffer = result.state.buffer
      return result.tokens
    },

    reset(): void {
      currentState = 'ground'
      currentBuffer = ''
    },

    buffer(): string {
      return currentBuffer
    },
  }
}

type InternalState = {
  state: State
  buffer: string
}

/** Single-pass tokenizer driver. */
function tokenize(
  input: string,
  initialState: State,
  initialBuffer: string,
  flush: boolean,
  x10Mouse: boolean,
): { tokens: Token[]; state: InternalState } {
  const tokens: Token[] = []
  const result: InternalState = {
    state: initialState,
    buffer: '',
  }

  const data = initialBuffer + input
  let i = 0
  let textStart = 0
  let seqStart = 0

  const flushText = (): void => {
    if (i > textStart) {
      const text = data.slice(textStart, i)
      if (text) {
        tokens.push({ type: 'text', value: text })
      }
    }
    textStart = i
  }

  const emitSequence = (seq: string): void => {
    if (seq) {
      tokens.push({ type: 'sequence', value: seq })
    }
    result.state = 'ground'
    textStart = i
  }

  while (i < data.length) {
    const code = data.charCodeAt(i)

    switch (result.state) {
      case 'ground':
        if (code === C0.ESC) {
          flushText()
          seqStart = i
          result.state = 'escape'
          i++
        } else {
          i++
        }
        break

      case 'escape':
        if (code === ESC_TYPE.CSI) {
          result.state = 'csi'
          i++
        } else if (code === ESC_TYPE.OSC) {
          result.state = 'osc'
          i++
        } else if (code === ESC_TYPE.DCS) {
          result.state = 'dcs'
          i++
        } else if (code === ESC_TYPE.APC) {
          result.state = 'apc'
          i++
        } else if (code === 0x4f) {
          // 'O' — SS3 (single shift to G3, used by some keypad keys).
          result.state = 'ss3'
          i++
        } else if (isCSIIntermediate(code)) {
          // ESC followed by an intermediate byte (e.g. `ESC (` for
          // charset designation). Stay in the buffer until we see a
          // final byte.
          result.state = 'escapeIntermediate'
          i++
        } else if (isEscFinal(code)) {
          // A two-byte ESC sequence (ESC c, ESC 7, ESC =, …).
          i++
          emitSequence(data.slice(seqStart, i))
        } else if (code === C0.ESC) {
          // Two ESCs in a row: emit the first as its own sequence and
          // restart. Real terminals send this when a user presses Alt
          // and the keymap layer hadn't started a proper modifier seq.
          emitSequence(data.slice(seqStart, i))
          seqStart = i
          result.state = 'escape'
          i++
        } else {
          // Unparseable — drop the ESC marker and reinterpret the byte
          // as text starting from where the ESC was.
          result.state = 'ground'
          textStart = seqStart
        }
        break

      case 'escapeIntermediate':
        if (isCSIIntermediate(code)) {
          i++
        } else if (isEscFinal(code)) {
          i++
          emitSequence(data.slice(seqStart, i))
        } else {
          result.state = 'ground'
          textStart = seqStart
        }
        break

      case 'csi':
        // X10 mouse event: `CSI M` followed by three raw payload bytes
        // (Cb+32, Cx+32, Cy+32). This branch is gated on x10Mouse
        // because for output parsing `CSI M` is the Delete Lines (DL)
        // command, and consuming the three following bytes there would
        // corrupt the output stream.
        //
        // The `i - seqStart === 2` check enforces that M is the very
        // first byte after `[`. SGR mouse (`CSI < … M`) has a `<` param
        // byte first and reaches M at offset > 2 — that path falls
        // through to the normal isCSIFinal branch.
        //
        // The ≥0x20 checks on each payload slot are belt-and-suspenders:
        // X10 guarantees Cb ≥ 32 (and the coords ≥ 33), so a control
        // byte (ESC = 0x1B) in any slot means we're actually looking at
        // CSI DL adjacent to another sequence, not a mouse event. We
        // need to check all three slots, not just the first — when a
        // paste happens to end in `\x1b[M` + 0–2 chars, PASTE_END's ESC
        // sits in one of those slots and we mustn't swallow it.
        //
        // Known limitation: this counts JavaScript string code units,
        // but X10 is byte-oriented and stdin uses UTF-8 encoding. At
        // column 162-191 × row 96-159 the two coordinate bytes
        // (0xC2-0xDF, 0x80-0xBF) form a valid UTF-8 two-byte sequence
        // and collapse to a single code unit — the 4-character look-
        // ahead fails and the event sits in the buffer until the next
        // keypress completes it. Fixing this requires switching stdin
        // to latin1, which has wider downsides; X10's 223-column cap is
        // the reason SGR mouse (mode 1006) exists, and terminals that
        // don't support SGR at 162+ columns are essentially extinct.
        if (
          x10Mouse &&
          code === 0x4d /* M */ &&
          i - seqStart === 2 &&
          (i + 1 >= data.length || data.charCodeAt(i + 1) >= 0x20) &&
          (i + 2 >= data.length || data.charCodeAt(i + 2) >= 0x20) &&
          (i + 3 >= data.length || data.charCodeAt(i + 3) >= 0x20)
        ) {
          if (i + 4 <= data.length) {
            i += 4
            emitSequence(data.slice(seqStart, i))
          } else {
            // Incomplete payload — exit the loop and let the
            // end-of-input handling buffer from seqStart. On re-entry
            // we'll re-tokenize from ground via the invalid-CSI path.
            i = data.length
          }
          break
        }
        if (isCSIFinal(code)) {
          i++
          emitSequence(data.slice(seqStart, i))
        } else if (isCSIParam(code) || isCSIIntermediate(code)) {
          i++
        } else {
          // Malformed CSI — drop the introducer and replay as text.
          result.state = 'ground'
          textStart = seqStart
        }
        break

      case 'ss3':
        // SS3 = ESC O + one final byte (0x40-0x7E).
        if (code >= 0x40 && code <= 0x7e) {
          i++
          emitSequence(data.slice(seqStart, i))
        } else {
          result.state = 'ground'
          textStart = seqStart
        }
        break

      case 'osc':
        // OSC terminates on BEL or on `ESC \` (ST). We accept both
        // because terminals are inconsistent about which they emit.
        if (code === C0.BEL) {
          i++
          emitSequence(data.slice(seqStart, i))
        } else if (
          code === C0.ESC &&
          i + 1 < data.length &&
          data.charCodeAt(i + 1) === ESC_TYPE.ST
        ) {
          i += 2
          emitSequence(data.slice(seqStart, i))
        } else {
          i++
        }
        break

      case 'dcs':
      case 'apc':
        // DCS and APC use the same terminator rules as OSC. We don't
        // interpret their payloads here — they're typically tmux
        // passthrough or kitty graphics, handed off as raw sequences.
        if (code === C0.BEL) {
          i++
          emitSequence(data.slice(seqStart, i))
        } else if (
          code === C0.ESC &&
          i + 1 < data.length &&
          data.charCodeAt(i + 1) === ESC_TYPE.ST
        ) {
          i += 2
          emitSequence(data.slice(seqStart, i))
        } else {
          i++
        }
        break
    }
  }

  if (result.state === 'ground') {
    flushText()
  } else if (flush) {
    // Caller asked for an explicit flush — best-effort emit whatever's
    // accumulated as a sequence token, even if the bytes don't form a
    // complete sequence.
    const remaining = data.slice(seqStart)
    if (remaining) tokens.push({ type: 'sequence', value: remaining })
    result.state = 'ground'
  } else {
    // Hold the partial sequence for the next feed call.
    result.buffer = data.slice(seqStart)
  }

  return { tokens, state: result }
}
