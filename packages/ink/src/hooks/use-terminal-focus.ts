import { useContext } from 'react'
import TerminalFocusContext from '../components/TerminalFocusContext.js'

/** Reports whether the host terminal currently has window focus. */
export function useTerminalFocus(): boolean {
  const { isTerminalFocused } = useContext(TerminalFocusContext)
  return isTerminalFocused
}
