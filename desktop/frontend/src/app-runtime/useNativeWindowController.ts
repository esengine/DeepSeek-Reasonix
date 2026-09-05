import { useEffect, useRef, useState } from "react";

import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";

export function useWindowsMaximised(enabled: boolean): readonly [boolean, () => void] {
  const [maximised, setMaximised] = useState(false);
  const generationRef = useRef(0);
  const sync = useCommittedCommand(() => {
    if (!enabled) return;
    const generation = ++generationRef.current;
    void app.IsMainWindowMaximised()
      .then((value) => { if (generation === generationRef.current) setMaximised(value); })
      .catch(() => { if (generation === generationRef.current) setMaximised(false); });
  });
  useEffect(() => {
    if (!enabled) {
      generationRef.current += 1;
      setMaximised(false);
      return;
    }
    sync();
    window.addEventListener("resize", sync);
    window.addEventListener("focus", sync);
    return () => {
      generationRef.current += 1;
      window.removeEventListener("resize", sync);
      window.removeEventListener("focus", sync);
    };
  }, [enabled, sync]);
  return [maximised, sync] as const;
}

export const nativeWindowCommands = {
  minimize: () => app.MinimiseMainWindow(),
  toggleMaximize: () => app.ToggleMaximiseMainWindow(),
  close: () => app.CloseMainWindow(),
};
