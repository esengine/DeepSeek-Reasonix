import { createContext } from "react";

/** ctrl-o toggles this; live streaming card swaps 4-line tail for full-tail view. */
export const LiveExpandContext = createContext<boolean>(false);
