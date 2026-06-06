import { useContext, useMemo } from 'react'
import StdinContext from '../components/StdinContext.js'
import type { DOMElement } from '../dom.js'
import instances from '../instances.js'
import type { MatchPosition } from '../render-to-screen.js'

type SearchPositionsState = {
  positions: MatchPosition[]
  rowOffset: number
  currentIdx: number
}

type SearchHighlightApi = {
  /** Set the live query. */
  setQuery: (query: string) => void

  scanElement: (el: DOMElement) => MatchPosition[]

  /** Drive the "current match" highlight from a position list. */
  setPositions: (state: SearchPositionsState | null) => void
}

/** Bridge React-land to the Ink instance's search-overlay machinery. */
export function useSearchHighlight(): SearchHighlightApi {
  useContext(StdinContext) // anchor to the App subtree so hook rules apply
  const ink = instances.get(process.stdout)
  return useMemo<SearchHighlightApi>(() => {
    if (!ink) {
      return {
        setQuery: () => {},
        scanElement: () => [],
        setPositions: () => {},
      }
    }
    return {
      setQuery: (query: string) => ink.setSearchHighlight(query),
      scanElement: (el: DOMElement) => ink.scanElementSubtree(el),
      setPositions: state => ink.setSearchPositions(state),
    }
  }, [ink])
}
