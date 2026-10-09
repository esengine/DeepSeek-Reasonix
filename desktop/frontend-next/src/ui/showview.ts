import { useCallback } from "react";
import { usePaneView, type PaneView } from "../state/paneview";
import { swapping } from "./swap";

/** Every route into a view goes through here, so the nav never grows a motion
 *  language of its own: it says which view, and this says how a pane changes
 *  from one to another. The workbench sits beside the conversation rather than
 *  over it, so picking it opens the dock the globe does. */
export function useShowView(id: string, onManualBrowser?: (on: boolean) => void): [PaneView, (to: PaneView) => void] {
  const [tab, setTab] = usePaneView(id);
  const showView = useCallback(
    (to: PaneView) => {
      if (to === "browser") {
        onManualBrowser?.(true);
        swapping(() => setTab("flow"), "tab");
        return;
      }
      if (to === "flow") onManualBrowser?.(false);
      swapping(() => setTab(to), "tab");
    },
    [onManualBrowser, setTab],
  );
  return [tab, showView];
}
