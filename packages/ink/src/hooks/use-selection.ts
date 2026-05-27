import { useContext, useMemo, useSyncExternalStore } from 'react'
import StdinContext from '../components/StdinContext.js'
import instances from '../instances.js'
import {
  type FocusMove,
  type SelectionState,
  shiftAnchor,
} from '../selection.js'

type SelectionApi = {
  copySelection: () => string
  /** Copy without clearing the highlight (for copy-on-select). */
  copySelectionNoClear: () => string
  clearSelection: () => void
  hasSelection: () => boolean
  /** Read the raw mutable selection state (used by drag-to-scroll). */
  getState: () => SelectionState | null
  /** Subscribe to selection mutations (start/update/finish/clear). */
  subscribe: (cb: () => void) => () => void
  /** Shift the anchor row by `dRow`, clamped to `[minRow, maxRow]`. */
  shiftAnchor: (dRow: number, minRow: number, maxRow: number) => void
  shiftSelection: (dRow: number, minRow: number, maxRow: number) => void
  moveFocus: (move: FocusMove) => void
  /** Capture text from rows about to scroll out of the viewport. */
  captureScrolledRows: (
    firstRow: number,
    lastRow: number,
    side: 'above' | 'below',
  ) => void
  setSelectionBgColor: (color: string) => void
}

export function useSelection(): SelectionApi {
  // The Ink instance is looked up by stdout to match `instances` map keying.
  // StdinContext is always provided in an Ink tree, so reading it here
  // anchors us to a valid App subtree (and keeps hook-rules linting happy)
  // without paying for unused values.
  useContext(StdinContext)
  const ink = instances.get(process.stdout)
  return useMemo<SelectionApi>(() => {
    if (!ink) {
      return {
        copySelection: () => '',
        copySelectionNoClear: () => '',
        clearSelection: () => {},
        hasSelection: () => false,
        getState: () => null,
        subscribe: () => () => {},
        shiftAnchor: () => {},
        shiftSelection: () => {},
        moveFocus: () => {},
        captureScrolledRows: () => {},
        setSelectionBgColor: () => {},
      }
    }
    return {
      copySelection: () => ink.copySelection(),
      copySelectionNoClear: () => ink.copySelectionNoClear(),
      clearSelection: () => ink.clearTextSelection(),
      hasSelection: () => ink.hasTextSelection(),
      getState: () => ink.selection,
      subscribe: (cb: () => void) => ink.subscribeToSelectionChange(cb),
      shiftAnchor: (dRow: number, minRow: number, maxRow: number) =>
        shiftAnchor(ink.selection, dRow, minRow, maxRow),
      shiftSelection: (dRow, minRow, maxRow) =>
        ink.shiftSelectionForScroll(dRow, minRow, maxRow),
      moveFocus: (move: FocusMove) => ink.moveSelectionFocus(move),
      captureScrolledRows: (firstRow, lastRow, side) =>
        ink.captureScrolledRows(firstRow, lastRow, side),
      setSelectionBgColor: (color: string) => ink.setSelectionBgColor(color),
    }
  }, [ink])
}

const NO_SUBSCRIBE = (): (() => void) => () => {}
const ALWAYS_FALSE = (): boolean => false

/** Reactive "is there a selection?" flag. */
export function useHasSelection(): boolean {
  useContext(StdinContext)
  const ink = instances.get(process.stdout)
  return useSyncExternalStore(
    ink ? ink.subscribeToSelectionChange : NO_SUBSCRIBE,
    ink ? ink.hasTextSelection : ALWAYS_FALSE,
  )
}
