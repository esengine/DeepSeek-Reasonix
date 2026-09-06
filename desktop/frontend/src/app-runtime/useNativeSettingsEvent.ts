import { useEffect } from "react";
import { useAppNavigationStore } from "../store/appNavigation";

export function useNativeSettingsEvent(input: {
  closeTransientOverlays: () => void;
  setSettingsTarget: (target: ReturnType<typeof useAppNavigationStore.getState>["lastSettingsTarget"]) => void;
}) {
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      input.closeTransientOverlays();
      input.setSettingsTarget(useAppNavigationStore.getState().lastSettingsTarget);
    });
  }, [input.closeTransientOverlays, input.setSettingsTarget]);
}
