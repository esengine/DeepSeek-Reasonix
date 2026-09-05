import { createContext, useContext } from "react";
import type { CommandOutcome } from "./commandOutcome";
import type { RemoteTabOpenOptions, RemoteTabRefView, TabMeta } from "./types";

/** Command-only dependency; no App snapshot or service lookup lives here. */
export type RemoteNavigationCommand = (remote: RemoteTabRefView, options: RemoteTabOpenOptions) => Promise<CommandOutcome<TabMeta | undefined>>;
const notReady: RemoteNavigationCommand = async () => ({ status: "cancelled", reason: "not-ready" });
export const RemoteNavigationContext = createContext<RemoteNavigationCommand>(notReady);
export function useRemoteNavigationCommand(): RemoteNavigationCommand { return useContext(RemoteNavigationContext); }
