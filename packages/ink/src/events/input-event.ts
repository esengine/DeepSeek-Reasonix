import { nonAlphanumericKeys, type ParsedKey } from '../parse-keypress.js'
import { Event } from './event.js'

export type Key = {
  upArrow: boolean
  downArrow: boolean
  leftArrow: boolean
  rightArrow: boolean
  pageDown: boolean
  pageUp: boolean
  wheelUp: boolean
  wheelDown: boolean
  home: boolean
  end: boolean
  return: boolean
  escape: boolean
  ctrl: boolean
  shift: boolean
  fn: boolean
  tab: boolean
  backspace: boolean
  delete: boolean
  meta: boolean
  super: boolean,
}

function parseKey(keypress: ParsedKey): [Key, string] {
  const key: Key = {
    upArrow: keypress.name === 'up',
    downArrow: keypress.name === 'down',
    leftArrow: keypress.name === 'left',
    rightArrow: keypress.name === 'right',
    pageDown: keypress.name === 'pagedown',
    pageUp: keypress.name === 'pageup',
    wheelUp: keypress.name === 'wheelup',
    wheelDown: keypress.name === 'wheeldown',
    home: keypress.name === 'home',
    end: keypress.name === 'end',
    return: keypress.name === 'return',
    escape: keypress.name === 'escape',
    fn: keypress.fn,
    ctrl: keypress.ctrl,
    shift: keypress.shift,
    tab: keypress.name === 'tab',
    backspace: keypress.name === 'backspace',
    delete: keypress.name === 'delete',
    // parseKeypress reports `\u001B\u001B[A` (meta + arrow up) with meta=false
    // and option=true. We collapse the two into the same `meta` bit so user
    // handlers don't have to branch on which terminal sent the sequence.
    // `escape` is folded in for the same reason: a lone ESC arrives with no
    // meta bit set but should behave like a modifier-bearing keypress here.
    meta: keypress.meta || keypress.name === 'escape' || keypress.option,
    // Super (Cmd on macOS, Win key on Windows) only arrives via kitty
    // keyboard protocol CSI u sequences. Distinct from `meta` (Alt/Option)
    // so bindings like cmd+c can be expressed separately from opt+c.
    super: keypress.super,
  }

  let input = keypress.ctrl ? keypress.name : keypress.sequence

  if (input === undefined) {
    input = ''
  }

  // When ctrl is held, keypress.name for the space bar is the literal
  // word 'space'. Convert to an actual space character so the ctrl+space
  // branch agrees with the CSI u branch below (which already maps
  // 'space' to ' '). Without this the word "space" leaks into text input.
  if (keypress.ctrl && input === 'space') {
    input = ' '
  }

  // Unrecognised escape sequences that matched the function-key regex but
  // have no entry in the key-name map (e.g. ESC[25~ = F13 / Right Alt on
  // Windows, ESC[26~ = F14). The ESC prefix is stripped further down, so
  // without this guard the remainder ("[25~") surfaces as literal text.
  if (keypress.code && !keypress.name) {
    input = ''
  }

  // Defensive sink for SGR mouse fragments arriving without their leading
  // ESC. When a heavy React commit blocks the event loop past App's
  // normal-flush timeout, a CSI split across stdin chunks gets its
  // buffered ESC flushed as a lone Escape key, and the continuation
  // arrives as a text token with name=''. It falls through every
  // ESC-anchored regex in parseKeypress and the nonAlphanumericKeys
  // clear below (because `name` is falsy), leaving the fragment to
  // surface in the prompt as literal text like `[<64;74;16M`. The real
  // fix lives in the tokenizer; this guard keeps the symptom from being
  // user-visible.
  if (!keypress.name && /^\[<\d+;\d+;\d+[Mm]/.test(input)) {
    input = ''
  }

  // Strip a stray leading ESC that parseKeypress didn't consume (legacy
  // F-keys, partial CSI fragments from a chunked read). The `meta` bit
  // above already carries the modifier semantics, so dropping the ESC
  // prevents the remainder from surfacing as literal text in handlers.
  if (input.startsWith('\u001B')) {
    input = input.slice(1)
  }

  // CSI u and application-keypad branches below rewrite `input` to the
  // resolved character. The nonAlphanumericKeys clear at the end of the
  // function would then wipe that work for arrow / function-key names,
  // so the branches that succeed flip this flag to skip the final clear.
  let processedAsSpecialSequence = false

  // CSI u sequences (kitty keyboard protocol). After the leading ESC is
  // stripped above, we're left with "[<codepoint>;<modifier>u" — for
  // example "[98;3u" for Alt+b. The parser already resolved the keyname,
  // so we substitute that here. The leading-digit check matters: a bare
  // startsWith('[') false-matches X10 mouse reports at row 85 (Cy = 117 =
  // 'u'), which would otherwise leak the literal text "mouse" into the
  // prompt via processedAsSpecialSequence.
  if (/^\[\d/.test(input) && input.endsWith('u')) {
    if (!keypress.name) {
      // Unmapped kitty functional key (Caps Lock 57358, F13-F35, KP nav,
      // bare modifiers, ...): keycodeToName() returned undefined. Swallow
      // so the raw "[57358u" doesn't leak into the prompt.
      input = ''
    } else {
      // 'space' -> ' '; 'escape' -> '' (key.escape already carries it,
      // and the processedAsSpecialSequence flag bypasses the
      // nonAlphanumericKeys clear below so we must zero it explicitly);
      // anything else uses the resolved key name.
      input =
        keypress.name === 'space'
          ? ' '
          : keypress.name === 'escape'
            ? ''
            : keypress.name
    }
    processedAsSpecialSequence = true
  }

  // xterm modifyOtherKeys sequences. After the leading ESC is stripped,
  // we see "[27;<modifier>;<keycode>~" — for example "[27;3;98~" for
  // Alt+b. Same shape as the CSI u path: substitute the resolved name so
  // printable-char keycodes (single-letter names) don't skip the
  // nonAlphanumericKeys clear and leak "[27;..." as input.
  if (input.startsWith('[27;') && input.endsWith('~')) {
    if (!keypress.name) {
      // Unmapped modifyOtherKeys keycode. Practically untriggerable today
      // (xterm modifyOtherKeys only emits ASCII keycodes, all mapped), but
      // we mirror the CSI u behaviour as a forward-compatibility guard.
      input = ''
    } else {
      input =
        keypress.name === 'space'
          ? ' '
          : keypress.name === 'escape'
            ? ''
            : keypress.name
    }
    processedAsSpecialSequence = true
  }

  // Application-keypad mode. After the leading ESC is stripped we see
  // "O<letter>" — e.g. "Op" for numpad 0, "Oy" for numpad 9. The parser
  // resolves the digit character into `name`; use it for input.
  if (
    input.startsWith('O') &&
    input.length === 2 &&
    keypress.name &&
    keypress.name.length === 1
  ) {
    input = keypress.name
    processedAsSpecialSequence = true
  }

  // Final clear for symbolic keys (arrows, function keys, etc.). The
  // CSI u / modifyOtherKeys / keypad branches above already replaced
  // `input` with the right value, so we skip the clear when they fired.
  if (
    !processedAsSpecialSequence &&
    keypress.name &&
    nonAlphanumericKeys.has(keypress.name)
  ) {
    input = ''
  }

  // Promote uppercase ASCII letters to shift=true. The length check
  // prevents non-letter characters that happen to be unchanged by
  // toUpperCase (digits, punctuation) from triggering the modifier flag.
  if (
    input.length === 1 &&
    typeof input[0] === 'string' &&
    input[0] >= 'A' &&
    input[0] <= 'Z'
  ) {
    key.shift = true
  }

  return [key, input]
}

/** Raw keypress event delivered by the emitter (not the DOM dispatcher). */
export class InputEvent extends Event {
  readonly keypress: ParsedKey
  readonly key: Key
  readonly input: string

  constructor(keypress: ParsedKey) {
    super()
    const [key, input] = parseKey(keypress)

    this.keypress = keypress
    this.key = key
    this.input = input
  }
}
