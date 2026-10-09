import { useEffect, useState, useSyncExternalStore } from "react";
import { navRailMode, onNavRailChange } from "../state/prefs";
import { folded, onFolds } from "./viewport";

/** What the icon column does this render. `drawn` is whether it exists at all:
 *  the machine's choice, unless the window has folded to the scene, where the
 *  workspace rail is a drawer. `shown` is whether it is on screen — in
 *  "collapsed" mode that follows the workspace rail, and the element stays
 *  mounted across that so the rail closing moves it rather than creating it.
 *  The fold is never written back to the choice. */
export function useNavRail(railOpen: boolean): { drawn: boolean; shown: boolean } {
  const mode = useSyncExternalStore(onNavRailChange, navRailMode, navRailMode);
  const [phone, setPhone] = useState(() => folded("scene"));
  useEffect(() => {
    setPhone(folded("scene"));
    return onFolds((now) => setPhone(now.split(" ").includes("scene")));
  }, []);
  const drawn = mode !== "off" && !phone;
  return { drawn, shown: drawn && (mode === "on" || !railOpen) };
}
