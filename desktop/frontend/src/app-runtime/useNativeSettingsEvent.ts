import { useEffect } from "react";
import { useAppNavigationStore } from "../store/appNavigation";

export function useNativeSettingsEvent(input: {
  closeTransientOverlays: () => void;
  setSettingsTarget: (target: ReturnType<typeof useAppNavigationStore.getState>["lastSettingsTarget"]) => void;
}) {
  const { closeTransientOverlays, setSettingsTarget } = input;
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      closeTransientOverlays();
      setSettingsTarget(useAppNavigationStore.getState().lastSettingsTarget);
    });
  }, [closeTransientOverlays, setSettingsTarget]);
}
