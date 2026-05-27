import { TerminalEvent } from './terminal-event.js'

/** Bracketed-paste event placeholder. */
export class PasteEvent extends TerminalEvent {
  readonly data: string

  constructor(data: string) {
    super('paste', { bubbles: true, cancelable: true })
    this.data = data
  }
}
