import { useContext } from 'react'
import StdinContext, { type Props as StdinContextValue } from '../components/StdinContext.js'

/** Access the stdin handle plus the raw-mode toggle that Ink owns. */
const useStdin = (): StdinContextValue => useContext(StdinContext)
export default useStdin
