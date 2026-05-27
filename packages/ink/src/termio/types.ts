/** Semantic types for the ANSI parser. */

// ---------------------------------------------------------------------------
// Colours
// ---------------------------------------------------------------------------

/** Named colours from the 16-colour ANSI palette. */
export type NamedColor =
  | 'black'
  | 'red'
  | 'green'
  | 'yellow'
  | 'blue'
  | 'magenta'
  | 'cyan'
  | 'white'
  | 'brightBlack'
  | 'brightRed'
  | 'brightGreen'
  | 'brightYellow'
  | 'brightBlue'
  | 'brightMagenta'
  | 'brightCyan'
  | 'brightWhite'

/** Colour specification. */
export type Color =
  | { type: 'named'; name: NamedColor }
  | { type: 'indexed'; index: number } // 0–255 palette
  | { type: 'rgb'; r: number; g: number; b: number }
  | { type: 'default' }

// ---------------------------------------------------------------------------
// Text style
// ---------------------------------------------------------------------------

/** Underline variants — `none` is distinct from the bool-ish absence. */
export type UnderlineStyle =
  | 'none'
  | 'single'
  | 'double'
  | 'curly'
  | 'dotted'
  | 'dashed'

/** Per-cell style state. */
export type TextStyle = {
  bold: boolean
  dim: boolean
  italic: boolean
  underline: UnderlineStyle
  blink: boolean
  inverse: boolean
  hidden: boolean
  strikethrough: boolean
  overline: boolean
  fg: Color
  bg: Color
  underlineColor: Color
}

export function defaultStyle(): TextStyle {
  return {
    bold: false,
    dim: false,
    italic: false,
    underline: 'none',
    blink: false,
    inverse: false,
    hidden: false,
    strikethrough: false,
    overline: false,
    fg: { type: 'default' },
    bg: { type: 'default' },
    underlineColor: { type: 'default' },
  }
}

/** Deep equality on two styles. Used by the diff loop to skip unchanged cells. */
export function stylesEqual(a: TextStyle, b: TextStyle): boolean {
  return (
    a.bold === b.bold &&
    a.dim === b.dim &&
    a.italic === b.italic &&
    a.underline === b.underline &&
    a.blink === b.blink &&
    a.inverse === b.inverse &&
    a.hidden === b.hidden &&
    a.strikethrough === b.strikethrough &&
    a.overline === b.overline &&
    colorsEqual(a.fg, b.fg) &&
    colorsEqual(a.bg, b.bg) &&
    colorsEqual(a.underlineColor, b.underlineColor)
  )
}

/** Deep equality on two colours. */
export function colorsEqual(a: Color, b: Color): boolean {
  if (a.type !== b.type) return false
  switch (a.type) {
    case 'named':
      return a.name === (b as typeof a).name
    case 'indexed':
      return a.index === (b as typeof a).index
    case 'rgb':
      return (
        a.r === (b as typeof a).r &&
        a.g === (b as typeof a).g &&
        a.b === (b as typeof a).b
      )
    case 'default':
      return true
  }
}

// ---------------------------------------------------------------------------
// Cursor actions
// ---------------------------------------------------------------------------

export type CursorDirection = 'up' | 'down' | 'forward' | 'back'

export type CursorAction =
  | { type: 'move'; direction: CursorDirection; count: number }
  | { type: 'position'; row: number; col: number }
  | { type: 'column'; col: number }
  | { type: 'row'; row: number }
  | { type: 'save' }
  | { type: 'restore' }
  | { type: 'show' }
  | { type: 'hide' }
  | {
      type: 'style'
      style: 'block' | 'underline' | 'bar'
      blinking: boolean
    }
  | { type: 'nextLine'; count: number }
  | { type: 'prevLine'; count: number }

// ---------------------------------------------------------------------------
// Erase actions
// ---------------------------------------------------------------------------

export type EraseAction =
  | { type: 'display'; region: 'toEnd' | 'toStart' | 'all' | 'scrollback' }
  | { type: 'line'; region: 'toEnd' | 'toStart' | 'all' }
  | { type: 'chars'; count: number }

// ---------------------------------------------------------------------------
// Scroll actions
// ---------------------------------------------------------------------------

export type ScrollAction =
  | { type: 'up'; count: number }
  | { type: 'down'; count: number }
  | { type: 'setRegion'; top: number; bottom: number }

// ---------------------------------------------------------------------------
// Mode actions
// ---------------------------------------------------------------------------

export type ModeAction =
  | { type: 'alternateScreen'; enabled: boolean }
  | { type: 'bracketedPaste'; enabled: boolean }
  | { type: 'mouseTracking'; mode: 'off' | 'normal' | 'button' | 'any' }
  | { type: 'focusEvents'; enabled: boolean }

// ---------------------------------------------------------------------------
// Link actions (OSC 8)
// ---------------------------------------------------------------------------

export type LinkAction =
  | { type: 'start'; url: string; params?: Record<string, string> }
  | { type: 'end' }

// ---------------------------------------------------------------------------
// Title actions (OSC 0/1/2)
// ---------------------------------------------------------------------------

export type TitleAction =
  | { type: 'windowTitle'; title: string }
  | { type: 'iconName'; name: string }
  | { type: 'both'; title: string }

// ---------------------------------------------------------------------------
// Tab status action (OSC 21337)
// ---------------------------------------------------------------------------

/** Per-tab chrome metadata. */
export type TabStatusAction = {
  indicator?: Color | null
  status?: string | null
  statusColor?: Color | null
}

// ---------------------------------------------------------------------------
// Parsed segments — the output of the parser
// ---------------------------------------------------------------------------

/** A styled run of text. */
export type TextSegment = {
  type: 'text'
  text: string
  style: TextStyle
}

/** A visual character unit pre-tagged with its display width. */
export type Grapheme = {
  value: string
  width: 1 | 2
}

/** Discriminated union of everything the parser can emit. */
export type Action =
  | { type: 'text'; graphemes: Grapheme[]; style: TextStyle }
  | { type: 'cursor'; action: CursorAction }
  | { type: 'erase'; action: EraseAction }
  | { type: 'scroll'; action: ScrollAction }
  | { type: 'mode'; action: ModeAction }
  | { type: 'link'; action: LinkAction }
  | { type: 'title'; action: TitleAction }
  | { type: 'tabStatus'; action: TabStatusAction }
  | { type: 'sgr'; params: string } // raw SGR body, applied to style state
  | { type: 'bell' }
  | { type: 'reset' } // full terminal reset (ESC c)
  | { type: 'unknown'; sequence: string } // unrecognised sequence, opaque
