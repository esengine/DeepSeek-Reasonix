import { createContext, useContext } from "react";

export interface AppApi {
  readonly exit: (error?: Error) => void;
}

export const AppContext = createContext<AppApi>({ exit: () => {} });

export function useApp(): AppApi {
  return useContext(AppContext);
}
