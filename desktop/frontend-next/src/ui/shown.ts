import { createContext, useContext, useSyncExternalStore } from "react";

/** Whether the pane this card lives in is the one being looked at. A pane
 *  behind another keeps running but has nobody to animate for. */
export const PaneShown = createContext(true);

export const useShown = () => useContext(PaneShown);

const subscribe = (on: () => void) => {
  document.addEventListener("visibilitychange", on);
  return () => document.removeEventListener("visibilitychange", on);
};

export const usePageVisible = () =>
  useSyncExternalStore(subscribe, () => document.visibilityState !== "hidden", () => true);
