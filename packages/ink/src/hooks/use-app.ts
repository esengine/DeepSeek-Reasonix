import { useContext } from 'react'
import AppContext, { type Props as AppContextValue } from '../components/AppContext.js'

/** Access the app-level exit handle. */
const useApp = (): AppContextValue => useContext(AppContext)
export default useApp
