import type { ClickEvent } from './click-event.js'
import type { FocusEvent } from './focus-event.js'
import type { KeyboardEvent } from './keyboard-event.js'
import type { PasteEvent } from './paste-event.js'
import type { ResizeEvent } from './resize-event.js'

type KeyboardHandler = (event: KeyboardEvent) => void
type FocusHandler = (event: FocusEvent) => void
type PasteHandler = (event: PasteEvent) => void
type ResizeHandler = (event: ResizeEvent) => void
type ClickHandler = (event: ClickEvent) => void
type HoverHandler = () => void

/** Event-handler props accepted by Box and other host components. */
export type EventHandlerProps = {
  onKeyDown?: KeyboardHandler
  onKeyDownCapture?: KeyboardHandler

  onFocus?: FocusHandler
  onFocusCapture?: FocusHandler
  onBlur?: FocusHandler
  onBlurCapture?: FocusHandler

  onPaste?: PasteHandler
  onPasteCapture?: PasteHandler

  onResize?: ResizeHandler

  onClick?: ClickHandler
  onMouseEnter?: HoverHandler
  onMouseLeave?: HoverHandler,
}

export const HANDLER_FOR_EVENT: Record<
  string,
  { bubble?: keyof EventHandlerProps; capture?: keyof EventHandlerProps }
> = {
  keydown: { bubble: 'onKeyDown', capture: 'onKeyDownCapture' },
  focus: { bubble: 'onFocus', capture: 'onFocusCapture' },
  blur: { bubble: 'onBlur', capture: 'onBlurCapture' },
  paste: { bubble: 'onPaste', capture: 'onPasteCapture' },
  resize: { bubble: 'onResize' },
  click: { bubble: 'onClick' },
}

/** Set of every prop name recognised as an event handler. */
export const EVENT_HANDLER_PROPS = new Set<string>([
  'onKeyDown',
  'onKeyDownCapture',
  'onFocus',
  'onFocusCapture',
  'onBlur',
  'onBlurCapture',
  'onPaste',
  'onPasteCapture',
  'onResize',
  'onClick',
  'onMouseEnter',
  'onMouseLeave',
])
