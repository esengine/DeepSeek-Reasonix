import { useEffect } from "react";

import { app } from "../lib/bridge";
import { setMainWindowMaximised } from "../store/windowChrome";

// Module-owned sync state for the single AppRuntime host: the enabled gate
// mirrors the active lifecycle, and the generation ticket discards
// out-of-order IsMainWindowMaximised resolutions.
let syncEnabled = false;
let syncGeneration = 0;

/**
 * Re-reads the native maximised flag into the windowChrome store. Event
 * handlers call this after a toggle/zoom; a no-op while the lifecycle is
 * disabled so a non-frameless platform never issues the bridge call.
 */
export function syncMainWindowMaximised(): void {
  if (!syncEnabled) return;
  const generation = ++syncGeneration;
  void app.IsMainWindowMaximised()
    .then((value) => { if (generation === syncGeneration) setMainWindowMaximised(value); })
    .catch(() => { if (generation === syncGeneration) setMainWindowMaximised(false); });
}

/**
 * Owns the maximised-sync lifecycle: initial sync, resize/focus listeners and
 * the disabled/unmount reset. The flag itself lives in the windowChrome store;
 * consumers select `mainWindowMaximised` from there.
 */
export function useWindowsMaximisedSync(enabled: boolean): void {
  useEffect(() => {
    if (!enabled) {
      syncEnabled = false;
      syncGeneration += 1;
      setMainWindowMaximised(false);
      return;
    }
    syncEnabled = true;
    syncMainWindowMaximised();
    window.addEventListener("resize", syncMainWindowMaximised);
    window.addEventListener("focus", syncMainWindowMaximised);
    return () => {
      syncEnabled = false;
      syncGeneration += 1;
      window.removeEventListener("resize", syncMainWindowMaximised);
      window.removeEventListener("focus", syncMainWindowMaximised);
    };
  }, [enabled]);
}

export const nativeWindowCommands = {
  minimize: () => app.MinimiseMainWindow(),
  toggleMaximize: () => app.ToggleMaximiseMainWindow(),
  close: () => app.CloseMainWindow(),
};
