import { Event } from './event.js'

/** Mouse left-click event. */
export class ClickEvent extends Event {
  readonly col: number
  readonly row: number
  /** Click column relative to the current handler's Box (col - box.x). */
  localCol = 0
  /** Click row relative to the current handler's Box (row - box.y). */
  localRow = 0
  readonly cellIsBlank: boolean

  constructor(col: number, row: number, cellIsBlank: boolean) {
    super()
    this.col = col
    this.row = row
    this.cellIsBlank = cellIsBlank
  }
}
