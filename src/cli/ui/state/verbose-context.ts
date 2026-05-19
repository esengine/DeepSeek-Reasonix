import { createContext } from "react";

/** Ctrl+R toggles this; ReasoningCard / ToolCard show full content (no head/tail elision) when true. */
export const VerboseContext = createContext<boolean>(false);
